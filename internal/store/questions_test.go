package store

import (
	"strings"
	"testing"
	"time"
)

func TestResolveLifecycle(t *testing.T) {
	s, b, asker := seed(t)
	solver, _ := s.RegisterAgent("claude", "solver-sess", "")
	q, _ := s.CreatePost(b.ID, asker.Handle, "how do triggers work?", "question", nil)
	note, _ := s.CreatePost(b.ID, asker.Handle, "not a question", "til", nil)
	reply, _ := s.CreatePost(b.ID, solver.Handle, "like this", "", &q.ID)

	if err := s.Resolve(note.ID, solver.Handle); err == nil {
		t.Error("resolving a non-question must fail")
	}
	if err := s.Resolve(reply.ID, solver.Handle); err == nil {
		t.Error("resolving a reply must fail")
	}
	if err := s.Resolve(q.ID, "ghost-vole-9"); err == nil {
		t.Error("unknown resolver must fail")
	}
	if err := s.Resolve(q.ID, solver.Handle); err != nil {
		t.Fatalf("valid resolve failed: %v", err)
	}
	err := s.Resolve(q.ID, asker.Handle)
	if err == nil || !strings.Contains(err.Error(), solver.Handle) {
		t.Errorf("double resolve should name the resolver, got %v", err)
	}
}

func TestOpenQuestionsScoping(t *testing.T) {
	s, b, a := seed(t)
	b2, _ := s.EnsureBoard("other", "")
	q1, _ := s.CreatePost(b.ID, a.Handle, "open here", "question", nil)
	s.CreatePost(b2.ID, a.Handle, "open there", "question", nil)
	dead, _ := s.CreatePost(b.ID, a.Handle, "tombstoned q", "question", nil)
	s.Tombstone(dead.ID, a.Handle)
	solved, _ := s.CreatePost(b.ID, a.Handle, "solved q", "question", nil)
	s.Resolve(solved.ID, a.Handle)

	open, err := s.OpenQuestions(b.ID, 10)
	if err != nil || len(open) != 1 || open[0].ID != q1.ID {
		t.Fatalf("board scope: %v %+v", err, open)
	}
	all, _ := s.OpenQuestions(0, 10)
	if len(all) != 2 {
		t.Errorf("all-boards open = %d, want 2", len(all))
	}
	n, _ := s.OpenQuestionCount(b.ID)
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

func TestOpenQuestionsReplyCounts(t *testing.T) {
	s, b, asker := seed(t)
	solver, _ := s.RegisterAgent("claude", "solver-sess", "")
	answered, _ := s.CreatePost(b.ID, asker.Handle, "how do I mock time?", "question", nil)
	quiet, _ := s.CreatePost(b.ID, asker.Handle, "anyone seen flaky FTS?", "question", nil)
	s.CreatePost(b.ID, solver.Handle, "inject a clock", "", &answered.ID)
	s.CreatePost(b.ID, asker.Handle, "ooh, trying that", "", &answered.ID)
	dead, _ := s.CreatePost(b.ID, solver.Handle, "wrong, ignore me", "", &answered.ID)
	s.Tombstone(dead.ID, solver.Handle)

	open, err := s.OpenQuestions(b.ID, 10)
	if err != nil || len(open) != 2 {
		t.Fatalf("OpenQuestions: %v %+v", err, open)
	}
	replies := map[uint]int64{}
	for _, q := range open {
		replies[q.ID] = q.Replies
	}
	if replies[answered.ID] != 2 {
		t.Errorf("answered question replies = %d, want 2 (live only, tombstoned excluded)", replies[answered.ID])
	}
	if replies[quiet.ID] != 0 {
		t.Errorf("quiet question replies = %d, want 0", replies[quiet.ID])
	}
}

// Every open question carries its asker's last_seen_at so the CLI can annotate
// how long ago the asker was around. Unregistered askers stay nil: absence is
// data (mention-before-register is legal), never an error.
func TestOpenQuestionsCarryAskerLastSeen(t *testing.T) {
	s, b, asker := seed(t)
	q, err := s.CreatePost(b.ID, asker.Handle, "who owns the hook contract?", "question", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	// Backdate after posting: CreatePost heartbeats its author, so an asker
	// only reads as idle once nothing has happened since they asked.
	old := time.Now().Add(-time.Hour)
	if err := s.DB.Model(&Agent{}).Where("handle = ?", asker.Handle).
		Update("last_seen_at", old).Error; err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}
	// CreatePost rejects unregistered authors, so the never-registered asker is
	// written straight to the table — the shape a mention-before-register or a
	// pruned agent row leaves behind.
	ghost := &Post{BoardID: b.ID, AuthorHandle: "ghost-vole-9",
		Body: "asked before registering?", Tag: "question", CreatedAt: time.Now()}
	if err := s.DB.Create(ghost).Error; err != nil {
		t.Fatalf("insert ghost question: %v", err)
	}

	open, err := s.OpenQuestions(b.ID, 0)
	if err != nil {
		t.Fatalf("OpenQuestions: %v", err)
	}
	seenAt := map[uint]*time.Time{}
	for _, o := range open {
		seenAt[o.ID] = o.AskerLastSeenAt
	}
	got := seenAt[q.ID]
	if got == nil {
		t.Fatalf("registered asker must carry AskerLastSeenAt, got nil (open=%+v)", open)
	}
	if age := time.Since(*got); age < 55*time.Minute || age > 65*time.Minute {
		t.Errorf("AskerLastSeenAt age = %v, want ~1h", age)
	}
	if seenAt[ghost.ID] != nil {
		t.Errorf("unregistered asker must stay nil, got %v", *seenAt[ghost.ID])
	}
	// The store reports raw last_seen_at for every registered asker, fresh
	// ones included; the staleness threshold is a display decision the cmd
	// layer makes, not a filter baked into the query.
	fresh, err := s.RegisterAgent("claude", "fresh-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	fq, err := s.CreatePost(b.ID, fresh.Handle, "anything new on hooks?", "question", nil)
	if err != nil {
		t.Fatalf("CreatePost(fresh): %v", err)
	}
	open, err = s.OpenQuestions(b.ID, 0)
	if err != nil {
		t.Fatalf("OpenQuestions: %v", err)
	}
	for _, o := range open {
		if o.ID == fq.ID && o.AskerLastSeenAt == nil {
			t.Error("a freshly registered asker must still carry AskerLastSeenAt")
		}
	}
}

func TestResolveHumanPostsAnyTagAnyDepth(t *testing.T) {
	s, b, human, agent := humanFixture(t)
	top, _ := s.CreatePost(b.ID, human.Handle, "please look at X", "", nil)
	reply, _ := s.CreatePost(b.ID, human.Handle, "thanks, done", "", &top.ID)
	if err := s.Resolve(top.ID, agent.Handle); err != nil {
		t.Fatalf("untagged human top-level: %v", err)
	}
	if err := s.Resolve(reply.ID, human.Handle); err != nil {
		t.Fatalf("human reply: %v", err)
	}
	if err := s.Resolve(top.ID, agent.Handle); err == nil {
		t.Fatal("double resolve must still error")
	}
	// Agent posts keep the old gate.
	til, _ := s.CreatePost(b.ID, agent.Handle, "til", "til", nil)
	if err := s.Resolve(til.ID, agent.Handle); err == nil {
		t.Fatal("agent til must not be resolvable")
	}
	q, _ := s.CreatePost(b.ID, agent.Handle, "q?", "question", nil)
	ar, _ := s.CreatePost(b.ID, agent.Handle, "reply", "", &q.ID)
	if err := s.Resolve(ar.ID, agent.Handle); err == nil {
		t.Fatal("agent reply must not be resolvable")
	}
	// A resolved human post leaves the unaddressed queue and never enters xfa questions.
	if n, _ := s.UnaddressedHumanCount(b.ID); n != 0 {
		t.Fatalf("unaddressed = %d", n)
	}
	qs, _ := s.OpenQuestions(b.ID, 10)
	for _, oq := range qs {
		if oq.ID == top.ID || oq.ID == reply.ID {
			t.Fatal("human post leaked into open questions")
		}
	}
}
