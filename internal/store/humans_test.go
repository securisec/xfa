package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func humanFixture(t *testing.T) (*Store, *Board, *Agent, *Agent) {
	t.Helper()
	s := openTemp(t)
	b, err := s.EnsureBoard("b1", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	human, _ := s.RegisterAgent("human", "web", "")
	agent, _ := s.RegisterAgent("claude", "sess-1", "")
	return s, b, human, agent
}

func TestAuthorsFor(t *testing.T) {
	s, b, human, agent := humanFixture(t)
	dir := t.TempDir()
	if err := s.RegisterProject(dir, b.ID); err != nil {
		t.Fatal(err)
	}
	inProj, err := s.RegisterAgentAt(dir, "claude", "sess-p", "")
	if err != nil {
		t.Fatal(err)
	}
	handles := []string{human.Handle, agent.Handle, inProj.Handle, "ghost-handle-9"}

	// one project: human flag works, paths gated off
	m, err := s.AuthorsFor(handles)
	if err != nil {
		t.Fatalf("AuthorsFor: %v", err)
	}
	if !m[human.Handle].Human || m[agent.Handle].Human || m["ghost-handle-9"].Human {
		t.Fatalf("wrong human set: %v", m)
	}
	if m[inProj.Handle].ProjectPath != "" {
		t.Fatalf("single-project DB must gate the path off, got %q", m[inProj.Handle].ProjectPath)
	}

	// second project opens the gate
	if err := s.RegisterProject(t.TempDir(), b.ID); err != nil {
		t.Fatal(err)
	}
	m, _ = s.AuthorsFor(handles)
	if got := m[inProj.Handle].ProjectPath; got != NormalizePath(dir) {
		t.Fatalf("ProjectPath = %q, want %q", got, NormalizePath(dir))
	}
	if m[inProj.Handle].Project() != filepath.Base(NormalizePath(dir)) {
		t.Fatalf("Project() = %q", m[inProj.Handle].Project())
	}
	if m[agent.Handle].ProjectPath != "" || m[human.Handle].ProjectPath != "" {
		t.Fatalf("agents without a project must stay empty: %v", m)
	}
	if m2, _ := s.AuthorsFor(nil); m2 == nil {
		t.Fatal("empty input must return a non-nil map")
	}
}

func TestReadBoardHuman(t *testing.T) {
	s, b, human, agent := humanFixture(t)
	hp, _ := s.CreatePost(b.ID, human.Handle, "from the human", "", nil)
	_, _ = s.CreatePost(b.ID, agent.Handle, "from an agent", "", nil)
	posts, err := s.ReadBoardHuman(b.ID, "", time.Time{}, 20)
	if err != nil {
		t.Fatalf("ReadBoardHuman: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != hp.ID {
		t.Fatalf("want only the human post, got %v", posts)
	}
}

func TestUnaddressedHumanCount(t *testing.T) {
	s, b, human, agent := humanFixture(t)
	p1, _ := s.CreatePost(b.ID, human.Handle, "q one", "", nil)
	p2, _ := s.CreatePost(b.ID, human.Handle, "q two", "", nil)
	p3, _ := s.CreatePost(b.ID, human.Handle, "q three", "", nil)
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 3 {
		t.Fatalf("all three unaddressed, got %d", n)
	}
	// A human replying to themselves does not address the parent, and the
	// self-reply is itself a human-authored post with no non-human direct
	// reply yet — it counts too (replies are not exempt from the spec).
	selfReply, _ := s.CreatePost(b.ID, human.Handle, "self reply", "", &p1.ID)
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 4 {
		t.Fatalf("self-reply adds to the count without addressing p1, got %d", n)
	}
	// An agent's direct reply addresses p1; the self-reply remains unaddressed.
	_, _ = s.CreatePost(b.ID, agent.Handle, "on it", "", &p1.ID)
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 3 {
		t.Fatalf("agent reply must address p1 only, got %d", n)
	}
	// Resolving addresses it (any tag state — resolved_at is the signal).
	if err := s.Resolve(p2.ID, agent.Handle); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 2 {
		t.Fatalf("resolved_at must address p2, got %d", n)
	}
	// Tombstoned human posts drop out entirely; the self-reply remains.
	_ = s.Tombstone(p3.ID, human.Handle)
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 1 {
		t.Fatalf("tombstone must remove p3 leaving only the self-reply, got %d", n)
	}
	// An agent directly replying to the self-reply finally addresses it.
	_, _ = s.CreatePost(b.ID, agent.Handle, "got the self-reply too", "", &selfReply.ID)
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 0 {
		t.Fatalf("agent reply must address the self-reply, got %d", n)
	}
}

func TestAsksForHuman(t *testing.T) {
	s, b, human, agent := humanFixture(t)
	ids := func(t *testing.T, boardID uint) []uint {
		t.Helper()
		posts, err := s.AsksForHuman(boardID)
		if err != nil {
			t.Fatalf("AsksForHuman: %v", err)
		}
		var out []uint
		for _, p := range posts {
			out = append(out, p.ID)
		}
		return out
	}
	// Plain question without @human is a peer question, not an ask.
	_, _ = s.CreatePost(b.ID, agent.Handle, "peer q?", "question", nil)
	top, _ := s.CreatePost(b.ID, agent.Handle, "@human which env?", "question", nil)
	if got := ids(t, b.ID); !reflect.DeepEqual(got, []uint{top.ID}) {
		t.Fatalf("top-level ask: got %v", got)
	}
	// A reply carrying @human is an ask too (any depth).
	rep, _ := s.CreatePost(b.ID, agent.Handle, "@human ok to drop the table?", "", &top.ID)
	if got := ids(t, b.ID); !reflect.DeepEqual(got, []uint{rep.ID, top.ID}) {
		t.Fatalf("reply ask, id DESC: got %v", got)
	}
	// A peer (non-human) reply does NOT clear an ask.
	_, _ = s.CreatePost(b.ID, agent.Handle, "I'd say prod", "", &top.ID)
	if got := ids(t, b.ID); len(got) != 2 {
		t.Fatalf("peer reply must not clear: got %v", got)
	}
	// The human's direct reply clears it.
	_, _ = s.CreatePost(b.ID, human.Handle, "staging", "", &top.ID)
	if got := ids(t, b.ID); !reflect.DeepEqual(got, []uint{rep.ID}) {
		t.Fatalf("human reply must clear top: got %v", got)
	}
	// Resolving clears it (the asker's path for a top-level question).
	top2, _ := s.CreatePost(b.ID, agent.Handle, "@human second?", "question", nil)
	if err := s.Resolve(top2.ID, agent.Handle); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, b.ID); !reflect.DeepEqual(got, []uint{rep.ID}) {
		t.Fatalf("resolve must clear: got %v", got)
	}
	// Tombstoned asks are excluded.
	if err := s.Tombstone(rep.ID, agent.Handle); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, b.ID); got != nil {
		t.Fatalf("tombstoned must be excluded: got %v", got)
	}
	// A human typing @human is not a self-ask.
	_, _ = s.CreatePost(b.ID, human.Handle, "@human note to self", "", nil)
	if got := ids(t, b.ID); got != nil {
		t.Fatalf("human-authored must be excluded: got %v", got)
	}
	// boardID 0 spans boards.
	b2, _ := s.EnsureBoard("b2", "")
	other, _ := s.CreatePost(b2.ID, agent.Handle, "@human cross-board?", "question", nil)
	if got := ids(t, b.ID); got != nil {
		t.Fatalf("board filter leaked: got %v", got)
	}
	if got := ids(t, 0); !reflect.DeepEqual(got, []uint{other.ID}) {
		t.Fatalf("boardID 0 must span boards: got %v", got)
	}
}

func TestPostsByAuthor(t *testing.T) {
	s, b, human, agent := humanFixture(t)
	b2, err := s.EnsureBoard("b2", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	p1, _ := s.CreatePost(b.ID, human.Handle, "human top-level", "", nil)
	p2, _ := s.CreatePost(b.ID, human.Handle, "human reply", "", &p1.ID)
	_, _ = s.CreatePost(b.ID, agent.Handle, "from an agent", "", nil)
	p4, _ := s.CreatePost(b2.ID, human.Handle, "human elsewhere", "", nil)

	posts, err := s.PostsByAuthor(b.ID, human.Handle, 20)
	if err != nil {
		t.Fatalf("PostsByAuthor: %v", err)
	}
	if len(posts) != 2 || posts[0].ID != p2.ID || posts[1].ID != p1.ID {
		t.Fatalf("board-scoped wants p2,p1 newest first, got %v", posts)
	}

	all, err := s.PostsByAuthor(0, human.Handle, 20)
	if err != nil {
		t.Fatalf("PostsByAuthor(0): %v", err)
	}
	if len(all) != 3 || all[0].ID != p4.ID {
		t.Fatalf("all boards wants 3 newest-first, got %v", all)
	}

	// A tombstoned own post stays in the list, masked.
	if err := s.Tombstone(p2.ID, human.Handle); err != nil {
		t.Fatal(err)
	}
	posts, _ = s.PostsByAuthor(b.ID, human.Handle, 20)
	if len(posts) != 2 || posts[0].ID != p2.ID || posts[0].Body != "[deleted]" {
		t.Fatalf("tombstoned post must survive masked, got %v", posts)
	}
}
