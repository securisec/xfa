package hookrun

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeInvoke asserts the exact documented PreInvocation output shape:
// {"injectSteps":[{"ephemeralMessage": "<text>"}]} with exactly one element.
func decodeInvoke(t *testing.T, out string) string {
	t.Helper()
	var p struct {
		InjectSteps []struct {
			EphemeralMessage string `json:"ephemeralMessage"`
		} `json:"injectSteps"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if len(p.InjectSteps) != 1 {
		t.Fatalf("injectSteps has %d elements, want 1: %s", len(p.InjectSteps), out)
	}
	if p.InjectSteps[0].EphemeralMessage == "" {
		t.Fatalf("empty ephemeralMessage: %s", out)
	}
	return p.InjectSteps[0].EphemeralMessage
}

// The conversation's first invocation gets the session-start preamble, and the
// first-ness mark is namespaced (a bare conversation-id row would satisfy the
// Stop hook's fire-once guard and silence the end-of-session nudge).
func TestAntigravityInvokeFirstEmitsSessionStart(t *testing.T) {
	s, root := stopFixture(t)
	b, _ := s.ResolveBoard(root)
	a := AntigravityInput{ConversationID: "conv-1", WorkspacePaths: []string{root}}
	out, err := AntigravityInvoke(s, a)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	msg := decodeInvoke(t, out)
	if !strings.Contains(msg, "b/"+b.Slug) {
		t.Errorf("first invoke should carry the board preamble: %s", msg)
	}
	if !strings.Contains(msg, "xfa register --session conv-1") {
		t.Errorf("preamble should render the conversation id as the session: %s", msg)
	}
	if _, ok, _ := s.GetMark("conv-1" + agStartSuffix); !ok {
		t.Error("first invoke must set the namespaced start mark")
	}
	if _, ok, _ := s.GetMark("conv-1"); ok {
		t.Error("the bare conversation id must not be marked — it would silence the Stop nudge")
	}
}

// Later invocations route to the user-prompt logic: HWM-silent until someone
// outside the conversation posts, then the digest fires as injectSteps.
func TestAntigravityInvokeSecondRoutesToUserPrompt(t *testing.T) {
	s, root, b, lead, other := promptFixture(t)
	a := AntigravityInput{ConversationID: "s1", WorkspacePaths: []string{root}}
	if out, err := AntigravityInvoke(s, a); err != nil || out == "" {
		t.Fatalf("first invoke should emit the preamble, got %q err=%v", out, err)
	}
	quiet, err := AntigravityInvoke(s, a)
	if err != nil || quiet != "" {
		t.Fatalf("nothing new: second invoke must be silent, got %q err=%v", quiet, err)
	}
	if _, err := s.CreatePost(b.ID, other, "fresh finding", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := AntigravityInvoke(s, a)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	msg := decodeInvoke(t, out)
	if !strings.Contains(msg, "1 unread on b/"+b.Slug) {
		t.Errorf("later invoke should carry the unread digest: %s", msg)
	}
	if !strings.Contains(msg, lead) {
		t.Errorf("digest should name the lead handle: %s", msg)
	}
}

// Stop re-wraps the Claude nudge as antigravity's continue decision, fires
// once, and shares the fire-once guard with the Claude-shaped Stop.
func TestAntigravityStopFiresOnce(t *testing.T) {
	s, root := stopFixture(t)
	a := AntigravityInput{ConversationID: "conv-s", WorkspacePaths: []string{root}}
	out, err := AntigravityStop(s, a)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	var p struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if p.Decision != "continue" {
		t.Errorf("decision = %q, want \"continue\"", p.Decision)
	}
	if !strings.Contains(p.Reason, "xfa register --session conv-s") {
		t.Errorf("reason should carry the nudge: %s", p.Reason)
	}
	if second, _ := AntigravityStop(s, a); second != "" {
		t.Errorf("second stop must be silent, got %q", second)
	}
	// Same guard as the Claude-shaped Stop: no double nudge across shapes.
	if claude, _ := Stop(s, Input{SessionID: "conv-s", Cwd: root}); claude != "" {
		t.Errorf("Claude Stop must share the fire-once guard, got %q", claude)
	}
}

// A payload missing the conversation id or workspace paths (which is also what
// decode garbage produces) yields empty output and no error, on both events.
func TestAntigravityFailsOpenOnBadPayload(t *testing.T) {
	s, root := stopFixture(t)
	for _, a := range []AntigravityInput{
		{},
		{ConversationID: "c"},
		{WorkspacePaths: []string{root}},
	} {
		if out, err := AntigravityInvoke(s, a); err != nil || out != "" {
			t.Errorf("AntigravityInvoke(%+v) = %q, %v; want empty, nil", a, out, err)
		}
		if out, err := AntigravityStop(s, a); err != nil || out != "" {
			t.Errorf("AntigravityStop(%+v) = %q, %v; want empty, nil", a, out, err)
		}
	}
}
