package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/securisec/xfa/internal/store"
	web "github.com/securisec/xfa/internal/web"
)

// `xfa tui </dev/null` is the reviewer's bypass of the char-device stat:
// /dev/null IS a char device but NOT a terminal, so term.IsTerminal must
// refuse — before the DB is even created. stdinCharDevice (reset_test.go)
// swaps os.Stdin itself for /dev/null, so this exercises the real gate.
func TestTuiRefusesCharDeviceNonTerminal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	stdinCharDevice(t)

	out, err := runXfaErr(t, "tui")
	if err == nil {
		t.Fatalf("tui with /dev/null stdin must fail, output %q", out)
	}
	if !strings.Contains(err.Error(), "human-only") {
		t.Errorf("want the human-only refusal, got %v", err)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Errorf("refused tui must not create the DB, stat: %v", statErr)
	}
}

// tui is HUMAN-ONLY: on non-terminal stdin (an agent's pipe) it must refuse
// outright, BEFORE ever opening the database — same gate order as reset.
func TestTuiRefusesWithoutTerminal(t *testing.T) {
	dbPath := seedResetDB(t) // temp XFA_DB with one board/agent/post, pool closed

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
	w.Close()

	out, err := runXfaErr(t, "tui")
	if err == nil {
		t.Fatalf("tui on piped stdin must fail, output %q", out)
	}
	if !strings.Contains(err.Error(), "human-only") ||
		!strings.Contains(err.Error(), "`xfa threads`") ||
		!strings.Contains(err.Error(), "`xfa board`") {
		t.Errorf("want the human-only redirection to the agent commands, got %v", err)
	}
	// The refusal must precede store.Open: an open would materialize a -wal
	// sidecar (seedResetDB closed the pool cleanly).
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Errorf("refused tui must leave the DB untouched: %v", statErr)
	}
	if _, statErr := os.Stat(dbPath + "-wal"); !os.IsNotExist(statErr) {
		t.Errorf("refused tui must not open the DB (found -wal sidecar): %v", statErr)
	}
}

// --web is still the HUMAN-ONLY tui command: the TTY gate runs FIRST, so an
// agent probing the web server (with or without --port) hits the same refusal
// before a listener, a DB, or a human handle is ever created.
func TestTuiWebRefusesWithoutTerminal(t *testing.T) {
	for _, args := range [][]string{
		{"tui", "--web"},
		{"tui", "--web", "--port", "8080"},
		{"tui", "--port", "8080"}, // gate precedes the --port/--web check
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			resetTuiFlags(t)
			dbPath := filepath.Join(t.TempDir(), "board.db")
			t.Setenv("XFA_DB", dbPath)

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
			w.Close()

			out, err := runXfaErr(t, args...)
			if err == nil {
				t.Fatalf("%v on piped stdin must fail, output %q", args, out)
			}
			if !strings.Contains(err.Error(), "human-only") {
				t.Errorf("want the human-only refusal, got %v", err)
			}
			if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
				t.Errorf("refused tui must not create the DB, stat: %v", statErr)
			}
		})
	}
}

// --port is meaningless without --web: rejected loudly rather than ignored.
// Checked on the flag set directly because the human-only gate (correctly)
// fires before this check for every non-terminal invocation.
func TestTuiPortRequiresWeb(t *testing.T) {
	parse := func(t *testing.T, args ...string) *cobra.Command {
		t.Helper()
		c := &cobra.Command{Use: "tui"}
		registerTuiWebFlags(c)
		if err := c.ParseFlags(args); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		return c
	}

	if _, _, err := webFlags(parse(t, "--port", "8080")); err == nil ||
		!strings.Contains(err.Error(), "--port requires --web") {
		t.Errorf("--port without --web must be rejected, got %v", err)
	}
	web, port, err := webFlags(parse(t, "--web", "--port", "8080"))
	if err != nil || !web || port != 8080 {
		t.Errorf("--web --port 8080: got web=%v port=%d err=%v", web, port, err)
	}
	if web, port, err := webFlags(parse(t)); err != nil || web || port != 0 {
		t.Errorf("no flags: got web=%v port=%d err=%v", web, port, err)
	}
}

// resetTuiFlags restores tuiCmd's flags after a test: rootCmd is a package
// global, so flag values (and their Changed bits) survive an Execute.
func resetTuiFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for name, zero := range map[string]string{"web": "false", "port": "0", "board": ""} {
			f := tuiCmd.Flags().Lookup(name)
			f.Value.Set(zero)
			f.Changed = false
		}
	})
}

// fakeTTY patches the gate's seam so a test can exercise the code BELOW the
// human-only refusal. The refusal tests above deliberately do not use it —
// they run the real stdinIsTTY.
func fakeTTY(t *testing.T) {
	t.Helper()
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
}

// serveCall records what cmd/tui.go's --web branch hands to web.Serve.
type serveCall struct {
	calls    int
	ctx      context.Context
	ctxErrIn error // ctx.Err() observed *inside* the call
	store    *store.Store
	opts     web.Options
}

func stubServeWebUI(t *testing.T, ret error) *serveCall {
	t.Helper()
	c := &serveCall{}
	orig := serveWebUI
	serveWebUI = func(ctx context.Context, s *store.Store, o web.Options) error {
		c.calls++
		c.ctx, c.store, c.opts, c.ctxErrIn = ctx, s, o, ctx.Err()
		fmt.Fprintln(o.Out, "stub-serve-banner")
		return ret
	}
	t.Cleanup(func() { serveWebUI = orig })
	return c
}

// Past the gate (a real human at a terminal), --port without --web is a loud
// error, not a silently ignored flag — and the server is never started.
func TestTuiPortWithoutWebIsRejected(t *testing.T) {
	t.Setenv("XFA_DB", filepath.Join(t.TempDir(), "board.db"))
	fakeTTY(t)
	resetTuiFlags(t)
	called := stubServeWebUI(t, nil)

	out, err := runXfaErr(t, "tui", "--port", "8080")
	if err == nil || !strings.Contains(err.Error(), "--port requires --web") {
		t.Fatalf("want the --port/--web error, got err=%v output=%q", err, out)
	}
	if called.calls != 0 {
		t.Errorf("--port without --web must not start the web UI (calls=%d)", called.calls)
	}
}

// The --web wiring: flags and the resolved board must actually reach Serve,
// under a cancellable (signal) context, writing to the command's stdout, and
// Serve's error must propagate out of RunE.
func TestTuiWebInvokesServeWithResolvedBoard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := s.EnsureBoard("webwire", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	if sqlDB, err := s.DB.DB(); err == nil {
		sqlDB.Close()
	}

	t.Run("explicit board", func(t *testing.T) {
		fakeTTY(t)
		resetTuiFlags(t)
		sentinel := errors.New("serve exploded")
		called := stubServeWebUI(t, sentinel)

		out, err := runXfaErr(t, "tui", "--web", "--port", "8123", "--board", "b/webwire")
		if !errors.Is(err, sentinel) {
			t.Fatalf("Serve's error must propagate, got %v (output %q)", err, out)
		}
		if called.calls != 1 {
			t.Fatalf("--web must invoke Serve exactly once, got %d", called.calls)
		}
		if called.opts.InitialBoard != "webwire" {
			t.Errorf("resolved board must reach Serve, got %q", called.opts.InitialBoard)
		}
		if called.opts.Port != 8123 {
			t.Errorf("--port must reach Serve, got %d", called.opts.Port)
		}
		if !called.opts.OpenBrowser {
			t.Error("the cmd path must ask Serve to open a browser")
		}
		if called.store == nil {
			t.Error("Serve must get the opened store")
		}
		// Out is the command's stdout, not os.Stdout.
		if !strings.Contains(out, "stub-serve-banner") {
			t.Errorf("Options.Out must be the command's writer, got %q", out)
		}
		// signal.NotifyContext returns a cancellable ctx; cmd.Context() (a
		// plain Background) has a nil Done channel, so this pins the wiring.
		if called.ctx == nil || called.ctx.Done() == nil {
			t.Errorf("Serve must run under the cancellable signal context, got %v", called.ctx)
		}
		if called.ctxErrIn != nil {
			t.Errorf("the signal context must still be live inside Serve: %v", called.ctxErrIn)
		}
		// ...and RunE's `defer stop()` must release the signal handler on the
		// way out, which cancels that same context.
		if called.ctx.Err() == nil {
			t.Error("RunE must stop() the signal context when Serve returns")
		}
	})

	t.Run("unresolvable board", func(t *testing.T) {
		fakeTTY(t)
		resetTuiFlags(t)
		called := stubServeWebUI(t, nil)

		out, err := runXfaErr(t, "tui", "--web")
		if err != nil {
			t.Fatalf("tui --web with no board must succeed, err=%v output=%q", err, out)
		}
		if called.calls != 1 || called.opts.InitialBoard != "" {
			t.Errorf("unresolved cwd must pass an empty slug, calls=%d slug=%q",
				called.calls, called.opts.InitialBoard)
		}
		if called.opts.Port != 0 {
			t.Errorf("default port must be 0 (random free port), got %d", called.opts.Port)
		}
	})
}
