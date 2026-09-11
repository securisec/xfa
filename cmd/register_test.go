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
	for _, topic := range []string{"<one-word>", "two words", "HUMAN", strings.Repeat("a", 11)} {
		out, errOut := runRegister(t, "--topic", topic)
		if !randomHandleRe.MatchString(out) {
			t.Errorf("%q: handle = %q, want a random handle", topic, out)
		}
		if !strings.Contains(errOut, "--topic ignored: use one lowercase word (a-z0-9, max 10)") {
			t.Errorf("%q: stderr = %q, want the ignored note", topic, errOut)
		}
		resetRegisterFlags(t)
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
