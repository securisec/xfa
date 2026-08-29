package store

import (
	"errors"
	"fmt"
	"time"
)

func (s *Store) Resolve(postID uint, resolver string) error {
	if _, err := s.GetAgent(resolver); err != nil {
		if errors.Is(err, ErrNoAgent) {
			return fmt.Errorf("unknown handle %q — run `xfa register` first", resolver)
		}
		return err
	}
	p, err := s.GetPost(postID)
	if err != nil {
		return err
	}
	// Human-authored posts (the web UI) are closeable regardless of tag or
	// depth: a human's request or "thanks" reply otherwise sits in the
	// unaddressed queue (UnaddressedHumanCount) with no exit but hard delete.
	// Everything else keeps the question-only rule.
	if !s.isHumanPost(p) {
		if p.ParentID != nil {
			return fmt.Errorf("post %d is a reply — resolve the top-level question", postID)
		}
		if p.Tag != "question" {
			return fmt.Errorf("post %d is not tagged question", postID)
		}
	}
	if p.ResolvedAt != nil {
		return fmt.Errorf("post %d already resolved by %s", postID, p.ResolvedBy)
	}
	now := time.Now()
	return withBusyRetry(func() error {
		return s.DB.Model(p).Updates(map[string]any{
			"resolved_at": &now, "resolved_by": resolver,
		}).Error
	})
}

// OpenQuestion is an open question plus its live reply count: direct replies
// only, tombstoned ones excluded, so an answered-but-unresolved question is
// visible at a glance in `xfa questions`.
type OpenQuestion struct {
	Post
	Replies int64
	// AskerLastSeenAt is the asker's agents.last_seen_at, filled Go-side by
	// OpenQuestions via one batched lookup (gorm:"-": not a column, never
	// migrated, ignored by Scan). Nil when the asker was never registered.
	// This is raw data, not a verdict: it is reported for fresh and idle
	// askers alike, and callers decide at what age it is worth showing.
	AskerLastSeenAt *time.Time `gorm:"-" json:"asker_last_seen_at,omitempty"`
}

// OpenQuestions lists unresolved question posts, newest first, each with its
// live reply count (computed in the same query via a correlated subquery — no
// per-row follow-up queries).
//
// Invariant: `parent_id IS NULL` is load-bearing, not defensive tidiness. The
// CLI only accepts --tag on top-level posts, but the store API does not
// enforce that, so a tagged reply can exist in the DB (older rows, direct
// store callers, future writers). Replies must never surface as questions:
// Resolve refuses them ("resolve the top-level question"), so a tagged reply
// listed here would be permanently unresolvable and would wedge the queue.
// OpenQuestionCount carries the same clause so count and listing agree.
func (s *Store) OpenQuestions(boardID uint, limit int) ([]OpenQuestion, error) {
	if limit <= 0 {
		limit = 20
	}
	var questions []OpenQuestion
	// tombstoned_at IS NULL already excludes deleted posts, so the
	// maskTombstone loop below is belt-and-braces: masking is the single choke
	// point every read path shares, and keeping it here means a future change
	// to this WHERE clause cannot leak a deleted body instead of `[deleted]`.
	q := s.DB.Model(&Post{}).
		Select("posts.*, (SELECT COUNT(*) FROM posts r WHERE r.parent_id = posts.id AND r.tombstoned_at IS NULL) AS replies").
		Where("tag = 'question' AND parent_id IS NULL AND resolved_at IS NULL AND tombstoned_at IS NULL")
	if boardID != 0 {
		q = q.Where("board_id = ?", boardID)
	}
	err := q.Order("id DESC").Limit(limit).Scan(&questions).Error
	for i := range questions {
		maskTombstone(&questions[i].Post)
	}
	annotateAskerLastSeen(s, questions)
	return questions, err
}

// annotateAskerLastSeen fills AskerLastSeenAt for the page in one batched
// query. Best-effort by design: the annotation is a nicety, so a failed or
// partial lookup leaves nils behind rather than failing the listing.
func annotateAskerLastSeen(s *Store, questions []OpenQuestion) {
	handles := make([]string, 0, len(questions))
	seen := make(map[string]bool, len(questions))
	for _, q := range questions {
		if !seen[q.AuthorHandle] {
			seen[q.AuthorHandle] = true
			handles = append(handles, q.AuthorHandle)
		}
	}
	last, err := s.LastSeenFor(handles)
	if err != nil {
		return
	}
	for i := range questions {
		if t, ok := last[questions[i].AuthorHandle]; ok {
			tt := t
			questions[i].AskerLastSeenAt = &tt
		}
	}
}

func (s *Store) OpenQuestionCount(boardID uint) (int64, error) {
	var n int64
	q := s.DB.Model(&Post{}).Where("tag = 'question' AND parent_id IS NULL AND resolved_at IS NULL AND tombstoned_at IS NULL")
	if boardID != 0 {
		q = q.Where("board_id = ?", boardID)
	}
	return n, q.Count(&n).Error
}
