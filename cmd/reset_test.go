package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

// runXfaErr executes the real root command and returns its combined output
// and error — for commands whose failure IS the behavior under test.
func runXfaErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

// seedResetDB creates a temp DB with one board, one agent, one post and
// points XFA_DB at it. The pool is closed so nothing holds the file open.
func seedResetDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("resettest", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "reset-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "precious data", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if sqlDB, err := s.DB.DB(); err == nil {
		sqlDB.Close()
	}
	return dbPath
}

// Reset is HUMAN-ONLY: without --yes and without an interactive terminal it
// must refuse outright — BEFORE ever opening (and thereby AutoMigrate-ing or
// checkpointing) the database it declined to touch. The test swaps os.Stdin
// for a pipe — exactly what an agent-driven `echo x | xfa reset` looks like.
// (A genuine TTY at the confirmation prompt is still manual-smoke-only; the
// prompt's read path is covered below via cmd.InOrStdin.)
func TestResetRefusesWithoutTerminalOrYes(t *testing.T) {
	dbPath := seedResetDB(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		r.Close()
	})
	w.WriteString("x\n")
	w.Close()

	out, err := runXfaErr(t, "reset", "--yes=false")
	if err == nil {
		t.Fatalf("reset on piped stdin without --yes must fail, output %q", out)
	}
	if !strings.Contains(err.Error(), "refusing to reset") ||
		!strings.Contains(err.Error(), "not a terminal") {
		t.Errorf("want the human-only refusal, got %v", err)
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Errorf("refused reset must leave the DB untouched: %v", statErr)
	}
	// The refusal must fire before store.Open: no summary printed, and no
	// -wal sidecar materialized by an open (seedResetDB closed cleanly, so a
	// -wal here could only come from reset itself opening the DB).
	if strings.Contains(out, "This deletes the ENTIRE") {
		t.Errorf("refusal must precede the open/summary, got %q", out)
	}
	if _, statErr := os.Stat(dbPath + "-wal"); !os.IsNotExist(statErr) {
		t.Errorf("refused reset must not open the DB (found -wal sidecar): %v", statErr)
	}
}

// stdinCharDevice swaps os.Stdin for /dev/null — a character device, so the
// TTY gate passes — while the prompt's actual reader comes from
// cmd.InOrStdin(), letting the harness script the confirmation.
func stdinCharDevice(t *testing.T) {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	origStdin := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = origStdin
		f.Close()
	})
}

// runXfaWithStdin is runXfaErr with a scripted stdin for the command's
// cmd.InOrStdin() reader.
func runXfaWithStdin(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	rootCmd.SetIn(in)
	t.Cleanup(func() { rootCmd.SetIn(nil) })
	return runXfaErr(t, args...)
}

func TestResetPromptNonConfirmationAborts(t *testing.T) {
	dbPath := seedResetDB(t)
	stdinCharDevice(t)

	out, err := runXfaWithStdin(t, strings.NewReader("nope\n"), "reset", "--yes=false")
	if err == nil || err.Error() != "aborted" {
		t.Fatalf("non-'reset' confirmation must abort, err=%v output=%q", err, out)
	}
	if !strings.Contains(out, "This deletes the ENTIRE") ||
		!strings.Contains(out, "type 'reset' to confirm: ") {
		t.Errorf("prompt path must show summary then prompt, got %q", out)
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Errorf("aborted reset must leave the DB untouched: %v", statErr)
	}
}

func TestResetPromptConfirmationDeletes(t *testing.T) {
	dbPath := seedResetDB(t)
	stdinCharDevice(t)

	out, err := runXfaWithStdin(t, strings.NewReader("reset\n"), "reset", "--yes=false")
	if err != nil {
		t.Fatalf("typed 'reset' must confirm: %v (output %q)", err, out)
	}
	if !strings.Contains(out, "database reset") {
		t.Errorf("missing completion message, got %q", out)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Errorf("confirmed reset must delete the DB, stat: %v", statErr)
	}
}

// EOF at the prompt is what `xfa reset </dev/null` looks like — /dev/null IS
// a character device, so it passes the TTY gate. No human is answering, so
// the full agents-must-never-run refusal fires, not a generic "aborted".
func TestResetPromptEOFRefuses(t *testing.T) {
	dbPath := seedResetDB(t)
	stdinCharDevice(t)

	out, err := runXfaWithStdin(t, strings.NewReader(""), "reset", "--yes=false")
	if err == nil {
		t.Fatalf("EOF at the prompt must refuse, output %q", out)
	}
	if !strings.Contains(err.Error(), "refusing to reset") ||
		!strings.Contains(err.Error(), "agents must never run it") {
		t.Errorf("EOF must get the full human-only refusal, got %v", err)
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Errorf("refused reset must leave the DB untouched: %v", statErr)
	}
}

// A missing main DB with stray -wal/-shm sidecars (e.g. a crash between the
// removes of a previous reset) still gets swept before "nothing to reset".
func TestResetSweepsOrphanSidecars(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.WriteFile(sidecar, []byte("orphan"), 0o644); err != nil {
			t.Fatalf("plant %s: %v", sidecar, err)
		}
	}

	out, err := runXfaErr(t, "reset", "--yes=false")
	if err != nil || !strings.Contains(out, "nothing to reset") {
		t.Fatalf("missing main DB: err=%v output=%q, want nothing to reset", err, out)
	}
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if _, statErr := os.Stat(sidecar); !os.IsNotExist(statErr) {
			t.Errorf("orphan sidecar %s must be swept, stat: %v", sidecar, statErr)
		}
	}
}

func TestResetYesDeletesDatabase(t *testing.T) {
	dbPath := seedResetDB(t)

	out, err := runXfaErr(t, "reset", "--yes")
	if err != nil {
		t.Fatalf("reset --yes: %v (output %q)", err, out)
	}
	if !strings.Contains(out, "1 boards, 1 posts, 1 agents") {
		t.Errorf("summary must show what is being deleted, got %q", out)
	}
	if !strings.Contains(out, "database reset") {
		t.Errorf("missing completion message, got %q", out)
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s must be gone, stat: %v", p, statErr)
		}
	}

	// A second run has nothing left to do — and must not error.
	out, err = runXfaErr(t, "reset", "--yes")
	if err != nil || !strings.Contains(out, "nothing to reset") {
		t.Errorf("reset on a missing DB: err=%v output=%q, want nothing to reset", err, out)
	}
}
