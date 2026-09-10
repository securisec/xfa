package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

func resetProjectRegisterFlags(t *testing.T) {
	t.Helper()
	f := projectRegisterCmd.Flags().Lookup("board")
	_ = f.Value.Set("")
	f.Changed = false
}

func TestProjectRegisterBindsAndRefusesRebind(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	t.Cleanup(func() { resetProjectRegisterFlags(t) })
	t.Setenv("XFA_CWD", "/client/one")
	out, err := runXfaErr(t, "project-register", "--board", "shared")
	if err != nil || !strings.Contains(out, "b/shared") {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if _, err := runXfaErr(t, "project-register", "--board", "shared"); err != nil {
		t.Fatalf("idempotent re-register: %v", err)
	}
	// another machine's path deriving the same slug shares the board
	t.Setenv("XFA_CWD", "/client/two/shared")
	if _, err := runXfaErr(t, "project-register", "--board", "shared"); err != nil {
		t.Fatal(err)
	}
	// rebinding an existing path to a different board is refused
	t.Setenv("XFA_CWD", "/client/one")
	_, err = runXfaErr(t, "project-register", "--board", "other")
	if err == nil || !strings.Contains(err.Error(), "already bound to b/shared") {
		t.Fatalf("rebind: %v", err)
	}
	s, _ := store.Open(dbPath)
	p, err := s.ResolveProject("/client/one")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.GetBoardBySlug("shared")
	if p.BoardID != b.ID {
		t.Fatal("rebind changed the binding")
	}
}

// The guard must compare the STORED form: a client path under a symlinked
// ancestor that exists on the server (macOS /tmp → /private/tmp) is stored
// resolved, and a re-register spelled through the symlink must still match.
func TestProjectRegisterRebindGuardSeesThroughSymlinks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	t.Cleanup(func() { resetProjectRegisterFlags(t) })
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	t.Setenv("XFA_CWD", filepath.Join(real, "proj"))
	if _, err := runXfaErr(t, "project-register", "--board", "alpha"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XFA_CWD", filepath.Join(link, "proj"))
	_, err := runXfaErr(t, "project-register", "--board", "beta")
	if err == nil || !strings.Contains(err.Error(), "already bound to b/alpha") {
		t.Fatalf("symlinked rebind slipped through: %v", err)
	}
}

// The guard runs in both directions: a descendant must not carve a different
// board out of a registered ancestor, and an ancestor must not capture an
// already-registered descendant's walk-up.
func TestProjectRegisterRefusesAncestorAndDescendantCapture(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	t.Cleanup(func() { resetProjectRegisterFlags(t) })

	t.Setenv("XFA_CWD", "/host/proj")
	if _, err := runXfaErr(t, "project-register", "--board", "hostboard"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XFA_CWD", "/host/proj/sub")
	if _, err := runXfaErr(t, "project-register", "--board", "other"); err == nil ||
		!strings.Contains(err.Error(), "is already inside b/hostboard") {
		t.Fatalf("descendant on another board: %v", err)
	}
	// the same board is not a split, so it still registers
	if _, err := runXfaErr(t, "project-register", "--board", "hostboard"); err != nil {
		t.Fatalf("same-board descendant: %v", err)
	}
	// registering an ancestor would capture the walk-up of both
	t.Setenv("XFA_CWD", "/host")
	_, err := runXfaErr(t, "project-register", "--board", "hostboard")
	if err == nil || !strings.Contains(err.Error(), "would capture /host/proj") {
		t.Fatalf("ancestor: %v", err)
	}
	// A multibyte component must not slip past: SQLite counts TEXT in
	// characters, so a byte length handed to substr would misalign here.
	t.Setenv("XFA_CWD", "/hôst/proj")
	if _, err := runXfaErr(t, "project-register", "--board", "accented"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XFA_CWD", "/hôst")
	_, err = runXfaErr(t, "project-register", "--board", "capturer")
	if err == nil || !strings.Contains(err.Error(), "would capture /hôst/proj") {
		t.Fatalf("multibyte ancestor: %v", err)
	}
}

func TestProjectRegisterIsHidden(t *testing.T) {
	if !projectRegisterCmd.Hidden {
		t.Fatal("project-register must be hidden")
	}
}
