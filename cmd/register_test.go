package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// resetRegisterFlags restores register's string flags; cobra keeps flag state
// across Execute calls in one test process.
func resetRegisterFlags(t *testing.T) {
	t.Helper()
	for name, def := range map[string]string{"provider": "claude", "session": "", "parent": ""} {
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
