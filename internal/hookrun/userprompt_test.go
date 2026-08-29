package hookrun

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

func decodePrompt(t *testing.T, out string) (event, ctx string) {
	t.Helper()
	var p struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	return p.HookSpecificOutput.HookEventName, p.HookSpecificOutput.AdditionalContext
}

// promptFixture: temp store, registered project board, a lead agent on session
// "s1", and a foreign agent on another session.
func promptFixture(t *testing.T) (s *store.Store, root string, b *store.Board, lead, other string) {
	t.Helper()
	s, root = stopFixture(t)
	b, _ = s.ResolveBoard(root)
	l, err := s.RegisterAgent("claude", "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.RegisterAgent("claude", "other-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, root, b, l.Handle, o.Handle
}

func TestUserPromptEmitsDigestOnce(t *testing.T) {
	s, root, b, lead, other := promptFixture(t)
	for _, body := range []string{"fts rebuild is idempotent", "wal mode is on"} {
		if _, err := s.CreatePost(b.ID, other, body, "", nil); err != nil {
			t.Fatal(err)
		}
	}

	in := Input{SessionID: "s1", Cwd: root}
	out, err := UserPrompt(s, in)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	event, ctx := decodePrompt(t, out)
	if event != "UserPromptSubmit" {
		t.Errorf("hookEventName = %q, want UserPromptSubmit", event)
	}
	if !strings.Contains(ctx, "2 unread on b/"+b.Slug) {
		t.Errorf("context should carry the unread count and board: %s", ctx)
	}
	if !strings.Contains(ctx, "xfa read --unread --as "+lead) {
		t.Errorf("context should carry the lead's copy-pasteable command: %s", ctx)
	}

	second, err := UserPrompt(s, in)
	if err != nil || second != "" {
		t.Errorf("HWM should suppress the repeat digest, got %q err=%v", second, err)
	}
}

func TestUserPromptReemitsOnNewForeignPost(t *testing.T) {
	s, root, b, lead, other := promptFixture(t)
	for _, body := range []string{"one", "two"} {
		if _, err := s.CreatePost(b.ID, other, body, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{SessionID: "s1", Cwd: root}
	if _, err := UserPrompt(s, in); err != nil {
		t.Fatal(err)
	}
	if quiet, _ := UserPrompt(s, in); quiet != "" {
		t.Fatalf("expected silence before the new post, got %q", quiet)
	}

	if _, err := s.CreatePost(b.ID, other, "three", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := UserPrompt(s, in)
	if err != nil {
		t.Fatal(err)
	}
	_, ctx := decodePrompt(t, out)
	// The lead's read cursor never moved, so unread is cumulative, not the
	// count of posts past the high-water-mark.
	if !strings.Contains(ctx, "3 unread on b/"+b.Slug) {
		t.Errorf("re-emit should report cursor-based unread of 3: %s", ctx)
	}
	if !strings.Contains(ctx, lead) {
		t.Errorf("re-emit should still name the lead: %s", ctx)
	}
}

func TestUserPromptSilentWhenOnlyOwnSessionPosts(t *testing.T) {
	s, root, b, lead, _ := promptFixture(t)
	sub, err := s.RegisterAgent("claude", "s1", lead)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, lead, "lead speaks", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, sub.Handle, "subagent speaks", "", nil); err != nil {
		t.Fatal(err)
	}

	out, err := UserPrompt(s, Input{SessionID: "s1", Cwd: root})
	if err != nil || out != "" {
		t.Errorf("a session must never be nagged about its own posts, got %q err=%v", out, err)
	}
}

func TestUserPromptSilentCases(t *testing.T) {
	t.Run("empty session id", func(t *testing.T) {
		s, root, b, _, other := promptFixture(t)
		if _, err := s.CreatePost(b.ID, other, "hello", "", nil); err != nil {
			t.Fatal(err)
		}
		out, err := UserPrompt(s, Input{Cwd: root})
		if err != nil || out != "" {
			t.Errorf("empty session id should be silent, got %q err=%v", out, err)
		}
	})

	t.Run("cwd outside any project", func(t *testing.T) {
		s, _, b, _, other := promptFixture(t)
		if _, err := s.CreatePost(b.ID, other, "hello", "", nil); err != nil {
			t.Fatal(err)
		}
		out, err := UserPrompt(s, Input{SessionID: "s1", Cwd: t.TempDir()})
		if err != nil || out != "" {
			t.Errorf("unregistered cwd should be silent, got %q err=%v", out, err)
		}
	})

	t.Run("no non-parented handle in session", func(t *testing.T) {
		s, root, b, lead, other := promptFixture(t)
		if _, err := s.CreatePost(b.ID, other, "hello", "", nil); err != nil {
			t.Fatal(err)
		}
		// "s2" holds only a parented subagent: no lead to address.
		if _, err := s.RegisterAgent("claude", "s2", lead); err != nil {
			t.Fatal(err)
		}
		out, err := UserPrompt(s, Input{SessionID: "s2", Cwd: root})
		if err != nil || out != "" {
			t.Errorf("session without a lead should be silent, got %q err=%v", out, err)
		}
	})

	t.Run("lead caught up", func(t *testing.T) {
		s, root, b, lead, other := promptFixture(t)
		p, err := s.CreatePost(b.ID, other, "hello", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.MarkReadID(lead, b.ID, p.ID); err != nil {
			t.Fatal(err)
		}
		out, err := UserPrompt(s, Input{SessionID: "s1", Cwd: root})
		if err != nil || out != "" {
			t.Errorf("caught-up lead should be silent, got %q err=%v", out, err)
		}
		// The mark must NOT advance: the next genuinely-unread post still fires.
		if _, ok, err := s.GetMark("s1" + promptHWMSuffix); err != nil || ok {
			t.Errorf("caught-up path must not burn the high-water-mark (ok=%v err=%v)", ok, err)
		}
	})
}

// Task 12: the human nudge is a sliding 10-minute throttle per session, and it
// must fire independently of the unread digest's high-water-mark.
func TestUserPromptHumanNudgeThrottle(t *testing.T) {
	s, root, b, _, _ := promptFixture(t)
	h, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, h.Handle, "please look at the flaky test", "", nil); err != nil {
		t.Fatal(err)
	}

	in := Input{SessionID: "s1", Cwd: root}
	out, err := UserPrompt(s, in)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	_, ctx := decodePrompt(t, out)
	if !strings.Contains(ctx, "1 unaddressed human post(s) on b/"+b.Slug) {
		t.Errorf("first prompt should carry the human nudge: %s", ctx)
	}
	if !strings.Contains(ctx, "xfa read --human") {
		t.Errorf("human nudge should carry the command: %s", ctx)
	}

	// Immediate repeat: the unread digest is HWM-silent and the human nudge is
	// throttled, so there is nothing left to say.
	second, err := UserPrompt(s, in)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	if second != "" {
		t.Errorf("human nudge should be throttled on an immediate repeat, got %q", second)
	}

	// Backdate the throttle mark past the window: the nudge returns, on its own,
	// with the unread digest still silent.
	stale := strconv.FormatInt(time.Now().Add(-11*time.Minute).Unix(), 10)
	if err := s.SetMark("s1"+humanNudgeSuffix, stale); err != nil {
		t.Fatal(err)
	}
	third, err := UserPrompt(s, in)
	if err != nil {
		t.Fatalf("must fail open, got err=%v", err)
	}
	_, ctx3 := decodePrompt(t, third)
	if !strings.Contains(ctx3, "unaddressed human post(s)") {
		t.Errorf("nudge should re-fire once the window elapsed: %s", ctx3)
	}
	if strings.Contains(ctx3, "unread on b/") {
		t.Errorf("the unread digest must stay HWM-silent here: %s", ctx3)
	}
	// The mark must have been refreshed, not left stale.
	if v, ok, err := s.GetMark("s1" + humanNudgeSuffix); err != nil || !ok || v == stale {
		t.Errorf("re-fire should refresh the throttle mark (v=%q ok=%v err=%v)", v, ok, err)
	}
}

// Zero unaddressed human posts must not burn the throttle mark — otherwise the
// first real human post waits out a window it never triggered.
func TestUserPromptHumanNudgeSkipsMarkWhenNoHumanPosts(t *testing.T) {
	s, root, b, _, other := promptFixture(t)
	if _, err := s.CreatePost(b.ID, other, "agent chatter", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := UserPrompt(s, Input{SessionID: "s1", Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	_, ctx := decodePrompt(t, out)
	if strings.Contains(ctx, "unaddressed human") {
		t.Errorf("no human posts must produce no human line: %s", ctx)
	}
	if _, ok, err := s.GetMark("s1" + humanNudgeSuffix); err != nil || ok {
		t.Errorf("zero-count path must not write the throttle mark (ok=%v err=%v)", ok, err)
	}
}

// The throttle key must stay namespaced: a bare session-id row would satisfy
// the Stop hook's fire-once guard and silence the end-of-session nudge.
func TestUserPromptHumanNudgeLeavesStopNudgeIntact(t *testing.T) {
	s, root, b, _, _ := promptFixture(t)
	h, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, h.Handle, "ping", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := UserPrompt(s, Input{SessionID: "s1", Cwd: root}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetMark("s1" + humanNudgeSuffix); !ok {
		t.Fatal("expected a namespaced human-nudge row")
	}
	if _, ok, _ := s.GetMark("s1"); ok {
		t.Error("the bare session id must not be marked — it would silence the Stop nudge")
	}
	out, err := Stop(s, Input{SessionID: "s1", Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("Stop must still nudge after the human throttle mark was written, got %q", out)
	}
}

// The mark key must stay namespaced: a bare session-id row would satisfy the
// Stop hook's fire-once guard and silence the end-of-session nudge.
func TestUserPromptMarkKeyIsNamespaced(t *testing.T) {
	s, root, b, _, other := promptFixture(t)
	if _, err := s.CreatePost(b.ID, other, "hello", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := UserPrompt(s, Input{SessionID: "s1", Cwd: root}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetMark("s1" + promptHWMSuffix); !ok {
		t.Error("expected a namespaced high-water-mark row")
	}
	if _, ok, _ := s.GetMark("s1"); ok {
		t.Error("the bare session id must not be marked — it would silence the Stop nudge")
	}
}
