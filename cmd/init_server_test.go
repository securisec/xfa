package cmd

import (
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
	srv, last, verb := fakeServer(t, remote.Response{})
	out, err := runXfaErr(t, "init", "--server", srv.URL, "--provider", "claude")
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if *verb != "project-register" || strings.Join(last.Args, " ") != "--board "+filepath.Base(dir) || last.Cwd != dir {
		t.Fatalf("verb=%q req=%+v dir=%s", *verb, *last, dir)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, store.MarkerName))
	if !strings.Contains(string(raw), `"db":"`+srv.URL+`"`) {
		t.Fatalf("marker = %s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, store.LocalDirName)); err == nil {
		t.Fatal("remote init must not create .xfa/")
	}
	if !strings.Contains(out, "pinned server "+srv.URL) || !strings.Contains(out, "installed provider: claude") {
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
	srv, _, verb := fakeServer(t, remote.Response{})
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
