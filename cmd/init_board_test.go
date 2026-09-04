package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An explicit --board is intent to share: several projects may bind the same
// slug under --global and end up on one board.
func TestInitExplicitBoardSharesAcrossProjects(t *testing.T) {
	first, globalDB := localProject(t)
	second := t.TempDir()

	runXfa(t, "init", "--board", "shared", "--db", "", "--global", "--provider", "claude")
	t.Chdir(second)
	runXfa(t, "init", "--board", "shared", "--db", "", "--global", "--provider", "claude")

	for _, dir := range []string{first, second} {
		if !projectRegistered(t, globalDB, dir) {
			t.Fatalf("%s must be registered", dir)
		}
	}
	if n := boardCount(t, globalDB); n != 1 {
		t.Fatalf("want exactly one board, got %d", n)
	}
	if !boardExists(t, globalDB, "shared") {
		t.Fatal("board shared must exist")
	}
}

// A DERIVED slug colliding with another project's is still refused, and the
// error hints at --board <slug> to share deliberately.
func TestInitDerivedBoardCollisionRefused(t *testing.T) {
	_, globalDB := localProject(t)
	first := filepath.Join(t.TempDir(), "api")
	second := filepath.Join(t.TempDir(), "api")
	for _, d := range []string{first, second} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(first)
	runXfa(t, "init", "--board", "", "--db", "", "--global", "--provider", "claude")
	t.Chdir(second)
	_, err := runXfaErr(t, "init", "--board", "", "--db", "", "--global", "--provider", "claude")
	if err == nil {
		t.Fatal("derived slug collision must be refused")
	}
	if !strings.Contains(err.Error(), "already bound") || !strings.Contains(err.Error(), "--board api") {
		t.Fatalf("error must say already bound and hint --board api, got: %v", err)
	}
	if projectRegistered(t, globalDB, second) {
		t.Fatal("refused init must not register the second project")
	}
}
