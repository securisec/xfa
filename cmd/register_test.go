package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

func registeredRepo(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	t.Setenv("XFA_DB", dbPath)
	handle := strings.TrimSpace(runXfa(t, append([]string{"register", "--session", "s"}, args...)...))
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.GetAgent(handle)
	if err != nil {
		t.Fatal(err)
	}
	return a.Repo
}

// --repo is stored verbatim; without it, register labels the agent with the
// name of the enclosing git checkout, or the cwd when there is none.
func TestRegisterRepo(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "board.db")
	if err := os.MkdirAll(filepath.Join(root, "myrepo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "myrepo", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(filepath.Join(root, "myrepo", "sub"))
	if got := registeredRepo(t, dbPath, "--repo", "explicit"); got != "explicit" {
		t.Errorf("--repo not stored: %q", got)
	}
	if got := registeredRepo(t, dbPath, "--repo", ""); got != "myrepo" {
		t.Errorf("default should be the git checkout name, got %q", got)
	}
	t.Chdir(filepath.Join(root, "plain"))
	if got := registeredRepo(t, dbPath, "--repo", ""); got != "plain" {
		t.Errorf("default without .git should be the cwd name, got %q", got)
	}
}

// The repo label follows the handle on every listing, text and --json alike.
func TestRepoLabelAcrossListings(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureBoard("cmdrepo", "")
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := s.RegisterAgentWithRepo("claude", "repo-sess", "", "repo1")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.RegisterAgent("claude", "plain-sess", "")
	if err != nil {
		t.Fatal(err)
	}
	mustPostTagged(t, s, b.ID, tagged.Handle, "why does the cache miss", "question", nil)
	root := mustPost(t, s, b.ID, plain.Handle, "plain root", nil)
	mustPost(t, s, b.ID, tagged.Handle, "reply mentioning @"+plain.Handle, &root.ID)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"questions", []string{"questions", "--board", "cmdrepo", "--all=false", "--limit", "20"}},
		{"threads", []string{"threads", "--board", "cmdrepo", "--session", "", "--limit", "50"}},
		{"read", []string{"read", "--board", "cmdrepo", "--as", plain.Handle, "--unread=false", "--limit", "20"}},
		{"thread", []string{"thread", postArg(root.ID)}},
		{"inbox", []string{"inbox", "--as", plain.Handle, "--limit", "20"}},
		{"search", []string{"search", "cache", "--board", "cmdrepo", "--all=false", "--limit", "20"}},
	} {
		out := runXfa(t, append(append([]string{}, tc.args...), "--json=false")...)
		if !strings.Contains(out, tagged.Handle+" (repo1) (") {
			t.Errorf("%s text: repo label missing:\n%s", tc.name, out)
		}
		if strings.Contains(out, plain.Handle+" (plain") || strings.Contains(out, "() (") {
			t.Errorf("%s text: unlabeled handle must render as before:\n%s", tc.name, out)
		}
		js := runXfa(t, append(append([]string{}, tc.args...), "--json=true")...)
		if !strings.Contains(js, `"repo":"repo1"`) {
			t.Errorf("%s --json: repo field missing:\n%s", tc.name, js)
		}
		if strings.Contains(js, `"repo":""`) {
			t.Errorf("%s --json: empty repo must be omitted:\n%s", tc.name, js)
		}
	}
}
