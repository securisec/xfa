package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func claudeMd(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	return string(b)
}

// init creates CLAUDE.md with the managed awareness block when none exists.
func TestInstallClaudeCreatesClaudeMd(t *testing.T) {
	dir := t.TempDir()
	if err := InstallClaude(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	got := claudeMd(t, dir)
	for _, must := range []string{
		awarenessBeginMarker, awarenessEndMarker,
		"Every agent uses xfa",
		"Awareness does not arrive on its own",
		"xfa reply <id>",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("created CLAUDE.md missing %q:\n%s", must, got)
		}
	}
}

// init appends the block to an existing CLAUDE.md without disturbing user
// content, and backs the original up.
func TestInstallClaudePreservesExistingClaudeMd(t *testing.T) {
	dir := t.TempDir()
	orig := "# My Project\n\nSome important project guidance the user wrote.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	got := claudeMd(t, dir)
	if !strings.Contains(got, "Some important project guidance the user wrote.") {
		t.Errorf("user content lost:\n%s", got)
	}
	if !strings.Contains(got, awarenessBeginMarker) {
		t.Errorf("managed block not appended:\n%s", got)
	}
	if idx := strings.Index(got, "# My Project"); idx != 0 {
		t.Errorf("user content should remain at the top, block appended after; got:\n%s", got)
	}
	bak, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md.xfa-bak"))
	if err != nil || string(bak) != orig {
		t.Errorf("backup missing or wrong: err=%v content=%q", err, string(bak))
	}
}

// re-init does not duplicate the block; a changed block body replaces in place.
func TestInstallClaudeClaudeMdIdempotentAndUpgrades(t *testing.T) {
	dir := t.TempDir()
	// Seed a CLAUDE.md that already carries an OLD xfa block (stale body).
	old := "# Proj\n\n" + awarenessBeginMarker + "\n## xfa\nold stale wording\n" + awarenessEndMarker + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/x/xfa"); err != nil { // second init, same content
		t.Fatal(err)
	}
	got := claudeMd(t, dir)
	if n := strings.Count(got, awarenessBeginMarker); n != 1 {
		t.Errorf("begin marker appears %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, awarenessEndMarker); n != 1 {
		t.Errorf("end marker appears %d times, want 1:\n%s", n, got)
	}
	if strings.Contains(got, "old stale wording") {
		t.Errorf("stale block body not replaced:\n%s", got)
	}
	if !strings.Contains(got, "Every agent uses xfa") {
		t.Errorf("current block body missing after upgrade:\n%s", got)
	}
	if !strings.Contains(got, "# Proj") {
		t.Errorf("surrounding user content lost on upgrade:\n%s", got)
	}
}

// uninstall strips the block; a file that held only our block is removed,
// while user content is preserved with just the block gone.
func TestUninstallClaudeRemovesAwarenessBlock(t *testing.T) {
	// Case A: CLAUDE.md created by init (block only) → file removed.
	dirA := t.TempDir()
	if err := InstallClaude(dirA, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClaude(dirA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dirA, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("block-only CLAUDE.md should be removed on uninstall, stat err=%v", err)
	}

	// Case B: user content present → file kept, block gone.
	dirB := t.TempDir()
	orig := "# Keep Me\n\nUser guidance.\n"
	if err := os.WriteFile(filepath.Join(dirB, "CLAUDE.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dirB, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClaude(dirB); err != nil {
		t.Fatal(err)
	}
	got := claudeMd(t, dirB)
	if strings.Contains(got, awarenessBeginMarker) || strings.Contains(got, "Every agent uses xfa") {
		t.Errorf("managed block not removed:\n%s", got)
	}
	if !strings.Contains(got, "User guidance.") {
		t.Errorf("user content lost on uninstall:\n%s", got)
	}
}

// A pre-rename ("xaf") skill dir under .claude/skills is cleared on install so
// a re-init migrates the project off the old name.
func TestInstallClaudeRemovesLegacyXafSkill(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, ".claude", "skills", "xaf")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "SKILL.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy .claude/skills/xaf survived install (err=%v)", err)
	}
	// The current skill is still installed.
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "xfa", "SKILL.md")); err != nil {
		t.Errorf("current xfa skill missing after install: %v", err)
	}
}

// A CLAUDE.md with no xfa markers is never touched by uninstall.
func TestUninstallClaudeLeavesForeignClaudeMdAlone(t *testing.T) {
	dir := t.TempDir()
	orig := "# Someone Else's File\n\nNo xfa here.\n"
	p := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(p, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	if got := claudeMd(t, dir); got != orig {
		t.Errorf("foreign CLAUDE.md modified:\ngot:  %q\nwant: %q", got, orig)
	}
}
