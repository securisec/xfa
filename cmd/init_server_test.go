package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/remote"
	"github.com/securisec/xfa/internal/store"
)

// resetInitFlags zeroes init's db/server/global flags (cobra keeps them
// across Execute calls; a stale --db would trip the mutual-exclusion group).
func resetInitFlags(t *testing.T) {
	t.Helper()
	for name, def := range map[string]string{"db": "", "server": "", "global": "false", "board": ""} {
		f := initCmd.Flags().Lookup(name)
		_ = f.Value.Set(def)
		f.Changed = false
	}
}

// remoteProject is markerProject (temp cwd, XFA_DB unset, XDG guarded,
// provider flags reset) plus init-flag hygiene. Returns the cwd exactly as
// os.Getwd() reports it: t.Chdir sets PWD, so no symlink resolution.
func remoteProject(t *testing.T) string {
	t.Helper()
	dir, _ := markerProject(t)
	resetInitFlags(t)
	t.Cleanup(func() { resetInitFlags(t) })
	return dir
}

func TestInitServerWritesMarkerRegistersAndInstalls(t *testing.T) {
	dir := remoteProject(t)
	srv, last, verb := fakeServer(t, remote.Response{Stdout: "registered x \u2192 b/foo\n"})
	out, err := runXfaErr(t, "init", "--server", srv.URL, "--provider", "claude")
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	// No --board: the client forwards no slug and JOINS the server's board.
	if *verb != "project-register" || len(last.Args) != 0 || last.Cwd != dir {
		t.Fatalf("verb=%q req=%+v dir=%s", *verb, *last, dir)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, store.MarkerName))
	if !strings.Contains(string(raw), `"db":"`+srv.URL+`"`) {
		t.Fatalf("marker = %s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, store.LocalDirName)); err == nil {
		t.Fatal("remote init must not create .xfa/")
	}
	if !strings.Contains(out, "pinned server "+srv.URL) || !strings.Contains(out, "installed provider: claude") || !strings.Contains(out, "board b/foo ready") {
		t.Fatalf("out = %q", out)
	}
}

// An explicit --board is still forwarded verbatim (overrides the join).
func TestInitServerExplicitBoardForwarded(t *testing.T) {
	remoteProject(t)
	srv, last, verb := fakeServer(t, remote.Response{Stdout: "registered x \u2192 b/shared\n"})
	out, err := runXfaErr(t, "init", "--server", srv.URL, "--board", "b/shared", "--provider", "claude")
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if *verb != "project-register" || strings.Join(last.Args, " ") != "--board shared" {
		t.Fatalf("verb=%q args=%v", *verb, last.Args)
	}
	if !strings.Contains(out, "board b/shared ready") {
		t.Fatalf("out = %q", out)
	}
}

func TestInitServerRollsBackMarkerOnFailure(t *testing.T) {
	dir := remoteProject(t)
	srv, _, _ := fakeServer(t, remote.Response{Code: 1, Stderr: "Error: boom\n"})
	_, err := runXfaErr(t, "init", "--server", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, store.MarkerName)); serr == nil {
		t.Fatal("marker left behind after failed remote init")
	}
	resetInitFlags(t)
	_, err = runXfaErr(t, "init", "--server", "http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "cannot reach xfa server") {
		t.Fatalf("dead: %v", err)
	}
	// a pre-existing marker is restored on failure — the OLD pin, not the URL
	// this run tried and failed to register with
	resetInitFlags(t)
	store.WriteMarker(dir, "http://old-server:1234")
	before, _ := os.ReadFile(filepath.Join(dir, store.MarkerName))
	runXfaErr(t, "init", "--server", "http://127.0.0.1:1")
	after, serr := os.ReadFile(filepath.Join(dir, store.MarkerName))
	if serr != nil {
		t.Fatal("pre-existing marker was removed")
	}
	if string(after) != string(before) {
		t.Fatalf("marker = %s, want the old pin %s", after, before)
	}
}

func TestInitServerRefusals(t *testing.T) {
	dir := remoteProject(t)
	for _, c := range []struct{ args, want string }{
		{"ftp://x", "must be an http(s) URL"},
		{"http://u:p@x", "must be an http(s) URL"},
	} {
		resetInitFlags(t)
		if _, err := runXfaErr(t, "init", "--server", c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", c.args, err)
		}
	}
	for _, other := range [][]string{{"--db", "x.db"}, {"--global"}} {
		resetInitFlags(t)
		if _, err := runXfaErr(t, append([]string{"init", "--server", "http://h"}, other...)...); err == nil {
			t.Fatalf("%v with --server should be refused", other)
		}
	}
	for _, env := range []string{"/some/file.db", "http://other:1"} {
		resetInitFlags(t)
		t.Setenv("XFA_DB", env)
		if _, err := runXfaErr(t, "init", "--server", "http://h"); err == nil || !strings.Contains(err.Error(), "would shadow the server pin") {
			t.Fatalf("XFA_DB=%s: %v", env, err)
		}
	}
	t.Setenv("XFA_DB", "")
	resetInitFlags(t)
	os.MkdirAll(filepath.Join(dir, store.LocalDirName), 0o755)
	if _, err := runXfaErr(t, "init", "--server", "http://h"); err == nil || !strings.Contains(err.Error(), "has a local .xfa/ database") {
		t.Fatalf(".xfa/: %v", err)
	}
}

func TestBareInitUnderResolvedURLTakesRemoteBranch(t *testing.T) {
	dir := remoteProject(t)
	srv, _, verb := fakeServer(t, remote.Response{Stdout: "registered x → b/foo\n"})
	store.WriteMarker(dir, srv.URL)
	out, err := runXfaErr(t, "init", "--provider", "claude")
	if err != nil || *verb != "project-register" || !strings.Contains(out, "using database "+srv.URL) {
		t.Fatalf("marker: err=%v verb=%q out=%q", err, *verb, out)
	}
	os.Remove(filepath.Join(dir, store.MarkerName))
	*verb = ""
	resetInitFlags(t)
	t.Setenv("XFA_DB", srv.URL)
	if _, err := runXfaErr(t, "init", "--provider", "claude"); err != nil || *verb != "project-register" {
		t.Fatalf("XFA_DB: err=%v verb=%q", err, *verb)
	}
	if _, serr := os.Stat(filepath.Join(dir, store.MarkerName)); serr == nil {
		t.Fatal("bare init must not write a marker")
	}
}

// twoBoardServer routes /v1/boards (two boards) and /v1/project-register
// (echoes the chosen board), capturing the project-register args.
func twoBoardServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var regArgs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verb := strings.TrimPrefix(r.URL.Path, "/v1/")
		var req remote.Request
		json.NewDecoder(r.Body).Decode(&req)
		switch verb {
		case "boards":
			json.NewEncoder(w).Encode(remote.Response{Stdout: `[{"Slug":"pwn"},{"Slug":"other"}]`})
		case "project-register":
			if len(req.Args) == 0 {
				// no --board: the server cannot choose among two boards, so it
				// rejects — exactly what drives the client's interactive pick.
				json.NewEncoder(w).Encode(remote.Response{Code: 1,
					Stderr: "Error: server has 2 boards (b/other, b/pwn) — pass --board <slug>\n"})
				return
			}
			regArgs = req.Args
			slug := req.Args[1]
			json.NewEncoder(w).Encode(remote.Response{Stdout: "registered x → b/" + slug + "\n"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &regArgs
}

// A TTY init --server against a multi-board server prompts; the chosen board
// is what reaches project-register, and it is what init announces.
func TestInitServerPicksBoardInteractively(t *testing.T) {
	old := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = old })

	for _, c := range []struct {
		name, input, wantArg, wantBoard string
	}{
		{"by number", "2\n", "--board other", "b/other ready"},
		{"first by number", "1\n", "--board pwn", "b/pwn ready"},
		{"default is first", "\n", "--board pwn", "b/pwn ready"},
		{"new name scrubbed", "Fresh Board!\n", "--board fresh-board", "b/fresh-board ready"},
	} {
		t.Run(c.name, func(t *testing.T) {
			remoteProject(t)
			srv, regArgs := twoBoardServer(t)
			rootCmd.SetIn(strings.NewReader(c.input))
			out, err := runXfaErr(t, "init", "--server", srv.URL, "--provider", "claude")
			if err != nil {
				t.Fatalf("err=%v out=%q", err, out)
			}
			if strings.Join(*regArgs, " ") != c.wantArg {
				t.Fatalf("regArgs = %v, want %q", *regArgs, c.wantArg)
			}
			if !strings.Contains(out, c.wantBoard) {
				t.Fatalf("out = %q, want %q", out, c.wantBoard)
			}
		})
	}
}

// An out-of-range number is rejected and the marker rolled back.
func TestInitServerPickerRejectsBadChoice(t *testing.T) {
	old := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = old })
	// A plain out-of-range number and a number too big for int both reject,
	// rather than the latter falling through to create a board named "999…".
	for _, in := range []string{"9\n", "99999999999999999999\n"} {
		t.Run(strings.TrimSpace(in), func(t *testing.T) {
			dir := remoteProject(t)
			srv, _ := twoBoardServer(t)
			rootCmd.SetIn(strings.NewReader(in))
			if _, err := runXfaErr(t, "init", "--server", srv.URL); err == nil || !strings.Contains(err.Error(), "out of range") {
				t.Fatalf("err = %v", err)
			}
			if _, serr := os.Stat(filepath.Join(dir, store.MarkerName)); serr == nil {
				t.Fatal("marker left behind after a rejected pick")
			}
		})
	}
}

// An explicit --board never prompts, even on a TTY against a multi-board
// server.
func TestInitServerExplicitBoardSkipsPicker(t *testing.T) {
	remoteProject(t)
	old := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = old })
	srv, regArgs := twoBoardServer(t)
	rootCmd.SetIn(strings.NewReader("2\n")) // would pick "other" if consulted
	out, err := runXfaErr(t, "init", "--server", srv.URL, "--board", "b/mine", "--provider", "claude")
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if strings.Join(*regArgs, " ") != "--board mine" {
		t.Fatalf("regArgs = %v", *regArgs)
	}
}

// On a TTY, a server that accepts the no-board register (sole board, or this
// dir already bound) is joined silently — the picker never appears.
func TestInitServerTTYSingleBoardNoPrompt(t *testing.T) {
	old := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = old })
	remoteProject(t)
	srv, last, _ := fakeServer(t, remote.Response{Stdout: "registered x → b/pwn\n"})
	rootCmd.SetIn(strings.NewReader("2\n")) // must be ignored: no ambiguity
	out, err := runXfaErr(t, "init", "--server", srv.URL, "--provider", "claude")
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if len(last.Args) != 0 {
		t.Fatalf("forwarded a board unexpectedly: %v", last.Args)
	}
	if strings.Contains(out, "server has") || !strings.Contains(out, "board b/pwn ready") {
		t.Fatalf("out = %q", out)
	}
}

// EOF at the picker (Ctrl-D) aborts cleanly and writes no marker.
func TestInitServerPickerEOFAbortsWithoutMarker(t *testing.T) {
	dir := remoteProject(t)
	old := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = old })
	srv, _ := twoBoardServer(t)
	rootCmd.SetIn(strings.NewReader("")) // immediate EOF
	if _, err := runXfaErr(t, "init", "--server", srv.URL); err == nil || !strings.Contains(err.Error(), "no board selected") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, store.MarkerName)); serr == nil {
		t.Fatal("marker written despite an aborted pick")
	}
}
