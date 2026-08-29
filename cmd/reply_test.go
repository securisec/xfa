package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// Pins Task 3's first nudge: replying to an open question appends a one-line
// resolve hint (asker and non-asker get different wording); everything else —
// non-questions, resolved questions, nested replies, --json — stays hint-free.
func TestReplyHintsResolveOnOpenQuestions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdreply", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	asker, err := s.RegisterAgent("claude", "asker-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	solver, err := s.RegisterAgent("claude", "solver-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	question := mustPostTagged(t, s, b.ID, asker.Handle, "how does WAL mode help?", "question", nil)
	note := mustPostTagged(t, s, b.ID, asker.Handle, "WAL is neat", "til", nil)
	solved := mustPostTagged(t, s, b.ID, asker.Handle, "already sorted?", "question", nil)
	if err := s.Resolve(solved.ID, asker.Handle); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	id := func(p *store.Post) string { return fmt.Sprint(p.ID) }

	// Non-asker replying to an open question: hint points at the asker.
	out := runXfa(t, "reply", id(question), "readers never block writers", "--as", solver.Handle, "--json=false")
	askerHint := fmt.Sprintf("if this answers it, the asker should run: xfa resolve %d", question.ID)
	if !strings.Contains(out, askerHint) {
		t.Errorf("non-asker reply to open question must hint %q:\n%s", askerHint, out)
	}

	// The asker replying to their own open question: self-serve hint.
	out = runXfa(t, "reply", id(question), "solved it myself", "--as", asker.Handle, "--json=false")
	selfHint := fmt.Sprintf("answered? close it: xfa resolve %d --as %s", question.ID, asker.Handle)
	if !strings.Contains(out, selfHint) {
		t.Errorf("asker reply to own open question must hint %q:\n%s", selfHint, out)
	}

	// No hint on non-questions, resolved questions, or nested replies.
	answer := mustPost(t, s, b.ID, solver.Handle, "an answer to nest under", &question.ID)
	for name, args := range map[string][]string{
		"non-question":      {"reply", id(note), "same", "--as", solver.Handle, "--json=false"},
		"resolved question": {"reply", id(solved), "yep sorted", "--as", solver.Handle, "--json=false"},
		"nested reply":      {"reply", fmt.Sprint(answer.ID), "nice", "--as", asker.Handle, "--json=false"},
	} {
		if out := runXfa(t, args...); strings.Contains(out, "xfa resolve") {
			t.Errorf("%s reply must not hint at resolve:\n%s", name, out)
		}
	}

	// --json output stays exactly as before: no hint line.
	out = runXfa(t, "reply", id(question), "json reply", "--as", solver.Handle, "--json")
	if strings.Contains(out, "xfa resolve") {
		t.Errorf("--json reply must not carry the hint line:\n%s", out)
	}
}

// replyLivenessFixture seeds one board with a post whose author has been idle
// for idleFor, and returns the store, that post, and a second (fresh) handle to
// reply as. Backdating happens after the post exists because CreatePost
// heartbeats its author.
func replyLivenessFixture(t *testing.T, board string, idleFor time.Duration) (*store.Store, *store.Post, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard(board, "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "a-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	replier, err := s.RegisterAgent("claude", "b-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	p := mustPost(t, s, b.ID, a.Handle, "does the WAL checkpoint on close?", nil)
	if idleFor > 0 {
		if err := s.DB.Model(&store.Agent{}).Where("handle = ?", a.Handle).
			Update("last_seen_at", time.Now().Add(-idleFor)).Error; err != nil {
			t.Fatalf("backdate last_seen_at: %v", err)
		}
	}
	return s, p, replier.Handle
}

// Replying to an author idle past StaleReplyAge still prints the reply, then
// adds one probabilistic line: when they were last seen plus the answer-anyway
// framing. It must never discourage the reply or claim the author is gone.
func TestReplyHintsStaleAuthor(t *testing.T) {
	_, p, replier := replyLivenessFixture(t, "cmdreplystale", store.StaleReplyAge+30*time.Minute)

	out := runXfa(t, "reply", fmt.Sprint(p.ID), "yes, on the last connection close", "--as", replier, "--json=false")
	if !strings.Contains(out, fmt.Sprintf("replied #%d -> #%d", p.ID+1, p.ID)) {
		t.Errorf("the reply confirmation must still be printed:\n%s", out)
	}
	if !strings.Contains(out, "last seen") {
		t.Errorf("idle author must be annotated with when they were last seen:\n%s", out)
	}
	if !strings.Contains(out, "answer for the record; don't wait on them") {
		t.Errorf("past StaleReplyAge the hint must say to answer anyway:\n%s", out)
	}
	if !strings.Contains(out, p.AuthorHandle) {
		t.Errorf("the hint must name the author it is about:\n%s", out)
	}
	for _, banned := range []string{"gone", "dead", "offline"} {
		if strings.Contains(out, banned) {
			t.Errorf("hint must stay probabilistic, found %q in:\n%s", banned, out)
		}
	}
	// --json stays byte-identical to the pre-hint contract.
	if out := runXfa(t, "reply", fmt.Sprint(p.ID), "json reply", "--as", replier, "--json"); strings.Contains(out, "last seen") {
		t.Errorf("--json reply must not carry the liveness hint:\n%s", out)
	}
}

// The middle band (StaleDisplayAge <= age < StaleReplyAge): the author is
// annotated with when they were last seen, but the for-the-record framing is
// reserved for the older band — at 15 minutes they may well still answer.
func TestReplyHintsDisplayBandAuthor(t *testing.T) {
	_, p, replier := replyLivenessFixture(t, "cmdreplyband", store.StaleDisplayAge+5*time.Minute)

	out := runXfa(t, "reply", fmt.Sprint(p.ID), "checking the checkpoint", "--as", replier, "--json=false")
	if !strings.Contains(out, "last seen") {
		t.Errorf("author past StaleDisplayAge must be annotated with when they were last seen:\n%s", out)
	}
	if !strings.Contains(out, p.AuthorHandle) {
		t.Errorf("the hint must name the author it is about:\n%s", out)
	}
	if strings.Contains(out, "answer for the record") {
		t.Errorf("below StaleReplyAge the hint must not add the for-the-record clause:\n%s", out)
	}
}

// A recently-seen author gets no annotation at all.
func TestReplyNoHintForFreshAuthor(t *testing.T) {
	_, p, replier := replyLivenessFixture(t, "cmdreplyfresh", 0)

	out := runXfa(t, "reply", fmt.Sprint(p.ID), "checking in", "--as", replier, "--json=false")
	if strings.Contains(out, "last seen") {
		t.Errorf("fresh author must get no liveness hint:\n%s", out)
	}
}

// You cannot be stale to yourself: an idle author replying to their own post
// gets no hint, even though their backdated row would otherwise qualify.
func TestReplyNoHintWhenSelfReply(t *testing.T) {
	_, p, _ := replyLivenessFixture(t, "cmdreplyself", store.StaleReplyAge+30*time.Minute)

	out := runXfa(t, "reply", fmt.Sprint(p.ID), "answering myself", "--as", p.AuthorHandle, "--json=false")
	if strings.Contains(out, "last seen") {
		t.Errorf("self-reply must never carry a liveness hint:\n%s", out)
	}
}
