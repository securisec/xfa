package hookrun

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

// askQuestion posts one open question on the fixture board from a foreign session.
func askQuestion(t *testing.T, s *store.Store, root string) {
	t.Helper()
	b, err := s.ResolveBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.RegisterAgent("claude", "other-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "does the retry layer cover fts writes?", "question", nil); err != nil {
		t.Fatal(err)
	}
}

// findingNudge is the unconditional half of the reason: every subagent finish
// asks for findings, open questions or not.
func findingNudge(t *testing.T, reason string) {
	t.Helper()
	for _, want := range []string{
		"Before you finish",
		"post it now",
		"--tag finding",
		"finish now",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason missing %q:\n%s", want, reason)
		}
	}
}

func TestSubagentStopBlocksOnOpenQuestions(t *testing.T) {
	s, root := stopFixture(t)
	askQuestion(t, s, root)
	out, err := SubagentStop(s, Input{SessionID: "sess-sub-1", Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	decision, reason := decodeStop(t, out)
	if decision != "block" {
		t.Fatalf("want decision block, got %q (out=%q)", decision, out)
	}
	findingNudge(t, reason)
	for _, want := range []string{
		"1 open question(s) on b/proj",
		"xfa questions",
		"xfa inbox --as",
		"xfa reply",
		"resolve",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason missing %q:\n%s", want, reason)
		}
	}
}

// No open questions is no longer silence: the finding nudge fires regardless,
// and only the open-question sentence is omitted.
func TestSubagentStopFiresWithoutOpenQuestions(t *testing.T) {
	s, root := stopFixture(t)
	out, err := SubagentStop(s, Input{SessionID: "sess-sub-2", Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	decision, reason := decodeStop(t, out)
	if decision != "block" {
		t.Fatalf("want decision block, got %q (out=%q)", decision, out)
	}
	findingNudge(t, reason)
	if strings.Contains(reason, "open question") {
		t.Errorf("no open questions must not mention them:\n%s", reason)
	}
	if n := reminderCount(t, s); n != 1 {
		t.Errorf("firing must burn exactly one Reminder row, got %d", n)
	}
	second, _ := SubagentStop(s, Input{SessionID: "sess-sub-2", Cwd: root})
	if second != "" {
		t.Errorf("second subagent stop must be silent, got %q", second)
	}
}

// A store error on the question count only drops that sentence — the nudge
// still fires and never surfaces an error.
func TestSubagentStopFailsOpenOnCountError(t *testing.T) {
	s, root := stopFixture(t)
	if err := s.DB.Exec("DROP TABLE posts").Error; err != nil {
		t.Fatal(err)
	}
	out, err := SubagentStop(s, Input{SessionID: "sess-sub-8", Cwd: root})
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	decision, reason := decodeStop(t, out)
	if decision != "block" {
		t.Fatalf("want decision block, got %q (out=%q)", decision, out)
	}
	findingNudge(t, reason)
	if strings.Contains(reason, "open question") {
		t.Errorf("count error must drop the open-question sentence:\n%s", reason)
	}
}

// A subagent already continued from a block must never be re-blocked.
func TestSubagentStopSilentWhenStopHookActive(t *testing.T) {
	s, root := stopFixture(t)
	askQuestion(t, s, root)
	out, err := SubagentStop(s, Input{SessionID: "sess-sub-3", Cwd: root, StopHookActive: true})
	if err != nil || out != "" {
		t.Errorf("stop_hook_active must be silent, got %q err=%v", out, err)
	}
	if n := reminderCount(t, s); n != 0 {
		t.Errorf("stop_hook_active must not burn a Reminder row, got %d", n)
	}
}

func TestSubagentStopFiresOncePerSession(t *testing.T) {
	s, root := stopFixture(t)
	askQuestion(t, s, root)
	in := Input{SessionID: "sess-sub-4", Cwd: root}
	first, err := SubagentStop(s, in)
	if err != nil || !strings.Contains(first, `"decision":"block"`) {
		t.Fatalf("first subagent stop should block, got %q err=%v", first, err)
	}
	second, _ := SubagentStop(s, in)
	if second != "" {
		t.Errorf("second subagent stop must be silent, got %q", second)
	}
}

func TestSubagentStopSilentOutsideProject(t *testing.T) {
	s, _ := stopFixture(t)
	out, err := SubagentStop(s, Input{SessionID: "sess-sub-5", Cwd: t.TempDir()})
	if err != nil || out != "" {
		t.Errorf("unregistered cwd should be silent, got %q err=%v", out, err)
	}
	if n := reminderCount(t, s); n != 0 {
		t.Errorf("unregistered cwd must not burn a Reminder row, got %d", n)
	}
}

func TestSubagentStopSilentWithoutSessionID(t *testing.T) {
	s, root := stopFixture(t)
	askQuestion(t, s, root)
	out, err := SubagentStop(s, Input{Cwd: root})
	if err != nil || out != "" {
		t.Errorf("empty session id should be silent, got %q err=%v", out, err)
	}
	if n := reminderCount(t, s); n != 0 {
		t.Errorf("empty session id must not burn a Reminder row, got %d", n)
	}
}

// The two nudges share the reminders table but use distinct keys: firing one
// must not silence the other for the same session, in either order.
func TestSubagentStopAndStopNudgeAreIndependent(t *testing.T) {
	s, root := stopFixture(t)
	askQuestion(t, s, root)
	in := Input{SessionID: "sess-sub-6", Cwd: root}

	if out, _ := SubagentStop(s, in); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("subagent stop should block first, got %q", out)
	}
	if out, _ := Stop(s, in); !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("Stop nudge must be unaffected by the subagent key, got %q", out)
	}

	// Reverse order on a fresh fixture.
	s2, root2 := stopFixture(t)
	askQuestion(t, s2, root2)
	in2 := Input{SessionID: "sess-sub-7", Cwd: root2}
	if out, _ := Stop(s2, in2); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("Stop nudge should block first, got %q", out)
	}
	if out, _ := SubagentStop(s2, in2); !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("subagent stop must be unaffected by the Stop key, got %q", out)
	}
}

// Claude Code sets stop_hook_active on Stop-family hook input; the Input
// struct must decode it from the wire name.
func TestInputDecodesStopHookActive(t *testing.T) {
	var in Input
	if err := json.Unmarshal([]byte(`{"session_id":"s","stop_hook_active":true}`), &in); err != nil {
		t.Fatal(err)
	}
	if !in.StopHookActive {
		t.Error("stop_hook_active not decoded into Input.StopHookActive")
	}
}
