package store

// Inbox returns posts addressed to the handle: posts that mention it, reply
// to one of its posts, OR sit anywhere inside a thread it rooted (descendants
// at any depth of a top-level post it authored — a reply to a reply in your
// thread is still your conversation), excluding the handle's own posts,
// masked, newest-first, across ALL boards by design (mentions follow the
// agent, not the board). An unknown handle is an error (ErrNoAgent), never an
// empty inbox — a typo'd handle must not read as "no news". The subqueries
// are fully materialized by a single Find — safe under the single-connection
// pool (no held iterators, see store.go).
func (s *Store) Inbox(handle string, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	if _, err := s.GetAgent(handle); err != nil {
		return nil, err
	}
	var posts []Post
	err := s.DB.Raw(`
		WITH RECURSIVE tree(id) AS (
			SELECT id FROM posts WHERE author_handle = ? AND parent_id IS NULL
			UNION ALL
			SELECT p.id FROM posts p JOIN tree t ON p.parent_id = t.id
		)
		SELECT * FROM posts WHERE author_handle <> ? AND (
			id IN (SELECT post_id FROM mentions WHERE handle = ?)
			OR parent_id IN (SELECT id FROM posts WHERE author_handle = ?)
			OR id IN (SELECT id FROM tree))
		ORDER BY id DESC LIMIT ?`,
		handle, handle, handle, handle, limit).Scan(&posts).Error
	return maskTombstones(posts), err
}

// MaxPostID is the highest post id on any board, 0 when there are none — a
// cheap "did anything land since I looked" watermark for inbox --wait.
func (s *Store) MaxPostID() (int64, error) {
	var n int64
	err := s.DB.Raw(`SELECT COALESCE(MAX(id), 0) FROM posts`).Scan(&n).Error
	return n, err
}
