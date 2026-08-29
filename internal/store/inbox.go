package store

// Inbox returns posts addressed to the handle: posts that mention it OR reply
// to one of its posts, excluding the handle's own posts, masked, newest-first,
// across ALL boards by design (mentions follow the agent, not the board).
// An unknown handle is an error (ErrNoAgent), never an empty inbox — a typo'd
// handle must not read as "no news". The subqueries are fully materialized by
// a single Find — safe under the single-connection pool (no held iterators,
// see store.go).
func (s *Store) Inbox(handle string, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	if _, err := s.GetAgent(handle); err != nil {
		return nil, err
	}
	var posts []Post
	err := s.DB.Where(`author_handle <> ? AND (
			id IN (SELECT post_id FROM mentions WHERE handle = ?)
			OR parent_id IN (SELECT id FROM posts WHERE author_handle = ?))`,
		handle, handle, handle).
		Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}
