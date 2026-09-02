package install

// pi provider tests, mirroring opencode_test.go case-for-case where the
// mechanisms overlap (see pi.go's header for the researched pi facts):
//   - Extensions auto-load from .pi/extensions/*.ts (jiti, no build step, no
//     config file to edit), so install never writes any JSON config.
//   - Skills are Claude-compatible at .pi/skills/<name>/SKILL.md.
//   - Context is injected via the before_agent_start event's returned
//     { message } — once per process per session for the session-start digest,
//     then the throttled user-prompt nudge on every later prompt.
//   - No legacy "xaf" cleanup: pi postdates the rename.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/skill"
)

func TestInstallPiWritesExtensionAndSkill(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	ts, err := os.ReadFile(filepath.Join(dir, ".pi", "extensions", "xfa.ts"))
	if err != nil {
		t.Fatal("extension file missing")
	}
	// Exe path is quoted into a JS string literal (execFile's file argument),
	// matching the claude.go/opencode.go %EXE% convention.
	if !strings.Contains(string(ts), `"/usr/local/bin/xfa"`) {
		t.Errorf("extension does not embed the quoted xfa path:\n%s", ts)
	}
	if !strings.Contains(string(ts), `"before_agent_start"`) {
		t.Errorf("extension does not use the before_agent_start event:\n%s", ts)
	}
	// Both hook subcommands must be reachable: session-start on the first
	// prompt of a session, user-prompt on every later one.
	if !strings.Contains(string(ts), `"session-start"`) || !strings.Contains(string(ts), `"user-prompt"`) {
		t.Errorf("extension does not dispatch both hook subcommands:\n%s", ts)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".pi", "skills", "xfa", "SKILL.md"))
	if err != nil {
		t.Fatal("skill not installed to native pi location")
	}
	if string(raw) != skill.Content {
		t.Error("installed skill content differs from skill.Content")
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi", "skills", "xfa", ".xfa_version")); err != nil {
		t.Error("skill version stamp missing")
	}
}

// Extensions auto-load from .pi/extensions/ — there is no config file to
// register them in — so install creates exactly its four files and nothing
// else (especially no settings.json anywhere).
func TestInstallPiTouchesNoConfig(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		filepath.Join(".pi", "extensions", "xfa.ts"):          true,
		filepath.Join(".pi", "skills", "xfa", "SKILL.md"):     true,
		filepath.Join(".pi", "skills", "xfa", ".xfa_version"): true,
		"AGENTS.md": true,
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if !want[rel] {
			t.Errorf("install created unexpected file %s", rel)
		}
		delete(want, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for missing := range want {
		t.Errorf("install did not create %s", missing)
	}
}

// pi loads AGENTS.md-family context files regardless of project trust, so the
// awareness block goes there (the same file opencode seeds) and uninstall
// removes it.
func TestInstallPiSeedsAgentsMd(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	for _, must := range []string{awarenessBeginMarker, "Every agent uses xfa", "do exactly the same for every agent IT spawns"} {
		if !strings.Contains(string(got), must) {
			t.Errorf("AGENTS.md missing %q:\n%s", must, got)
		}
	}
	// Re-init is idempotent: the block is replaced in place, not duplicated.
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Count(string(again), awarenessBeginMarker) != 1 {
		t.Errorf("re-init duplicated the awareness block:\n%s", again)
	}
	// A user's existing AGENTS.md content survives, and uninstall strips only
	// our block.
	dir2 := t.TempDir()
	orig := "# Agents\n\nProject rules the user wrote.\n"
	if err := os.WriteFile(filepath.Join(dir2, "AGENTS.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallPi(dir2, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallPi(dir2); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir2, "AGENTS.md"))
	if err != nil {
		t.Fatalf("user AGENTS.md wrongly removed: %v", err)
	}
	if strings.Contains(string(after), awarenessBeginMarker) {
		t.Errorf("uninstall left the block behind:\n%s", after)
	}
	if !strings.Contains(string(after), "Project rules the user wrote.") {
		t.Errorf("uninstall lost user content:\n%s", after)
	}
}

func TestInstallPiIdempotentAndUpgrades(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/a/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := InstallPi(dir, "/b/xfa"); err != nil { // upgrade path
		t.Fatal(err)
	}
	ts, _ := os.ReadFile(filepath.Join(dir, ".pi", "extensions", "xfa.ts"))
	if strings.Count(string(ts), "before_agent_start") != 1 {
		t.Errorf("duplicated event registration:\n%s", ts)
	}
	if !strings.Contains(string(ts), "/b/xfa") || strings.Contains(string(ts), "/a/xfa") {
		t.Errorf("old exe path survived upgrade:\n%s", ts)
	}
}

// The extension's injection shape: session id from ctx.sessionManager, a
// once-per-process-per-session guard picking session-start vs user-prompt, the
// hook payload piped to stdin, a 3000ms bound on the xfa call, injection via a
// returned { message } with customType "xfa" and display: false, and fail-open
// try/catch around the whole handler.
func TestInstallPiExtensionShape(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	ts, _ := os.ReadFile(filepath.Join(dir, ".pi", "extensions", "xfa.ts"))
	for _, must := range []string{
		"ctx.sessionManager.getSessionId()",
		"seen.has(sid)",
		"seen.add(sid)",
		`JSON.stringify({ session_id: sid, cwd: ctx.cwd })`,
		"3000",
		`customType: "xfa"`,
		"display: false",
		"} catch {}",
	} {
		if !strings.Contains(string(ts), must) {
			t.Errorf("extension missing %q:\n%s", must, ts)
		}
	}
	// The run() helper must never reject — a hung or missing xfa resolves
	// empty instead of surfacing an error into the agent loop.
	if !strings.Contains(string(ts), `resolve("")`) {
		t.Errorf("run() helper does not resolve empty on error:\n%s", ts)
	}
}

// A byte-identical re-install must not rewrite files (no mtime churn).
func TestInstallPiSkipsIdenticalRewrite(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	ep := filepath.Join(dir, ".pi", "extensions", "xfa.ts")
	before, err := os.Stat(ep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ep, before.ModTime().Add(-time.Hour), before.ModTime().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(ep)
	if !after.ModTime().Equal(before.ModTime().Add(-time.Hour)) {
		t.Error("byte-identical extension was rewritten")
	}
}

// Paths with spaces survive: %EXE% is quoted into a JS string literal, and
// execFile passes it as the file argument without shell parsing.
func TestInstallPiQuotedSpacePath(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/Users/me/My Tools/xfa"); err != nil {
		t.Fatal(err)
	}
	ts, _ := os.ReadFile(filepath.Join(dir, ".pi", "extensions", "xfa.ts"))
	if !strings.Contains(string(ts), `"/Users/me/My Tools/xfa"`) {
		t.Errorf("space path not quoted:\n%s", ts)
	}
}

// Mirror of claude.go/opencode.go: a newer on-disk skill is never downgraded,
// but the extension still installs.
func TestInstallPiSkipsNewerSkill(t *testing.T) {
	t.Cleanup(stubVersion("1.0.0"))
	dir := t.TempDir()
	sdir := filepath.Join(dir, ".pi", "skills", "xfa")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, ".xfa_version"), []byte("9.9.9"), 0o644)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("newer content"), 0o644)
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(sdir, "SKILL.md"))
	if string(raw) != "newer content" {
		t.Errorf("newer on-disk skill was overwritten:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi", "extensions", "xfa.ts")); err != nil {
		t.Error("extension not installed when skill copy was skipped")
	}
}

func TestUninstallPiCleans(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallPi(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi", "extensions", "xfa.ts")); !os.IsNotExist(err) {
		t.Error("extension survived uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi", "skills", "xfa")); !os.IsNotExist(err) {
		t.Error("skill dir survived uninstall")
	}
	// Everything init created is gone, including now-empty parents.
	if _, err := os.Stat(filepath.Join(dir, ".pi")); !os.IsNotExist(err) {
		t.Error("empty .pi dir survived uninstall")
	}
}

// Foreign extensions, foreign skills, and a non-empty .pi must survive.
func TestUninstallPiPreservesForeignFiles(t *testing.T) {
	dir := t.TempDir()
	edir := filepath.Join(dir, ".pi", "extensions")
	os.MkdirAll(edir, 0o755)
	os.WriteFile(filepath.Join(edir, "other.ts"), []byte("export default function () {}\n"), 0o644)
	fsk := filepath.Join(dir, ".pi", "skills", "other", "SKILL.md")
	os.MkdirAll(filepath.Dir(fsk), 0o755)
	os.WriteFile(fsk, []byte("---\nname: other\n---\n"), 0o644)
	if err := InstallPi(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallPi(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(edir, "other.ts")); err != nil {
		t.Error("foreign extension removed by uninstall")
	}
	if _, err := os.Stat(fsk); err != nil {
		t.Error("foreign skill removed by uninstall")
	}
	if _, err := os.Stat(filepath.Join(edir, "xfa.ts")); !os.IsNotExist(err) {
		t.Error("xfa extension survived uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi", "skills", "xfa")); !os.IsNotExist(err) {
		t.Error("xfa skill survived uninstall")
	}
}

func TestUninstallPiDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	if err := UninstallPi(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi")); !os.IsNotExist(err) {
		t.Error("uninstall created .pi")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("uninstall created AGENTS.md")
	}
}
