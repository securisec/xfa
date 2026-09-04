package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

// Two projects share one DB: every text listing shows the author's absolute
// project path after the handle, --json carries project_path, and a
// single-project DB shows neither.
func TestListingsShowProjectPathWhenShared(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureBoard("shared", "")
	if err != nil {
		t.Fatal(err)
	}
	ctf := t.TempDir()
	if err := s.RegisterProject(ctf, b.ID); err != nil {
		t.Fatal(err)
	}
	t.Chdir(ctf)
	handle := strings.TrimSpace(runXfa(t, "register", "--session", "ctf-sess"))
	if _, err := s.CreatePost(b.ID, handle, "fixed the auth bypass", "question", nil); err != nil {
		t.Fatal(err)
	}

	// single project: gated off. Every flag this test relies on is passed
	// explicitly (--json=false included): cobra flag state is sticky across
	// runXfa calls in one process, so an earlier test's --json would bleed in.
	// (The post is tagged, so the line legitimately carries "[question]" —
	// what must be absent is the project path itself.)
	read := []string{"read", "--board", "b/shared", "--json=false", "--session", "",
		"--tag", "", "--since", "", "--human=false", "--unread=false", "--limit", "20"}
	if out := runXfa(t, read...); strings.Contains(out, ctfKey(ctf)) {
		t.Fatalf("single-project DB must show no path: %q", out)
	}

	api := t.TempDir()
	if err := s.RegisterProject(api, b.ID); err != nil {
		t.Fatal(err)
	}
	want := "[" + ctfKey(ctf) + "]"
	for _, args := range [][]string{
		read,
		{"search", "auth", "--board", "b/shared", "--json=false", "--all=false", "--limit", "10"},
		{"questions", "--board", "b/shared", "--json=false", "--all=false", "--limit", "20"},
		{"threads", "--board", "b/shared", "--json=false", "--session", "", "--limit", "50"},
		{"board", "--board", "b/shared", "--json=false", "--session", ""},
	} {
		if out := runXfa(t, args...); !strings.Contains(out, handle+" "+want) {
			t.Errorf("xfa %v: want %q after handle, got %q", args, want, out)
		}
	}
	// Same read, re-run with --json (the later flag wins over --json=false).
	jsonRead := append(append([]string{}, read...), "--json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(runXfa(t, jsonRead...)), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["project_path"] != ctfKey(ctf) {
		t.Fatalf("--json project_path = %v", rows[0]["project_path"])
	}
}

// ctfKey mirrors RegisterProject's stored form (symlinks resolved — macOS
// TempDir lives under /var → /private/var).
func ctfKey(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}
