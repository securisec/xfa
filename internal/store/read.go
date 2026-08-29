package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// sqlTimeLayout matches the "YYYY-MM-DD HH:MM:SS.SSS" text produced by
// SQLite's strftime with %Y-%m-%d %H:%M:%f.
const sqlTimeLayout = "2006-01-02 15:04:05.000"

// sinceExpr normalizes the stored offset-bearing TEXT timestamp to UTC via
// strftime before comparing, so the comparison is chronological. A raw
// lexicographic `created_at > ?` against a UTC cutoff never matches in
// negative-offset timezones.
const sinceExpr = "strftime('%Y-%m-%d %H:%M:%f', created_at) > ?"

// sinceArg quantizes to milliseconds by ROUNDING, not truncating: SQLite's
// strftime rounds the stored timestamp half-up to ms (.4786 -> .479), while
// Go's .000 format truncates. A truncated cursor taken just after a post in
// the same real millisecond would compare LOWER than the post's rounded value
// and leak the already-read post back as unread.
func sinceArg(since time.Time) string {
	return since.UTC().Round(time.Millisecond).Format(sqlTimeLayout)
}

// maskTombstones replaces the body of tombstoned posts with "[deleted]" at the
// store boundary. Tombstoned posts are never filtered out: replies to a
// tombstoned parent are allowed, so dropping the parent would orphan live
// replies out of the tree.
func maskTombstones(posts []Post) []Post {
	for i := range posts {
		maskTombstone(&posts[i])
	}
	return posts
}

// maskTombstone is the single-post form of maskTombstones, for read paths
// whose row type embeds Post (e.g. OpenQuestion) and so cannot pass a []Post.
func maskTombstone(p *Post) {
	if p.TombstonedAt != nil {
		p.Body = "[deleted]"
	}
}

func (s *Store) ReadBoard(boardID uint, since time.Time, limit int) ([]Post, error) {
	return s.ReadBoardTagged(boardID, "", since, limit)
}

// readBoardQuery is the shared board/tag/since predicate behind the flat
// newest-first listings (ReadBoardTagged and its session-scoped sibling), so
// the two can never disagree about what "since" or an empty tag mean.
func (s *Store) readBoardQuery(boardID uint, tag string, since time.Time) *gorm.DB {
	q := s.DB.Where("board_id = ?", boardID)
	if tag != "" {
		q = q.Where("tag = ?", tag)
	}
	if !since.IsZero() {
		q = q.Where(sinceExpr, sinceArg(since))
	}
	return q
}

// ReadBoardTagged is ReadBoard restricted to posts carrying tag; an empty tag
// matches all posts. Tombstoned posts are masked, never filtered.
func (s *Store) ReadBoardTagged(boardID uint, tag string, since time.Time, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	var posts []Post
	err := s.readBoardQuery(boardID, tag, since).
		Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}

// BoardPosts returns every post on the board, oldest first, masked. This is
// the "everything" fetch behind the board-view commands (threads/board), which
// group posts in Go instead of running per-thread recursive CTEs; boards are
// small by design, so there is deliberately no limit.
//
// The rule for the WHOLE package: every chronological read orders by id, NOT
// created_at; sinceExpr is for cutoff compares only. The posts table is
// `integer PRIMARY KEY AUTOINCREMENT` under _txlock=immediate (one writer at a
// time), so ids are never reused even though posts can be hard deleted
// (moderate.go) — id order IS commit order, and a parent is always inserted
// before its replies. ORDER BY created_at is a lexicographic compare over
// offset-bearing TEXT timestamps, which inverts chronology across differing
// UTC offsets (and is stamped in Go before the commit lands), so it can sort
// a reply before its parent and make it vanish from the grouped views.
func (s *Store) BoardPosts(boardID uint) ([]Post, error) {
	var posts []Post
	err := s.DB.Where("board_id = ?", boardID).
		Order("id ASC").Find(&posts).Error
	return maskTombstones(posts), err
}

// RootOf walks parent_id up to the top-level post. ErrNoPost propagates from
// GetPost when id (or, impossibly, an ancestor) doesn't exist. The walk is
// bounded so a corrupt parent cycle in a shared DB errors instead of hanging.
// ponytail: a loop of GetPost; recursive CTE if a thread ever gets deep enough to notice.
func (s *Store) RootOf(id uint) (uint, error) {
	start := id
	for i := 0; i < 1000; i++ {
		p, err := s.GetPost(id)
		if err != nil {
			return 0, err
		}
		if p.ParentID == nil {
			return p.ID, nil
		}
		id = *p.ParentID
	}
	return 0, fmt.Errorf("post %d: parent chain exceeds 1000 posts (cycle?)", start)
}

// Thread returns the whole thread containing id — any post in it, not just
// the root — ordered by id (insertion order, so a parent always precedes its
// replies; created_at is lexical offset-bearing TEXT and can invert that
// across timezones, see BoardPosts).
func (s *Store) Thread(id uint) ([]Post, error) {
	root, err := s.RootOf(id)
	if err != nil {
		return nil, err
	}
	var posts []Post
	err = s.DB.Raw(`
		WITH RECURSIVE tree(id) AS (
			SELECT id FROM posts WHERE id = ?
			UNION ALL
			SELECT p.id FROM posts p JOIN tree t ON p.parent_id = t.id
		)
		SELECT * FROM posts WHERE id IN (SELECT id FROM tree)
		ORDER BY id ASC`, root).Scan(&posts).Error
	return maskTombstones(posts), err
}

// UnreadPosts returns live posts on the board past the handle's per-board read
// cursor, excluding the handle's own posts, oldest-first (catch-up reads
// chronologically, by id). Unread reads *filter* tombstones (one predicate, in
// unreadBase) rather than masking them: a flat catch-up list has no thread
// shape to preserve. Tree reads — Thread, BoardPosts, ReadBoardTagged — still
// mask, so replies never lose their parent.
func (s *Store) UnreadPosts(boardID uint, handle string, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	if _, err := s.GetAgent(handle); err != nil {
		return nil, err
	}
	cur, err := s.readCursorID(boardID, handle)
	if err != nil {
		return nil, err
	}
	var posts []Post
	err = s.unreadAfterID(boardID, cur, handle).
		Order("id ASC").Limit(limit).Find(&posts).Error
	return posts, err
}

// freshCursorWindow is how far back a handle with no cursor row reads on its
// first catch-up — the same window the session-start digest reports, so the
// two agree instead of the digest saying "4 posts today" and read --unread
// paging through weeks of history.
const freshCursorWindow = 24 * time.Hour

// readCursorID returns the handle's cursor id on the board. A missing row is
// not an error: it is the floor — the last post (live or not; this is a
// position, not a listing) older than freshCursorWindow — and is never
// materialized here, so a handle that never catches up never gets a row.
func (s *Store) readCursorID(boardID uint, handle string) (uint, error) {
	var cur ReadCursor
	err := s.DB.Where("handle = ? AND board_id = ?", handle, boardID).First(&cur).Error
	if err == nil {
		return cur.LastReadID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	var floor uint
	err = s.DB.Model(&Post{}).Where("board_id = ?", boardID).
		Where("strftime('%Y-%m-%d %H:%M:%f', created_at) <= ?", sinceArg(time.Now().Add(-freshCursorWindow))).
		Select("COALESCE(MAX(id), 0)").Scan(&floor).Error
	return floor, err
}

// MarkReadID advances the handle's read cursor on the board to id, never
// backwards: a single busy-retried upsert on the (handle, board_id) unique
// index — no gorm transaction (single-conn pool, see store.go).
func (s *Store) MarkReadID(handle string, boardID uint, id uint) error {
	return withBusyRetry(func() error {
		now := time.Now()
		return s.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "handle"}, {Name: "board_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"last_read_id": gorm.Expr("MAX(COALESCE(last_read_id, 0), ?)", id),
				"last_read_at": now,
			}),
		}).Create(&ReadCursor{Handle: handle, BoardID: boardID, LastReadID: id, LastReadAt: now}).Error
	})
}

// unreadBase is THE definition of "unread" in this package: live
// (untombstoned) posts on the board not authored by excludeHandle (empty =
// no author filter). unreadQuery (time cutoff) and unreadAfterID (id cursor)
// both build on it, so a post can never be unread by one and read by another.
func (s *Store) unreadBase(boardID uint, excludeHandle string) *gorm.DB {
	q := s.DB.Where("board_id = ? AND tombstoned_at IS NULL", boardID)
	if excludeHandle != "" {
		q = q.Where("author_handle <> ?", excludeHandle)
	}
	return q
}

// unreadQuery is the time-cutoff form: unread posts created after since (a
// zero since means no time filter). This is the 24h digest's shape.
func (s *Store) unreadQuery(boardID uint, since time.Time, excludeHandle string) *gorm.DB {
	q := s.unreadBase(boardID, excludeHandle)
	if !since.IsZero() {
		q = q.Where(sinceExpr, sinceArg(since))
	}
	return q
}

// unreadAfterID is the cursor form: unread posts with id > afterID.
func (s *Store) unreadAfterID(boardID, afterID uint, excludeHandle string) *gorm.DB {
	return s.unreadBase(boardID, excludeHandle).Where("id > ?", afterID)
}

// UnreadCount counts unread posts on the board newer than since, optionally
// excluding one author. Empty excludeHandle counts everyone — that is the
// SessionStart digest's 24h board-activity use.
func (s *Store) UnreadCount(boardID uint, since time.Time, excludeHandle string) (int64, error) {
	var n int64
	err := s.unreadQuery(boardID, since, excludeHandle).Model(&Post{}).Count(&n).Error
	return n, err
}

// UnreadCountFor is the cursor-based unread count for handle: everything on
// the board past the handle's read cursor that the handle didn't write — the
// same cursor and predicate as UnreadPosts, so the "N unread" nudge always
// agrees with what read --unread shows.
func (s *Store) UnreadCountFor(boardID uint, handle string) (int64, error) {
	cur, err := s.readCursorID(boardID, handle)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.unreadAfterID(boardID, cur, handle).Model(&Post{}).Count(&n).Error
	return n, err
}

// NewForeignPosts reports live posts with id > afterID not authored by any
// handle in exclude, plus the max such id (0 when there are none). id-based,
// not time-based: ids are autoincrement insertion order (see BoardPosts),
// which makes this a race-free high-water-mark gate for the user-prompt hook.
func (s *Store) NewForeignPosts(boardID uint, afterID uint, exclude []string) (int64, uint, error) {
	q := s.DB.Model(&Post{}).
		Where("board_id = ? AND id > ? AND tombstoned_at IS NULL", boardID, afterID)
	if len(exclude) > 0 {
		q = q.Where("author_handle NOT IN ?", exclude)
	}
	var row struct {
		N   int64
		Max *uint
	}
	if err := q.Select("COUNT(*) AS n, MAX(id) AS max").Scan(&row).Error; err != nil {
		return 0, 0, err
	}
	if row.Max == nil {
		return row.N, 0, nil
	}
	return row.N, *row.Max, nil
}
