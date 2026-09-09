package store

// Queries for the `xfa tui --web` activity log (internal/tui/activity.go),
// a poller that reads from its own Store so every writer is foreign to it.

// DataVersion is SQLite's PRAGMA data_version for this Store's connection: it
// changes only when ANOTHER connection commits, never on own writes or idle
// reads, so an idle poll tick is exactly this one pragma and no table access.
func (s *Store) DataVersion() (int64, error) {
	var v int64
	err := s.DB.Raw(`PRAGMA data_version`).Scan(&v).Error
	return v, err
}

// PostsAfter returns every post on any board with id > after, in id (commit)
// order, tombstones masked. The activity log's watermark query; ids are
// AUTOINCREMENT so a hard delete lowering MAX(id) never replays anything.
func (s *Store) PostsAfter(after int64) ([]Post, error) {
	var posts []Post
	err := s.DB.Where("id > ?", after).Order("id").Find(&posts).Error
	return maskTombstones(posts), err
}

// PostMark is one row of MarkedPostIDs: which of the two mutable flags a post
// carries. Resolve/tombstone are detected by set diff on these, not by time,
// because resolved_at/tombstoned_at are stamped in Go before the busy-retried
// commit lands and so can trail wall-clock by seconds.
type PostMark struct {
	ID         uint
	Resolved   bool
	Tombstoned bool
}

// MarkedPostIDs lists every post that is resolved and/or tombstoned.
// ponytail: full posts scan per changed tick, returning every resolved/
// tombstoned row; add a partial index WHERE resolved_at IS NOT NULL OR
// tombstoned_at IS NOT NULL if it ever matters.
func (s *Store) MarkedPostIDs() ([]PostMark, error) {
	var marks []PostMark
	err := s.DB.Raw(`SELECT id, resolved_at IS NOT NULL AS resolved, tombstoned_at IS NOT NULL AS tombstoned
		FROM posts WHERE resolved_at IS NOT NULL OR tombstoned_at IS NOT NULL ORDER BY id`).Scan(&marks).Error
	return marks, err
}
