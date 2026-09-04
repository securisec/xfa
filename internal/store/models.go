package store

import "time"

type Board struct {
	ID          uint   `gorm:"primaryKey"`
	Slug        string `gorm:"uniqueIndex;not null"`
	Description string
	CreatedAt   time.Time
}

type Project struct {
	ID        uint   `gorm:"primaryKey"`
	Path      string `gorm:"uniqueIndex;not null"` // absolute
	BoardID   uint   `gorm:"not null"`
	CreatedAt time.Time
}

type Agent struct {
	ID           uint   `gorm:"primaryKey"`
	Handle       string `gorm:"uniqueIndex;not null"`
	Provider     string
	SessionID    string `gorm:"index"`
	ParentHandle string
	Repo         string // display hint: which repo the agent works in (`xfa register --repo`)
	LastSeenAt   time.Time
	CreatedAt    time.Time
}

type Post struct {
	ID           uint   `gorm:"primaryKey"`
	BoardID      uint   `gorm:"index;not null"`
	AuthorHandle string `gorm:"index;not null"`
	ParentID     *uint  `gorm:"index"` // nil = top-level
	Body         string
	Tag          string `gorm:"index"`
	CreatedAt    time.Time
	TombstonedAt *time.Time // NOT gorm.DeletedAt — see Global Constraints
	ResolvedAt   *time.Time // question-tagged posts only; nil = still open
	ResolvedBy   string
}

// ReadCursor is a per-(handle, board) unread cursor: posts on the board with
// id > LastReadID are unread for the handle. id, not time: ids are
// AUTOINCREMENT under _txlock=immediate so id order is commit order, while
// created_at is offset-bearing TEXT whose lexical order is not chronological
// and is stamped before commit. LastReadAt is kept (written as "now") for the
// one-time backfill (store.go) and for humans reading the table. No row = the
// 24h floor (see readCursorID), never "everything since epoch". Per-board, so
// catching up on one board never consumes unread posts on another.
type ReadCursor struct {
	ID         uint   `gorm:"primaryKey"`
	Handle     string `gorm:"uniqueIndex:idx_cursor,priority:1"`
	BoardID    uint   `gorm:"uniqueIndex:idx_cursor,priority:2"`
	LastReadID uint
	LastReadAt time.Time
}

// Mention is written at post time by parsing @handle references from the body.
type Mention struct {
	ID        uint   `gorm:"primaryKey"`
	PostID    uint   `gorm:"index;not null"`
	Handle    string `gorm:"index;not null"`
	CreatedAt time.Time
}

// PostLink is written at post time by parsing #id references from the body
// (PostRefIDs). Only targets that exist at write time are recorded; rows are
// immutable with the post and hard-deleted along with either end.
type PostLink struct {
	ID           uint `gorm:"primaryKey"`
	SourcePostID uint `gorm:"index;not null;uniqueIndex:idx_post_links_pair"`
	TargetPostID uint `gorm:"index;not null;uniqueIndex:idx_post_links_pair"`
	CreatedAt    time.Time
}

// Session is a human/agent-assigned label for a provider session id. Rows are
// created lazily, on the first rename only: `xfa register` never writes here,
// so an unnamed session simply has no row and display layers fall back to
// lead-handle · date · short id. Empty session ids are never nameable.
type Session struct {
	ID        uint   `gorm:"primaryKey"`
	SessionID string `gorm:"uniqueIndex;not null"`
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Reminder marks a session already nudged by the stop hook (fire once), and
// doubles as a tiny key/value store for per-session hook marks (SetMark /
// GetMark) — Value stays empty for plain fire-once rows.
type Reminder struct {
	ID        uint   `gorm:"primaryKey"`
	SessionID string `gorm:"uniqueIndex;not null"`
	Value     string
	CreatedAt time.Time
}
