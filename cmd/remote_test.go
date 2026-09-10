package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/remote"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/pflag"
)

// fakeServer records the last forwarded request and answers with resp.
func fakeServer(t *testing.T, resp remote.Response) (*httptest.Server, *remote.Request, *string) {
	t.Helper()
	var last remote.Request
	var verb string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verb = strings.TrimPrefix(r.URL.Path, "/v1/")
		json.NewDecoder(r.Body).Decode(&last)
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &last, &verb
}

// runRemote is runXfaErr with os.Args set to what a user would have typed,
// since the forwarder reads os.Args (the test binary's own args otherwise).
// It also resets the persistent/local flags the remote tests touch.
func runRemote(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"xfa"}, args...)
	t.Cleanup(func() {
		os.Args = old
		rootCmd.SetIn(nil) // the hook pre-run re-seats root's reader; never leave it behind
		for _, f := range []*pflag.Flag{rootCmd.PersistentFlags().Lookup("json"), resetCmd.Flags().Lookup("yes")} {
			if f != nil {
				_ = f.Value.Set("false")
				f.Changed = false
			}
		}
	})
	return runXfaErr(t, args...)
}

func TestForwardsVerbWithFlagsBeforeIt(t *testing.T) {
	srv, last, verb := fakeServer(t, remote.Response{Stdout: "remote out\n"})
	t.Setenv("XFA_DB", srv.URL)
	t.Setenv("XFA_HANDLE", "amber-otter-1")
	out, err := runRemote(t, "--json", "read")
	var code ExitCode
	if !errors.As(err, &code) || code != 0 {
		t.Fatalf("want ExitCode(0), got %v", err)
	}
	if *verb != "read" || strings.Join(last.Args, " ") != "--json" {
		t.Fatalf("verb=%q args=%v", *verb, last.Args)
	}
	if last.Handle != "amber-otter-1" || !filepath.IsAbs(last.Cwd) {
		t.Fatalf("request = %+v", *last)
	}
	if out != "remote out\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestForwardsNestedSubcommand(t *testing.T) {
	srv, last, verb := fakeServer(t, remote.Response{})
	t.Setenv("XFA_DB", srv.URL)
	runRemote(t, "session", "name", "abc", "my name")
	if *verb != "session" || strings.Join(last.Args, "|") != "name|abc|my name" {
		t.Fatalf("verb=%q args=%v", *verb, last.Args)
	}
}

func TestForwardPropagatesNonZeroExitAndStderr(t *testing.T) {
	srv, _, _ := fakeServer(t, remote.Response{Stderr: "Error: post 12 not found\n", Code: 1})
	t.Setenv("XFA_DB", srv.URL)
	out, err := runRemote(t, "thread", "12")
	var code ExitCode
	if !errors.As(err, &code) || code != 1 || !strings.Contains(out, "post 12 not found") {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

func TestLocalVerbsAreNotForwarded(t *testing.T) {
	srv, _, verb := fakeServer(t, remote.Response{})
	t.Setenv("XFA_DB", srv.URL)
	runRemote(t, "reset", "--yes") // refused locally (Task 9); must never reach the server
	if *verb != "" {
		t.Fatalf("reset was forwarded as %q", *verb)
	}
}

func TestLocalProjectIsNotForwarded(t *testing.T) {
	_, _, verb := fakeServer(t, remote.Response{})
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	runRemote(t, "boards")
	if *verb != "" {
		t.Fatal("local project forwarded")
	}
}

func TestHookForwardsStdinAndFailsOpen(t *testing.T) {
	srv, last, _ := fakeServer(t, remote.Response{Stdout: "{\"x\":1}\n"})
	t.Setenv("XFA_DB", srv.URL)
	rootCmd.SetIn(strings.NewReader(`{"session_id":"s","cwd":"/a/b"}`))
	t.Cleanup(func() { rootCmd.SetIn(nil) })
	out, err := runRemote(t, "hook", "session-start")
	var code ExitCode
	if !errors.As(err, &code) || code != 0 || out != "{\"x\":1}\n" {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if string(last.Stdin) != `{"session_id":"s","cwd":"/a/b"}` || last.Cwd != "/a/b" {
		t.Fatalf("request = %+v", *last)
	}
	// antigravity shape resolves from workspacePaths[0]
	rootCmd.SetIn(strings.NewReader(`{"conversationId":"c","workspacePaths":["/w/p"]}`))
	runRemote(t, "hook", "antigravity-invoke")
	if last.Cwd != "/w/p" {
		t.Fatalf("antigravity cwd = %q", last.Cwd)
	}
	// dead server: nothing printed, exit 0
	t.Setenv("XFA_DB", "http://127.0.0.1:1")
	rootCmd.SetIn(strings.NewReader(`{"session_id":"s","cwd":"/a/b"}`))
	out, err = runRemote(t, "hook", "session-start")
	if !errors.As(err, &code) || code != 0 || out != "" {
		t.Fatalf("dead server: err=%v out=%q", err, out)
	}
}

// A local project: the pre-run reads stdin to find cwd, then must hand the
// same bytes to the hook RunE. Regression guard for a drained stdin.
func TestLocalHookStillReadsPayloadAfterPreRun(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.EnsureBoard("hooklocal", "")
	proj := t.TempDir()
	s.RegisterProject(proj, b.ID)
	a, _ := s.RegisterAgent("claude", "other-session", "")
	s.CreatePost(b.ID, a.Handle, "seen by the hook", "", nil)
	out, err := runXfaWithStdin(t, strings.NewReader(`{"session_id":"fresh","cwd":"`+proj+`"}`), "hook", "session-start")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "b/hooklocal") {
		t.Fatalf("local hook lost its payload (stdin drained by pre-run): %q", out)
	}
}

func TestCwdHelper(t *testing.T) {
	t.Setenv("XFA_CWD", "/from/env")
	if cwd() != "/from/env" {
		t.Fatal("XFA_CWD not honored")
	}
	t.Setenv("XFA_CWD", "")
	wd, _ := os.Getwd()
	if cwd() != wd {
		t.Fatal("fallback to Getwd broken")
	}
}

// A transport failure on a non-hook verb is an ordinary error, not an
// ExitCode: main prints "Error: …" for it like every other CLI failure.
func TestTransportErrorIsAnOrdinaryError(t *testing.T) {
	t.Setenv("XFA_DB", "http://127.0.0.1:1")
	out, err := runRemote(t, "read")
	var code ExitCode
	if err == nil || errors.As(err, &code) {
		t.Fatalf("want a plain error, got %v", err)
	}
	if !strings.Contains(err.Error(), "cannot reach xfa server") || out != "" {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

// A forwarded `inbox --wait` must outlive its own 9m cap.
func TestForwardTimeoutOutlivesInboxWait(t *testing.T) {
	if forwardTimeout <= inboxWaitTimeout {
		t.Fatal("forwardTimeout must exceed inbox --wait's cap")
	}
}
