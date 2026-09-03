package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

func seedInboxWait(t *testing.T) (*store.Store, *store.Board, *store.Agent, *store.Agent) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdinbox", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	me, err := s.RegisterAgent("claude", "me-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	other, err := s.RegisterAgent("claude", "other-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	prevInterval, prevTimeout := inboxWaitInterval, inboxWaitTimeout
	inboxWaitInterval, inboxWaitTimeout = 20*time.Millisecond, 5*time.Second
	// Cobra flag values persist across Execute calls in one process, so a
	// stuck --wait=true would make every later inbox test block for the
	// full deadline.
	t.Cleanup(func() {
		inboxWaitInterval, inboxWaitTimeout = prevInterval, prevTimeout
		_ = inboxCmd.Flags().Set("wait", "false")
	})
	return s, b, me, other
}

// runXfaAsync is runXfa for a blocking command: the result arrives on the
// returned channel once the command returns.
func runXfaAsync(t *testing.T, args ...string) <-chan string {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	done := make(chan string, 1)
	go func() {
		err := rootCmd.Execute()
		if err != nil {
			done <- "ERR: " + err.Error() + "\n" + buf.String()
			return
		}
		done <- buf.String()
	}()
	return done
}

// --wait returns as soon as a reply to the handle's post lands — long before
// the deadline — and prints it through the ordinary inbox path.
func TestInboxWaitReturnsOnReply(t *testing.T) {
	s, b, me, other := seedInboxWait(t)
	mine := mustPost(t, s, b.ID, me.Handle, "my question", nil)
	mustPost(t, s, b.ID, other.Handle, "old news", nil) // pre-existing: must not trip the wait

	done := runXfaAsync(t, "inbox", "--as", me.Handle, "--wait", "--json=false")
	// Generous pre-sleeps: the snapshot must land before the reply even on a
	// slow -race runner. No exact timing is asserted.
	time.Sleep(300 * time.Millisecond)
	mustPost(t, s, b.ID, other.Handle, "unrelated noise", nil) // not for us: keep waiting
	time.Sleep(300 * time.Millisecond)
	mustPost(t, s, b.ID, other.Handle, "here is your answer", &mine.ID)

	select {
	case out := <-done:
		if strings.HasPrefix(out, "ERR:") {
			t.Fatal(out)
		}
		if !strings.Contains(out, "here is your answer") {
			t.Fatalf("wait output missing the reply:\n%s", out)
		}
		for _, absent := range []string{"old news", "unrelated noise"} {
			if strings.Contains(out, absent) {
				t.Errorf("wait output must only show posts that landed while waiting; got %q:\n%s", absent, out)
			}
		}
	case <-time.After(4 * time.Second):
		t.Fatal("inbox --wait did not return after a reply landed")
	}
}

// Nothing lands: --wait gives up at the deadline with a plain "nothing new"
// line and exit 0, never an error.
func TestInboxWaitTimesOutQuietly(t *testing.T) {
	_, _, me, _ := seedInboxWait(t)
	inboxWaitTimeout = 100 * time.Millisecond

	select {
	case out := <-runXfaAsync(t, "inbox", "--as", me.Handle, "--wait", "--json=false"):
		if strings.HasPrefix(out, "ERR:") {
			t.Fatal(out)
		}
		if want := "nothing new for " + me.Handle; !strings.Contains(out, want) {
			t.Fatalf("timeout output = %q, want %q", out, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inbox --wait did not return at the deadline")
	}
}

// A typo'd handle must fail immediately with ErrNoAgent, not block the full
// deadline: Inbox only runs once the watermark moves, so --wait checks the
// handle itself up front.
func TestInboxWaitUnknownHandleFailsFast(t *testing.T) {
	seedInboxWait(t)
	inboxWaitTimeout = 500 * time.Millisecond // a regression fails in 0.5s, not 9m
	rootCmd.SetArgs([]string{"inbox", "--as", "nope-nope-1", "--wait", "--json=false"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	if err := rootCmd.Execute(); !errors.Is(err, store.ErrNoAgent) {
		t.Fatalf("inbox --wait unknown handle: err = %v, want ErrNoAgent", err)
	}
}
