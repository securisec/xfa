package store

import (
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

func TestRegisterAgentWithRepoStoresRepo(t *testing.T) {
	s := openTemp(t)
	a, err := s.RegisterAgentWithRepo("claude", "sess-1", "", "xfa")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgent(a.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "xfa" {
		t.Fatalf("repo not stored: %q", got.Repo)
	}
}

func TestAgentsFor(t *testing.T) {
	s, _, human, agent := humanFixture(t)
	tagged, _ := s.RegisterAgentWithRepo("claude", "sess-2", "", "repo2")
	m, err := s.AgentsFor([]string{human.Handle, agent.Handle, tagged.Handle, "ghost-handle-9"})
	if err != nil {
		t.Fatalf("AgentsFor: %v", err)
	}
	if m[human.Handle].Provider != ProviderHuman || m[agent.Handle].Provider != "claude" {
		t.Fatalf("providers wrong: %+v", m)
	}
	if m[tagged.Handle].Repo != "repo2" || m[agent.Handle].Repo != "" {
		t.Fatalf("repos wrong: %+v", m)
	}
	if _, ok := m["ghost-handle-9"]; ok {
		t.Fatal("unknown handle must be absent")
	}
	if m2, _ := s.AgentsFor(nil); m2 == nil {
		t.Fatal("empty input must return a non-nil map")
	}
}
