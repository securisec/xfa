package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReadBoardMasksTombstones(t *testing.T) {
	s, b, a := seed(t)
	p, _ := s.CreatePost(b.ID, a.Handle, "secret", "", nil)
	s.Tombstone(p.ID, a.Handle)
	posts, err := s.ReadBoard(b.ID, time.Time{}, 10)
	if err != nil || len(posts) != 1 {
		t.Fatalf("ReadBoard: %v len=%d", err, len(posts))
	}
	if posts[0].Body != "[deleted]" {
		t.Errorf("tombstone leaked body %q", posts[0].Body)
	}
}

func TestThreadReturnsWholeSubtree(t *testing.T) {
	s, b, a := seed(t)
	root, _ := s.CreatePost(b.ID, a.Handle, "root", "", nil)
	r1, _ := s.CreatePost(b.ID, a.Handle, "child", "", &root.ID)
	s.CreatePost(b.ID, a.Handle, "grandchild", "", &r1.ID)
	s.CreatePost(b.ID, a.Handle, "unrelated", "", nil)
	posts, err := s.Thread(root.ID)
	if err != nil || len(posts) != 3 {
		t.Fatalf("Thread: %v len=%d want 3", err, len(posts))
	}
}

// Pins the timezone-safe cutoff comparison: SQLite stores offset-bearing TEXT
// timestamps, so a raw lexicographic `created_at > ?` against a UTC cutoff
// misses every post in negative-offset zones. Both queries must match a post
// created after a UTC cutoff.
func TestSinceCutoffIsTimezoneSafe(t *testing.T) {
	s, b, a := seed(t)
	cutoff := time.Now().UTC()
	// The cutoff comparison is quantized to milliseconds (strftime %f), so
	// step past the cutoff's millisecond before posting.
	time.Sleep(2 * time.Millisecond)
	if _, err := s.CreatePost(b.ID, a.Handle, "after cutoff", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	posts, err := s.ReadBoard(b.ID, cutoff, 10)
	if err != nil || len(posts) != 1 {
		t.Fatalf("ReadBoard with UTC cutoff: %v len=%d want 1", err, len(posts))
	}
	n, err := s.UnreadCount(b.ID, cutoff, "")
	if err != nil || n != 1 {
		t.Fatalf("UnreadCount with UTC cutoff = %d, %v; want 1", n, err)
	}
}

func TestUnreadCount(t *testing.T) {
	s, b, a := seed(t)
	s.CreatePost(b.ID, a.Handle, "old", "", nil)
	// The cutoff comparison is quantized to milliseconds (strftime %f):
	// keep "old", the cutoff, and "new" in distinct milliseconds.
	time.Sleep(2 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(2 * time.Millisecond)
	s.CreatePost(b.ID, a.Handle, "new", "", nil)
	n, err := s.UnreadCount(b.ID, cutoff, "")
	if err != nil || n != 1 {
		t.Fatalf("UnreadCount = %d, %v; want 1", n, err)
	}
}

func TestUnreadCursor(t *testing.T) {
	s, b, a := seed(t)
	reader, _ := s.RegisterAgent("claude", "reader-sess", "")
	before, _ := s.CreatePost(b.ID, a.Handle, "before mark", "", nil)
	if err := s.MarkReadID(reader.Handle, b.ID, before.ID); err != nil {
		t.Fatal(err)
	}
	s.CreatePost(b.ID, a.Handle, "after mark", "", nil)
	s.CreatePost(b.ID, reader.Handle, "own post", "", nil)

	unread, err := s.UnreadPosts(b.ID, reader.Handle, 10)
	if err != nil || len(unread) != 1 || unread[0].Body != "after mark" {
		t.Fatalf("unread: %v %+v", err, unread)
	}
	if _, err := s.UnreadPosts(b.ID, "ghost-vole-9", 10); !errors.Is(err, ErrNoAgent) {
		t.Errorf("unknown handle must surface ErrNoAgent, got %v", err)
	}
}

func TestUnreadNoCursorMeansRecent(t *testing.T) {
	s, b, a := seed(t)
	fresh, _ := s.RegisterAgent("claude", "fresh-sess", "")
	s.CreatePost(b.ID, a.Handle, "old news", "", nil)
	unread, err := s.UnreadPosts(b.ID, fresh.Handle, 10)
	if err != nil || len(unread) != 1 {
		t.Fatalf("no cursor row should mean everything in the 24h window unread: %v len=%d", err, len(unread))
	}
}

// Pins I3: cursors are per (handle, board) — marking one board read must not
// consume unread posts on another board.
func TestUnreadCursorsArePerBoard(t *testing.T) {
	s, bA, a := seed(t)
	bB, _ := s.EnsureBoard("other", "")
	reader, _ := s.RegisterAgent("claude", "xboard-sess", "")
	s.CreatePost(bA.ID, a.Handle, "on A", "", nil)
	onB, _ := s.CreatePost(bB.ID, a.Handle, "on B", "", nil)

	if err := s.MarkReadID(reader.Handle, bB.ID, onB.ID); err != nil {
		t.Fatal(err)
	}
	unreadA, err := s.UnreadPosts(bA.ID, reader.Handle, 10)
	if err != nil || len(unreadA) != 1 || unreadA[0].Body != "on A" {
		t.Fatalf("unread on A must survive a mark-read on B: %v %+v", err, unreadA)
	}
	unreadB, err := s.UnreadPosts(bB.ID, reader.Handle, 10)
	if err != nil || len(unreadB) != 0 {
		t.Fatalf("B should be caught up: %v %+v", err, unreadB)
	}
}

// Pins M1: catch-up reads chronologically — oldest first.
func TestUnreadOldestFirst(t *testing.T) {
	s, b, a := seed(t)
	reader, _ := s.RegisterAgent("claude", "order-sess", "")
	s.CreatePost(b.ID, a.Handle, "first", "", nil)
	s.CreatePost(b.ID, a.Handle, "second", "", nil)
	unread, err := s.UnreadPosts(b.ID, reader.Handle, 10)
	if err != nil || len(unread) != 2 {
		t.Fatalf("unread: %v %+v", err, unread)
	}
	if unread[0].Body != "first" || unread[1].Body != "second" {
		t.Errorf("want oldest first, got %q then %q", unread[0].Body, unread[1].Body)
	}
}

// Pins the single definition of "unread": live posts on the board, optionally
// past a cutoff, optionally excluding one author. Tombstones never count;
// self-exclusion is opt-in via excludeHandle.
func TestUnreadCountExcludesSelfAndTombstones(t *testing.T) {
	s, b, other := seed(t)
	me, err := s.RegisterAgent("claude", "me-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, other.Handle, "one", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, other.Handle, "two", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, me.Handle, "mine", "", nil); err != nil {
		t.Fatal(err)
	}
	dead, err := s.CreatePost(b.ID, other.Handle, "gone", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(dead.ID, other.Handle); err != nil {
		t.Fatal(err)
	}

	n, err := s.UnreadCount(b.ID, time.Time{}, "")
	if err != nil || n != 3 {
		t.Fatalf("UnreadCount(all authors) = %d, %v; want 3 (tombstone excluded)", n, err)
	}
	n, err = s.UnreadCount(b.ID, time.Time{}, me.Handle)
	if err != nil || n != 2 {
		t.Fatalf("UnreadCount(excluding self) = %d, %v; want 2", n, err)
	}
}

func TestUnreadCountForUsesCursorAndExcludesSelf(t *testing.T) {
	s, b, other := seed(t)
	me, err := s.RegisterAgent("claude", "me-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.RegisterAgent("claude", "fresh-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	seen, err := s.CreatePost(b.ID, other.Handle, "seen", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReadID(me.Handle, b.ID, seen.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, me.Handle, "my own", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, other.Handle, "theirs", "", nil); err != nil {
		t.Fatal(err)
	}

	n, err := s.UnreadCountFor(b.ID, me.Handle)
	if err != nil || n != 1 {
		t.Fatalf("UnreadCountFor(me) = %d, %v; want 1 (own post never unread)", n, err)
	}
	// No cursor row at all: the whole 24h window is unread except the reader's own.
	n, err = s.UnreadCountFor(b.ID, fresh.Handle)
	if err != nil || n != 3 {
		t.Fatalf("UnreadCountFor(never read) = %d, %v; want 3", n, err)
	}
}

func TestNewForeignPosts(t *testing.T) {
	s, b, other := seed(t)
	me, err := s.RegisterAgent("claude", "me-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, me.Handle, "mine", "", nil); err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreatePost(b.ID, other.Handle, "theirs", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p3, err := s.CreatePost(b.ID, other.Handle, "deleted theirs", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(p3.ID, other.Handle); err != nil {
		t.Fatal(err)
	}

	n, maxID, err := s.NewForeignPosts(b.ID, 0, []string{me.Handle})
	if err != nil || n != 1 || maxID != p2.ID {
		t.Fatalf("NewForeignPosts(from 0) = %d, %d, %v; want 1, %d, nil", n, maxID, err, p2.ID)
	}
	n, maxID, err = s.NewForeignPosts(b.ID, p2.ID, []string{me.Handle})
	if err != nil || n != 0 || maxID != 0 {
		t.Fatalf("NewForeignPosts(from p2) = %d, %d, %v; want 0, 0, nil", n, maxID, err)
	}
}

// Thread accepts any post in a thread, not just the root: it resolves to the
// root first and returns the whole thread from there.
func TestThreadFromReplyIDReturnsWholeThread(t *testing.T) {
	s, b, a := seed(t)
	a2, _ := s.RegisterAgent("claude", "sess-2", "")
	root, _ := s.CreatePost(b.ID, a.Handle, "q", "question", nil)
	r1, _ := s.CreatePost(b.ID, a2.Handle, "a", "", &root.ID)
	r2, _ := s.CreatePost(b.ID, a.Handle, "thanks", "", &r1.ID)
	posts, err := s.Thread(r2.ID)
	if err != nil || len(posts) != 3 || posts[0].ID != root.ID {
		t.Fatalf("thread from a reply id must start at the root: %v %+v", err, posts)
	}
	if id, err := s.RootOf(r2.ID); err != nil || id != root.ID {
		t.Fatalf("RootOf = %d, %v", id, err)
	}
	if _, err := s.RootOf(9999); !errors.Is(err, ErrNoPost) {
		t.Fatalf("RootOf(missing) = %v, want ErrNoPost", err)
	}
}

// created_at is offset-bearing TEXT, so ORDER BY created_at is lexical and a
// +09:00 root sorts after its UTC reply; id order is insertion order.
func TestThreadOrdersByIDUnderTZSkew(t *testing.T) {
	s, b, a := seed(t)
	root, reply := seedTZSkew(t, s, b.ID, a.Handle, a.Handle)
	if err := s.DB.Model(&reply).Update("parent_id", root.ID).Error; err != nil {
		t.Fatal(err)
	}
	posts, err := s.Thread(root.ID)
	if err != nil || len(posts) != 2 || posts[0].ID != root.ID {
		t.Fatalf("root must precede its reply: %v %+v", err, posts)
	}
}

// seedTZSkew inserts two posts whose TEXT timestamps sort backwards from
// their true order (a +09:00 stamp lexically after a later +00:00 stamp).
// id order is commit order; created_at text order is not.
func seedTZSkew(t *testing.T, s *Store, boardID uint, a, b string) (older, newer Post) {
	t.Helper()
	tokyo := time.FixedZone("JST", 9*3600)
	base := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	older = Post{BoardID: boardID, AuthorHandle: a, Body: "older", CreatedAt: base.In(tokyo)}
	newer = Post{BoardID: boardID, AuthorHandle: b, Body: "newer", CreatedAt: base.Add(time.Second)}
	if err := s.DB.Create(&older).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&newer).Error; err != nil {
		t.Fatal(err)
	}
	return older, newer
}

func TestReadBoardOrdersByIDNotText(t *testing.T) {
	s, b, a := seed(t)
	a2, _ := s.RegisterAgent("claude", "tz-sess", "")
	older, newer := seedTZSkew(t, s, b.ID, a.Handle, a2.Handle)
	posts, err := s.ReadBoard(b.ID, time.Time{}, 10)
	if err != nil || len(posts) != 2 {
		t.Fatalf("ReadBoard: %v %+v", err, posts)
	}
	if posts[0].ID != newer.ID || posts[1].ID != older.ID {
		t.Fatalf("newest-first by id, got %d,%d", posts[0].ID, posts[1].ID)
	}
}

func TestUnreadCursorDoesNotLoseTZSkewedPost(t *testing.T) {
	s, b, a := seed(t)
	a2, _ := s.RegisterAgent("claude", "tz-sess", "")
	older, newer := seedTZSkew(t, s, b.ID, a.Handle, a2.Handle)
	reader, _ := s.RegisterAgent("claude", "reader-sess", "")
	got, err := s.UnreadPosts(b.ID, reader.Handle, 1)
	if err != nil || len(got) != 1 || got[0].ID != older.ID {
		t.Fatalf("first unread must be the older post by id, got %v %+v", err, got)
	}
	if err := s.MarkReadID(reader.Handle, b.ID, got[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err = s.UnreadPosts(b.ID, reader.Handle, 1)
	if err != nil || len(got) != 1 || got[0].ID != newer.ID {
		t.Fatalf("second unread must be the newer post, got %v %+v", err, got)
	}
	if err := s.MarkReadID(reader.Handle, b.ID, got[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.UnreadCountFor(b.ID, reader.Handle); err != nil || n != 0 {
		t.Fatalf("caught up, got %d %v", n, err)
	}
}

// A post stamped BEFORE the cursor's wall-clock but committed AFTER it (gorm
// stamps in Go, the commit lands later under busy-retry) must still be unread.
func TestUnreadCursorIgnoresStaleTimestampOfLateCommit(t *testing.T) {
	s, b, a := seed(t)
	reader, _ := s.RegisterAgent("claude", "reader-sess", "")
	first, err := s.CreatePost(b.ID, a.Handle, "first", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReadID(reader.Handle, b.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	late := Post{BoardID: b.ID, AuthorHandle: a.Handle, Body: "late", CreatedAt: time.Now().Add(-time.Hour)}
	if err := s.DB.Create(&late).Error; err != nil {
		t.Fatal(err)
	}
	got, err := s.UnreadPosts(b.ID, reader.Handle, 10)
	if err != nil || len(got) != 1 || got[0].ID != late.ID {
		t.Fatalf("late-committed post must be unread, got %v %+v", err, got)
	}
}

func TestMarkReadIDNeverMovesBackwards(t *testing.T) {
	s, b, _ := seed(t)
	reader, _ := s.RegisterAgent("claude", "reader-sess", "")
	if err := s.MarkReadID(reader.Handle, b.ID, 9); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReadID(reader.Handle, b.ID, 4); err != nil {
		t.Fatal(err)
	}
	id, err := s.readCursorID(b.ID, reader.Handle)
	if err != nil || id != 9 {
		t.Fatalf("cursor regressed to %d (%v)", id, err)
	}
}

func TestFreshHandleFloorIs24h(t *testing.T) {
	s, b, a := seed(t)
	reader, _ := s.RegisterAgent("claude", "reader-sess", "")
	old := Post{BoardID: b.ID, AuthorHandle: a.Handle, Body: "ancient", CreatedAt: time.Now().Add(-48 * time.Hour)}
	if err := s.DB.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	recent, err := s.CreatePost(b.ID, a.Handle, "recent", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UnreadPosts(b.ID, reader.Handle, 10)
	if err != nil || len(got) != 1 || got[0].ID != recent.ID {
		t.Fatalf("fresh handle sees only the last 24h, got %v %+v", err, got)
	}
	if n, err := s.UnreadCountFor(b.ID, reader.Handle); err != nil || n != 1 {
		t.Fatalf("UnreadCountFor must use the same floor, got %d %v", n, err)
	}
	// The floor is not materialized: no cursor row was written.
	var n int64
	s.DB.Model(&ReadCursor{}).Where("handle = ?", reader.Handle).Count(&n)
	if n != 0 {
		t.Fatal("floor must not write a cursor row")
	}
}

// A corrupt parent cycle (only reachable via raw SQL on a shared DB) must
// error from RootOf, never hang the caller.
func TestRootOfBoundsCorruptCycle(t *testing.T) {
	s, b, a := seed(t)
	p1, _ := s.CreatePost(b.ID, a.Handle, "one", "", nil)
	p2, _ := s.CreatePost(b.ID, a.Handle, "two", "", &p1.ID)
	if err := s.DB.Exec("UPDATE posts SET parent_id = ? WHERE id = ?", p2.ID, p1.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.RootOf(p2.ID); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("RootOf on a cycle = %v, want cycle error", err)
	}
}
