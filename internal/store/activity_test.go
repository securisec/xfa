package store

import (
	"path/filepath"
	"testing"
)

// PRAGMA data_version is the activity poller's idle gate: it must move when
// ANOTHER connection commits and stay put across idle reads on the same
// connection. Two Opens on one path = two connections (SetMaxOpenConns(1)).
func TestDataVersionTracksForeignCommits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	a, err := Open(path)
	if err != nil {
		t.Fatalf("Open a: %v", err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open b: %v", err)
	}
	v0, err := b.DataVersion()
	if err != nil {
		t.Fatalf("DataVersion: %v", err)
	}
	for i := 0; i < 3; i++ {
		v, err := b.DataVersion()
		if err != nil {
			t.Fatalf("DataVersion idle: %v", err)
		}
		if v != v0 {
			t.Fatalf("idle read moved data_version %d -> %d", v0, v)
		}
	}
	if _, err := a.EnsureBoard("dv", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	v1, err := b.DataVersion()
	if err != nil {
		t.Fatalf("DataVersion after write: %v", err)
	}
	if v1 == v0 {
		t.Fatalf("foreign commit did not move data_version (still %d)", v0)
	}
	// own-connection writes do NOT move it — the reason the poller needs its
	// own store instead of sharing the web handlers' connection.
	if _, err := b.EnsureBoard("dv2", ""); err != nil {
		t.Fatalf("EnsureBoard b: %v", err)
	}
	v2, err := b.DataVersion()
	if err != nil {
		t.Fatalf("DataVersion after own write: %v", err)
	}
	if v2 != v1 {
		t.Errorf("own commit moved data_version %d -> %d", v1, v2)
	}
}

func TestPostsAfterAndMarkedPostIDs(t *testing.T) {
	s := openTemp(t)
	b1, _ := s.EnsureBoard("one", "")
	b2, _ := s.EnsureBoard("two", "")
	a, err := s.RegisterAgent("claude", "sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	q, _ := s.CreatePost(b1.ID, a.Handle, "q?", "question", nil)
	r, _ := s.CreatePost(b1.ID, a.Handle, "reply", "", &q.ID)
	p2, _ := s.CreatePost(b2.ID, a.Handle, "other board", "", nil)
	if err := s.Tombstone(r.ID, a.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}
	if err := s.Resolve(q.ID, a.Handle); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	posts, err := s.PostsAfter(int64(q.ID))
	if err != nil {
		t.Fatalf("PostsAfter: %v", err)
	}
	if len(posts) != 2 || posts[0].ID != r.ID || posts[1].ID != p2.ID {
		t.Fatalf("PostsAfter(%d) = %+v, want [%d %d] across boards in id order", q.ID, posts, r.ID, p2.ID)
	}
	if posts[0].Body != "[deleted]" {
		t.Errorf("tombstoned post must be masked, got %q", posts[0].Body)
	}
	if all, _ := s.PostsAfter(0); len(all) != 3 {
		t.Errorf("PostsAfter(0) = %d posts, want 3", len(all))
	}

	marks, err := s.MarkedPostIDs()
	if err != nil {
		t.Fatalf("MarkedPostIDs: %v", err)
	}
	if len(marks) != 2 {
		t.Fatalf("MarkedPostIDs = %+v, want 2 rows", marks)
	}
	got := map[uint]PostMark{}
	for _, m := range marks {
		got[m.ID] = m
	}
	if m := got[q.ID]; !m.Resolved || m.Tombstoned {
		t.Errorf("post %d mark = %+v, want resolved only", q.ID, m)
	}
	if m := got[r.ID]; m.Resolved || !m.Tombstoned {
		t.Errorf("post %d mark = %+v, want tombstoned only", r.ID, m)
	}
}
