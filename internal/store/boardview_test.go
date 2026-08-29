package store

import (
	"testing"
	"time"
)

// BoardPosts is the "everything" fetch behind `xfa threads` / `xfa board`:
// every post on the board, oldest first, tombstones masked (never filtered),
// scoped to the requested board.
func TestBoardPostsReturnsAllMaskedAndOrdered(t *testing.T) {
	s, b, a := seed(t)
	root, _ := s.CreatePost(b.ID, a.Handle, "first root", "", nil)
	time.Sleep(2 * time.Millisecond) // distinct ms (ms-quantized timestamps)
	reply, _ := s.CreatePost(b.ID, a.Handle, "secret reply", "", &root.ID)
	time.Sleep(2 * time.Millisecond)
	s.CreatePost(b.ID, a.Handle, "second root", "", nil)
	if err := s.Tombstone(reply.ID, a.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	posts, err := s.BoardPosts(b.ID)
	if err != nil {
		t.Fatalf("BoardPosts: %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("len = %d, want 3 (tombstones must never be filtered)", len(posts))
	}
	if posts[0].Body != "first root" || posts[2].Body != "second root" {
		t.Errorf("want id ASC order, got %q .. %q", posts[0].Body, posts[2].Body)
	}
	if posts[1].Body != "[deleted]" {
		t.Errorf("tombstoned reply leaked body %q", posts[1].Body)
	}
}

func TestBoardPostsIsBoardScoped(t *testing.T) {
	s, b, a := seed(t)
	other, _ := s.EnsureBoard("elsewhere", "")
	s.CreatePost(b.ID, a.Handle, "mine", "", nil)
	s.CreatePost(other.ID, a.Handle, "not mine", "", nil)

	posts, err := s.BoardPosts(b.ID)
	if err != nil || len(posts) != 1 || posts[0].Body != "mine" {
		t.Fatalf("BoardPosts must be board-scoped: %v %+v", err, posts)
	}
}

// Reviewer scenario: SQLite stores offset-bearing TEXT timestamps, so ORDER BY
// created_at is lexicographic, not chronological — a parent stamped +05:00 can
// sort AFTER its earlier-text reply stamped -04:00, and the grouping loops in
// cmd would then meet the reply before its parent. BoardPosts must order by id
// (AUTOINCREMENT => ids are never reused, so id order is insertion order even
// though posts can now be hard-deleted), which is immune.
func TestBoardPostsOrderSurvivesOffsetInversion(t *testing.T) {
	s, b, a := seed(t)
	parent, _ := s.CreatePost(b.ID, a.Handle, "offset parent", "", nil)
	reply, _ := s.CreatePost(b.ID, a.Handle, "offset reply", "", &parent.ID)
	// Parent: 18:00 UTC but lexicographically "23:..."; reply: 23:00 UTC but
	// lexicographically "19:..." — text order inverts chronological order.
	if err := s.DB.Exec("UPDATE posts SET created_at = '2026-08-19 23:00:00.000+05:00' WHERE id = ?", parent.ID).Error; err != nil {
		t.Fatalf("update parent: %v", err)
	}
	if err := s.DB.Exec("UPDATE posts SET created_at = '2026-08-19 19:00:00.000-04:00' WHERE id = ?", reply.ID).Error; err != nil {
		t.Fatalf("update reply: %v", err)
	}
	posts, err := s.BoardPosts(b.ID)
	if err != nil || len(posts) != 2 {
		t.Fatalf("BoardPosts: %v len=%d", err, len(posts))
	}
	if posts[0].Body != "offset parent" || posts[1].Body != "offset reply" {
		t.Errorf("want insertion (id) order parent->reply, got %q then %q",
			posts[0].Body, posts[1].Body)
	}
}

// BoardPostCounts feeds the TUI's board picker: one grouped query, counting
// every post per board. Tombstoned posts count (they are activity and are
// never filtered — v1 invariant); a board with no posts simply has no map
// entry (callers read the zero value).
func TestBoardPostCountsGroupsPerBoard(t *testing.T) {
	s, b, a := seed(t)
	other, err := s.EnsureBoard("counted", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	empty, err := s.EnsureBoard("empty", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	root, _ := s.CreatePost(b.ID, a.Handle, "one", "", nil)
	doomed, _ := s.CreatePost(b.ID, a.Handle, "two", "", &root.ID)
	s.CreatePost(other.ID, a.Handle, "elsewhere", "", nil)
	if err := s.Tombstone(doomed.ID, a.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	counts, err := s.BoardPostCounts()
	if err != nil {
		t.Fatalf("BoardPostCounts: %v", err)
	}
	if counts[b.ID] != 2 {
		t.Errorf("counts[%d] = %d, want 2 (tombstones count)", b.ID, counts[b.ID])
	}
	if counts[other.ID] != 1 {
		t.Errorf("counts[%d] = %d, want 1", other.ID, counts[other.ID])
	}
	if counts[empty.ID] != 0 {
		t.Errorf("empty board must read as zero, got %d", counts[empty.ID])
	}
}

// ThreadSummaries rolls a whole-board fetch up to per-root summaries: Replies
// is the subtree size minus one (all descendants, not just direct children),
// LastActivity is the newest CreatedAt anywhere in the subtree, and threads
// order most recently active first.
func TestThreadSummariesCountsAndOrdersByActivity(t *testing.T) {
	base := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	r1 := uint(1)
	c1 := uint(3)
	posts := []Post{
		{ID: 1, Body: "alpha root", CreatedAt: base},
		{ID: 2, Body: "beta root", CreatedAt: base.Add(1 * time.Minute)},
		{ID: 3, ParentID: &r1, Body: "alpha child", CreatedAt: base.Add(2 * time.Minute)},
		{ID: 4, ParentID: &c1, Body: "alpha grandchild", CreatedAt: base.Add(3 * time.Minute)},
	}
	threads := ThreadSummaries(posts)
	if len(threads) != 2 {
		t.Fatalf("len = %d, want 2 threads: %+v", len(threads), threads)
	}
	// Alpha is the OLDER root but has the newest activity via a nested
	// grandchild, so it must sort first with the whole-subtree count (2).
	if threads[0].Root.ID != 1 || threads[0].Replies != 2 {
		t.Errorf("thread[0] = root %d with %d replies, want root 1 with 2 (whole subtree)",
			threads[0].Root.ID, threads[0].Replies)
	}
	if !threads[0].LastActivity.Equal(base.Add(3 * time.Minute)) {
		t.Errorf("LastActivity = %v, want the grandchild's %v",
			threads[0].LastActivity, base.Add(3*time.Minute))
	}
	if threads[1].Root.ID != 2 || threads[1].Replies != 0 {
		t.Errorf("thread[1] = root %d with %d replies, want the reply-less root 2",
			threads[1].Root.ID, threads[1].Replies)
	}
	if !threads[1].LastActivity.Equal(base.Add(1 * time.Minute)) {
		t.Errorf("reply-less LastActivity = %v, want its own CreatedAt", threads[1].LastActivity)
	}
}

// Belt-and-braces (reviewer ruling, moved from cmd with the helpers): a reply
// whose parent was not resolved in the fetch "cannot happen" (same-board
// CreatePost validation + id-ordered BoardPosts), but if an ordering
// regression ever produced one, both grouping helpers must surface it as its
// own root — never drop it.
func TestThreadSummariesOrphanBecomesOwnRoot(t *testing.T) {
	absent := uint(99)
	orphan := Post{ID: 7, ParentID: &absent, Body: "orphan", CreatedAt: time.Now()}
	threads := ThreadSummaries([]Post{orphan})
	if len(threads) != 1 || threads[0].Root.ID != 7 || threads[0].Replies != 0 {
		t.Fatalf("orphan must become its own thread, got %+v", threads)
	}
	if threads[0].LastActivity.IsZero() {
		t.Errorf("orphan thread must carry its own activity time")
	}
}

func TestGroupThreadsOrphanBecomesOwnRoot(t *testing.T) {
	absent, r1 := uint(99), uint(1)
	posts := []Post{
		{ID: 1, Body: "root", CreatedAt: time.Now()},
		{ID: 2, ParentID: &r1, Body: "reply", CreatedAt: time.Now()},
		{ID: 7, ParentID: &absent, Body: "orphan", CreatedAt: time.Now()},
	}
	groups := GroupThreads(posts)
	if len(groups) != 2 {
		t.Fatalf("orphan must form its own group, got %d groups: %+v", len(groups), groups)
	}
	if len(groups[0]) != 2 || groups[0][0].ID != 1 {
		t.Errorf("first group must be root+reply, got %+v", groups[0])
	}
	if len(groups[1]) != 1 || groups[1][0].ID != 7 {
		t.Errorf("second group must be the orphan alone, got %+v", groups[1])
	}
}

// GroupThreads keeps roots in input (chronological/insertion) order and each
// thread's posts parents-before-children — render.Depths' precondition.
func TestGroupThreadsKeepsRootOrderAndParentFirst(t *testing.T) {
	r1, r2 := uint(1), uint(2)
	posts := []Post{
		{ID: 1, Body: "first root"},
		{ID: 2, Body: "second root"},
		{ID: 3, ParentID: &r1, Body: "reply to first"},
		{ID: 4, ParentID: &r2, Body: "reply to second"},
		{ID: 5, ParentID: &r1, Body: "another reply to first"},
	}
	groups := GroupThreads(posts)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
	if groups[0][0].ID != 1 || groups[1][0].ID != 2 {
		t.Errorf("roots must keep input order, got %d then %d", groups[0][0].ID, groups[1][0].ID)
	}
	if len(groups[0]) != 3 || groups[0][1].ID != 3 || groups[0][2].ID != 5 {
		t.Errorf("first group must be parent-first 1,3,5, got %+v", groups[0])
	}
	if len(groups[1]) != 2 || groups[1][1].ID != 4 {
		t.Errorf("second group must be 2,4, got %+v", groups[1])
	}
}
