package store

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoSession marks a hard-delete request for a session id that has no
// agents and no stored name — nothing exists to delete. Unlike ErrNoSessionID
// (an empty/blank filter), this is a well-formed id that simply isn't known.
var ErrNoSession = errors.New("no such session")

// doomedPostSubtreeCTE collects a post and its entire reply subtree,
// arbitrary depth, via a recursive CTE seeded by the given SELECT (which must
// yield an `id` column of the roots to doom). UNION (not UNION ALL) dedupes
// rows the recursion can otherwise re-derive through more than one seed path
// — e.g. a session-hard-delete seed set containing both an ancestor and a
// descendant post — which keeps the recursion cycle-proof and avoids
// redundant re-expansion; correctness of the final delete is unaffected
// either way since it filters by membership in a materialized id list, not
// by row count.
const doomedPostSubtreeCTE = `WITH RECURSIVE doomed(id) AS (
	%s
	UNION
	SELECT p.id FROM posts p JOIN doomed d ON p.parent_id = d.id
)`

// extraStmt is one additional raw-SQL write to run inside hardDeleteDoomed's
// retry closure, alongside the posts/mentions deletes — used by
// HardDeleteSession to fold in "forget the session's name row" so a single
// busy retry covers the whole operation instead of leaving a second,
// independently-failable retry unit whose failure would misreport the count.
type extraStmt struct {
	sql  string
	args []any
}

// hardDeleteDoomed deletes the posts, mentions and post_links rows for the doomed set
// produced by seedSQL (a SELECT yielding an `id` column of the roots), plus
// runs any extra statements, returning the number of posts removed. seedSQL
// must be a package literal — never caller- or user-supplied text — since it
// is spliced into the query string directly; all actual data must travel
// through the `?` placeholders bound by seedArgs.
//
// The doomed id set is resolved once, up front, via a read-only query, and
// then reused verbatim for every delete and for every busy retry. This
// matters because the deletes are not one SQL transaction (single-connection
// store, no gorm transactions — see store.go): if the doomed set were instead
// re-derived from the posts table inside the retry closure, the first
// successful "delete posts" statement would make the posts table stop
// containing the very rows the CTE seed query looks for, so a retry
// triggered by a later statement failing (or a busy error on any attempt
// after the first) would silently recompute an empty doomed set. Resolving
// it once up front avoids that trap entirely.
//
// Posts are deleted before mentions: if a failure lands between the two
// deletes, a live post left without its mention rows is only a dropped inbox
// entry, whereas an orphan mention row pointing at a live post is inert
// (Inbox joins mentions back through posts) — deleting posts first means any
// partial-failure state favors the inert outcome. post_links follows for the
// same reason: LinksFor resolves each far end back through posts, so a link
// row that outlives a deleted post simply stops rendering. All three deletes,
// and any extra statements, are idempotent (id-membership deletes /
// upsert-free deletes), so a busy retry safely re-runs the whole closure.
func (s *Store) hardDeleteDoomed(seedSQL string, seedArgs []any, extra ...extraStmt) (int64, error) {
	cte := fmt.Sprintf(doomedPostSubtreeCTE, seedSQL)
	var ids []uint
	if err := s.DB.Raw(cte+` SELECT id FROM doomed`, seedArgs...).Scan(&ids).Error; err != nil {
		return 0, err
	}
	err := withBusyRetry(func() error {
		if len(ids) > 0 {
			if err := s.DB.Exec(`DELETE FROM posts WHERE id IN ?`, ids).Error; err != nil {
				return err
			}
			if err := s.DB.Exec(`DELETE FROM mentions WHERE post_id IN ?`, ids).Error; err != nil {
				return err
			}
			if err := s.DB.Exec(`DELETE FROM post_links WHERE source_post_id IN ? OR target_post_id IN ?`, ids, ids).Error; err != nil {
				return err
			}
		}
		for _, e := range extra {
			if err := s.DB.Exec(e.sql, e.args...).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// HardDeletePost permanently removes post postID and its entire reply
// subtree, plus their mentions and post_links rows (in either direction).
// Returns the number of posts removed.
// No ownership check: this is a moderator path, reachable only through the
// human-gated web server.
func (s *Store) HardDeletePost(postID uint) (int64, error) {
	if _, err := s.GetPost(postID); err != nil {
		if errors.Is(err, ErrNoPost) {
			return 0, fmt.Errorf("post %d not found: %w", postID, ErrNoPost)
		}
		return 0, err
	}
	return s.hardDeleteDoomed(`SELECT id FROM posts WHERE id = ?`, []any{postID})
}

// HardDeleteSession permanently removes every post authored by the agents of
// sessionID (including foreign replies nested under them), plus their
// mentions and post_links, and forgets the session's name row. Agent rows themselves are
// left untouched. Returns the number of posts removed.
// No ownership check: this is a moderator path, reachable only through the
// human-gated web server.
func (s *Store) HardDeleteSession(sessionID string) (int64, error) {
	if strings.TrimSpace(sessionID) == "" {
		return 0, ErrNoSessionID
	}

	var agentCount int64
	if err := s.DB.Model(&Agent{}).Where("session_id = ?", sessionID).Count(&agentCount).Error; err != nil {
		return 0, err
	}
	var nameCount int64
	if err := s.DB.Model(&Session{}).Where("session_id = ?", sessionID).Count(&nameCount).Error; err != nil {
		return 0, err
	}
	if agentCount == 0 && nameCount == 0 {
		return 0, fmt.Errorf("session %s not found: %w", sessionID, ErrNoSession)
	}

	return s.hardDeleteDoomed(
		`SELECT id FROM posts WHERE author_handle IN (SELECT handle FROM agents WHERE session_id = ?)`,
		[]any{sessionID},
		extraStmt{sql: `DELETE FROM sessions WHERE session_id = ?`, args: []any{sessionID}},
	)
}
