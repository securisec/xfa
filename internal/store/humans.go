package store

import "time"

// ProviderHuman is the agents.provider value minted for web-UI writes
// (internal/web/identity.go registers the web handle with it).
const ProviderHuman = "human"

// HumanHandlesFor returns which of the given handles belong to
// provider=human agents, as a set. Mirrors LastSeenFor's batch shape.
func (s *Store) HumanHandlesFor(handles []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(handles) == 0 {
		return out, nil
	}
	var rows []string
	err := s.DB.Model(&Agent{}).
		Where("handle IN ? AND provider = ?", handles, ProviderHuman).
		Pluck("handle", &rows).Error
	if err != nil {
		return nil, err
	}
	for _, h := range rows {
		out[h] = true
	}
	return out, nil
}

// isHumanPost reports whether the post's author is a provider=human agent.
// A missing author row is not human.
func (s *Store) isHumanPost(p *Post) bool {
	a, err := s.GetAgent(p.AuthorHandle)
	return err == nil && a.Provider == ProviderHuman
}

// HandleSet returns the deduped author handles of posts.
func HandleSet(posts []Post) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range posts {
		if !seen[p.AuthorHandle] {
			seen[p.AuthorHandle] = true
			out = append(out, p.AuthorHandle)
		}
	}
	return out
}

// ReadBoardHuman is ReadBoardTagged narrowed to human-authored posts.
func (s *Store) ReadBoardHuman(boardID uint, tag string, since time.Time, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	var posts []Post
	err := s.readBoardQuery(boardID, tag, since).
		Where("author_handle IN (SELECT handle FROM agents WHERE provider = ?)", ProviderHuman).
		Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}

// UnaddressedHumanCount counts live human-authored posts — top-level or
// reply — with no live non-human direct reply and no resolution — the
// orchestrator's queue. Deliberately NOT restricted to top-level: with web
// inline replies, human messages are frequently replies themselves, and each
// one still wants an agent's direct reply (or a resolution) to count as
// addressed. A human's own reply to their own post does not address the
// parent, and is itself counted as unaddressed until something non-human
// replies to it directly.
func (s *Store) UnaddressedHumanCount(boardID uint) (int64, error) {
	var n int64
	err := s.DB.Raw(`SELECT COUNT(*) FROM posts p
		WHERE p.board_id = ?
		  AND p.tombstoned_at IS NULL
		  AND p.resolved_at IS NULL
		  AND p.author_handle IN (SELECT handle FROM agents WHERE provider = ?)
		  AND NOT EXISTS (
			SELECT 1 FROM posts r
			JOIN agents a ON a.handle = r.author_handle
			WHERE r.parent_id = p.id
			  AND r.tombstoned_at IS NULL
			  AND a.provider <> ?
		  )`, boardID, ProviderHuman, ProviderHuman).Scan(&n).Error
	return n, err
}

// PostsByAuthor lists one handle's own posts and replies, newest first.
// boardID 0 means all boards (Stats' convention). Tombstoned posts are kept
// and masked — the author's own deletions still belong in their own list.
func (s *Store) PostsByAuthor(boardID uint, handle string, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	q := s.DB.Model(&Post{}).Where("author_handle = ?", handle)
	if boardID != 0 {
		q = q.Where("board_id = ?", boardID)
	}
	var posts []Post
	err := q.Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}
