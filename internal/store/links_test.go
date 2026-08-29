package store

import "testing"

func TestPostRefIDs(t *testing.T) {
	cases := []struct {
		body string
		want []uint
	}{
		{"see #12 and #7", []uint{12, 7}},
		{"dup #5 #5", []uint{5}},
		{"@crimson-otter-7 is a handle, not a ref", nil},
		{"@12 is a handle sigil, not a post ref", nil},
		{"no refs here", nil},
		{"#0 is not a valid id", nil},
		{"#12abc is not a ref", nil},
		// The preceding-char guard: a '#' glued to a word char, '&', '/' or
		// another '#' belongs to something that is not a post reference.
		{"&#123;", nil},
		{"https://x.com/a#12", nil},
		{"https://x.com/#12", nil},
		{"##12", nil},
		{"issue#12 is a decorated identifier", nil},
		{"#12", []uint{12}},                           // start of string is fine
		{"first line\n#12 at line start", []uint{12}}, // so is start of line
		{"see #12.", []uint{12}},
		{"(#12) and [#7]", []uint{12, 7}},
	}
	for _, c := range cases {
		got := PostRefIDs(c.body)
		if len(got) != len(c.want) {
			t.Fatalf("PostRefIDs(%q) = %v, want %v", c.body, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("PostRefIDs(%q) = %v, want %v", c.body, got, c.want)
			}
		}
	}
}

func TestLinksWrittenOnCreate(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b1", "")
	b2, _ := s.EnsureBoard("b2", "")
	a, _ := s.RegisterAgent("claude", "sess-1", "")
	target, err := s.CreatePost(b.ID, a.Handle, "the original finding", "", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	reply, _ := s.CreatePost(b.ID, a.Handle, "a reply under it", "", &target.ID)
	// Cross-board link to a root, link to a reply, dangling ref, dup ref.
	src, err := s.CreatePost(b2.ID, a.Handle,
		"see #1 and #2 and #2 and the missing #999", "", nil)
	if err != nil {
		t.Fatalf("CreatePost source: %v", err)
	}
	ls, err := s.LinksFor([]uint{src.ID, target.ID, reply.ID})
	if err != nil {
		t.Fatalf("LinksFor: %v", err)
	}
	out := ls.Out[src.ID]
	if len(out) != 2 {
		t.Fatalf("outbound links = %v, want 2 (dangling #999 skipped, #2 deduped)", out)
	}
	if out[0].PostID != target.ID || out[0].BoardSlug != "b1" || out[0].ThreadID != target.ID {
		t.Fatalf("link to root wrong: %+v", out[0])
	}
	if out[1].PostID != reply.ID || out[1].ThreadID != target.ID {
		t.Fatalf("link to reply must carry the root as ThreadID: %+v", out[1])
	}
	if in := ls.In[target.ID]; len(in) != 1 || in[0].PostID != src.ID || in[0].BoardSlug != "b2" {
		t.Fatalf("backlink wrong: %v", ls.In[target.ID])
	}
	if got := ls.Out[target.ID]; len(got) != 0 {
		t.Fatalf("target has no outbound links, got %v", got)
	}
}

func TestHardDeleteCascadesLinks(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b1", "")
	a, _ := s.RegisterAgent("claude", "sess-1", "")
	target, _ := s.CreatePost(b.ID, a.Handle, "target", "", nil)
	src, _ := s.CreatePost(b.ID, a.Handle, "points at #1", "", nil)
	if _, err := s.HardDeletePost(src.ID); err != nil {
		t.Fatalf("HardDeletePost: %v", err)
	}
	ls, _ := s.LinksFor([]uint{target.ID})
	if len(ls.In[target.ID]) != 0 {
		t.Fatalf("backlink survived source hard delete: %v", ls.In[target.ID])
	}
	// And the other direction: deleting the target reaps rows where it is target.
	src2, _ := s.CreatePost(b.ID, a.Handle, "also #1", "", nil)
	if _, err := s.HardDeletePost(target.ID); err != nil {
		t.Fatalf("HardDeletePost target: %v", err)
	}
	var n int64
	s.DB.Raw(`SELECT COUNT(*) FROM post_links`).Scan(&n)
	if n != 0 {
		t.Fatalf("post_links rows survived, n=%d (src2=%d)", n, src2.ID)
	}
}

func TestTombstoneKeepsLinks(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b1", "")
	a, _ := s.RegisterAgent("claude", "sess-1", "")
	target, _ := s.CreatePost(b.ID, a.Handle, "target", "", nil)
	src, _ := s.CreatePost(b.ID, a.Handle, "see #1", "", nil)
	if err := s.Tombstone(target.ID, a.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}
	ls, _ := s.LinksFor([]uint{src.ID})
	if len(ls.Out[src.ID]) != 1 {
		t.Fatalf("tombstoned target must keep its links: %v", ls.Out[src.ID])
	}
}
