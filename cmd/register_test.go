package cmd

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// resetRegisterFlags restores register's string flags; cobra keeps flag state
// across Execute calls in one test process.
func resetRegisterFlags(t *testing.T) {
	t.Helper()
	for name, def := range map[string]string{"provider": "claude", "session": "", "parent": "", "topic": ""} {
		f := registerCmd.Flags().Lookup(name)
		_ = f.Value.Set(def)
		f.Changed = false
	}
}

// The web UI mints its human handle through store.RegisterAgent directly; the
// CLI must never mint one, because every session treats human posts as
// "answer before anything else" and human posts can be resolved by anyone.
func TestRegisterRefusesHumanProvider(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	_, err := runXfaErr(t, "register", "--provider", "human")
	if err == nil || !strings.Contains(err.Error(), "human is minted by the web UI, not register") {
		t.Fatalf("err = %v", err)
	}
}

// Free-string flags become network-reachable writes under remote mode.
func TestRegisterBoundsFlagLengths(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	long := strings.Repeat("x", 129)
	for _, flag := range []string{"--provider", "--session", "--parent"} {
		_, err := runXfaErr(t, "register", flag, long)
		if err == nil || !strings.Contains(err.Error(), "register: flag value too long") {
			t.Fatalf("%s: err = %v", flag, err)
		}
		resetRegisterFlags(t)
	}
}

// runRegister executes register with stdout and stderr kept apart — the
// --topic fallback note is only useful if it lands on stderr, away from the
// handle every caller of `xfa register` parses off stdout.
func runRegister(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(append([]string{"register"}, args...))
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		rootCmd.SetIn(nil)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("xfa register %v: %v", args, err)
	}
	return strings.TrimSpace(out.String()), errBuf.String()
}

var randomHandleRe = regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9]{1,2}$`)

// --topic lets an agent stamp what it is working on into its own handle.
func TestRegisterTopicPrefixesHandle(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	out, errOut := runRegister(t, "--topic", "web")
	if !strings.HasPrefix(out, "web-") || !randomHandleRe.MatchString(out) {
		t.Fatalf("handle = %q, want web-<word>-<N>", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want silence", errOut)
	}
}

// A junk --topic must not break the session: it degrades to a normal random
// handle and says so on stderr, which is how an agent learns the flag's shape.
func TestRegisterBadTopicFallsBackWithNote(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	for _, topic := range []string{"<one-word>", "two words", "HUMAN", "<orchestrator>"} {
		out, errOut := runRegister(t, "--topic", topic)
		if !randomHandleRe.MatchString(out) {
			t.Errorf("%q: handle = %q, want a random handle", topic, out)
		}
		if !strings.Contains(errOut, "--topic ignored: use one lowercase word (a-z0-9 only)") {
			t.Errorf("%q: stderr = %q, want the ignored note", topic, errOut)
		}
		resetRegisterFlags(t)
	}
}

// An over-long but otherwise valid --topic is shortened, not dropped — the
// real incident was `--topic orchestrator` degrading to a random adjective
// that 24 descendants then inherited. The shortening is said on stderr, never
// silently, and the note must not be the "ignored" one.
func TestRegisterLongTopicIsShortenedWithNote(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	out, errOut := runRegister(t, "--topic", "orchestrator")
	if !strings.HasPrefix(out, "orchestrat-") || !randomHandleRe.MatchString(out) {
		t.Fatalf("handle = %q, want orchestrat-<noun>-<N>", out)
	}
	if !strings.Contains(errOut, "--topic shortened to orchestrat") {
		t.Fatalf("stderr = %q, want the shortened note", errOut)
	}
	if strings.Contains(errOut, "ignored") {
		t.Fatalf("stderr = %q: a shortened topic is not an ignored one", errOut)
	}
	resetRegisterFlags(t)
	// Exactly 10 is unchanged and silent.
	out, errOut = runRegister(t, "--topic", "0123456789")
	if !strings.HasPrefix(out, "0123456789-") {
		t.Fatalf("handle = %q, want 0123456789-<noun>-<N>", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want silence", errOut)
	}
}

// The no-flag call is the shape every pre-topic caller still uses: a random
// adjective-noun-N on stdout and nothing on stderr.
func TestRegisterNoFlagsKeepsRandomHandle(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	out, errOut := runRegister(t)
	if !randomHandleRe.MatchString(out) {
		t.Fatalf("handle = %q, want <adjective>-<noun>-<N>", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want silence", errOut)
	}
}

// A subagent passes --parent but almost never --topic: the only thing telling
// it to was prose the lead had to copy into the spawn prompt. The parent's
// topic slot is inherited so lineage shares a prefix with zero cooperation.
func TestRegisterInheritsTopicFromParent(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	out, errOut := runRegister(t, "--parent", "web-wombat-25")
	if !strings.HasPrefix(out, "web-") || !randomHandleRe.MatchString(out) {
		t.Fatalf("handle = %q, want web-<word>-<N>", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want silence", errOut)
	}
}

// A lead splitting work across workers still needs to override the lineage
// prefix, so an explicit --topic beats inheritance.
func TestRegisterExplicitTopicBeatsParent(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	out, _ := runRegister(t, "--parent", "web-wombat-25", "--topic", "store")
	if !strings.HasPrefix(out, "store-") || !randomHandleRe.MatchString(out) {
		t.Fatalf("handle = %q, want store-<word>-<N>", out)
	}
}

// "ignored" has to mean "as if never passed": the hook prose ships a <word>
// placeholder that fails validation when pasted verbatim, so a rejected
// explicit --topic must still fall through to the parent's prefix.
func TestRegisterBadTopicStillInheritsFromParent(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	out, errOut := runRegister(t, "--parent", "web-wombat-25", "--topic", "<one-word>")
	if !strings.HasPrefix(out, "web-") || !randomHandleRe.MatchString(out) {
		t.Fatalf("handle = %q, want web-<word>-<N>", out)
	}
	if !strings.Contains(errOut, "--topic ignored: use one lowercase word (a-z0-9 only)") {
		t.Fatalf("stderr = %q, want the ignored note", errOut)
	}
}

// Inheritance is automatic, not something the caller asked for: an unusable
// parent degrades to a random handle in silence. A note here would blame an
// agent for a flag it never passed.
func TestRegisterUnusableParentInheritsNothingQuietly(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	t.Cleanup(func() { resetRegisterFlags(t) })
	for _, parent := range []string{"nodashes", strings.Repeat("a", 11) + "-otter-7", "-otter-7", "human-otter-7"} {
		out, errOut := runRegister(t, "--parent", parent)
		if !randomHandleRe.MatchString(out) {
			t.Errorf("%q: handle = %q, want a random handle", parent, out)
		}
		if errOut != "" {
			t.Errorf("%q: stderr = %q, want silence", parent, errOut)
		}
		resetRegisterFlags(t)
	}
}
