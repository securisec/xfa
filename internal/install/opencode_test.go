package install

// Tests adjusted from the task-11 brief after researching opencode's current
// mechanism (opencode.ai/docs/plugins, /docs/skills, /docs/config; cross-checked
// against the anomalyco/opencode source):
//   - Plugins auto-load from .opencode/plugins/*.js — the config "plugin" array
//     is for npm packages only, so xfa never writes opencode.json.
//   - Skills are native at .opencode/skills/<name>/SKILL.md, so the skill goes
//     there instead of a marker-fenced AGENTS.md section.
//   - Context is injected via the stable "chat.message" plugin hook (mutable
//     output.parts); session.created events cannot inject context.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/skill"
)

func TestInstallOpencodeWritesPluginAndSkill(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(dir, ".opencode", "plugins", "xfa.js"))
	if err != nil {
		t.Fatal("plugin file missing")
	}
	// Exe path is shell-quoted, matching the claude.go convention.
	if !strings.Contains(string(js), `"/usr/local/bin/xfa" hook session-start`) {
		t.Errorf("plugin does not invoke quoted xfa:\n%s", js)
	}
	if !strings.Contains(string(js), `"chat.message"`) {
		t.Errorf("plugin does not use the chat.message hook:\n%s", js)
	}
	// The mid-session nudge (claude's UserPromptSubmit equivalent) must run on
	// later messages of a session, not just the first.
	if !strings.Contains(string(js), `"/usr/local/bin/xfa" hook user-prompt`) {
		t.Errorf("plugin does not invoke the user-prompt hook:\n%s", js)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".opencode", "skills", "xfa", "SKILL.md"))
	if err != nil {
		t.Fatal("skill not installed to native opencode location")
	}
	if string(raw) != skill.Content {
		t.Error("installed skill content differs from skill.Content")
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode", "skills", "xfa", ".xfa_version")); err != nil {
		t.Error("skill version stamp missing")
	}
}

// Plugins auto-load from .opencode/plugins/; the "plugin" config key is for npm
// packages only, so install never creates or edits an opencode config file.
// (AGENTS.md is created deliberately — see TestInstallOpencodeSeedsAgentsMd.)
func TestInstallOpencodeTouchesNoConfig(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.jsonc"),
		filepath.Join(dir, ".opencode", "opencode.json"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("install created %s", p)
		}
	}
}

// opencode reads AGENTS.md as its always-on rules file (CLAUDE.md equivalent),
// so install seeds the same awareness block there and uninstall removes it.
func TestInstallOpencodeSeedsAgentsMd(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
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
	// A user's existing AGENTS.md content survives, and uninstall strips only
	// our block.
	dir2 := t.TempDir()
	orig := "# Agents\n\nProject rules the user wrote.\n"
	if err := os.WriteFile(filepath.Join(dir2, "AGENTS.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallOpencode(dir2, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallOpencode(dir2); err != nil {
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

// Pre-rename ("xaf") artifacts are the reason "opencode still looks for xaf":
// opencode auto-loads every plugin in .opencode/plugins, so a stale xaf.js
// keeps invoking a binary that no longer exists. Both install and uninstall
// must clear the legacy plugin and skill dir.
func TestOpencodeRemovesLegacyXafArtifacts(t *testing.T) {
	seedLegacy := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, ".opencode", "plugins"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".opencode", "plugins", "xaf.js"), []byte("// stale xaf plugin\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, ".opencode", "skills", "xaf"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".opencode", "skills", "xaf", "SKILL.md"), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	assertGone := func(dir string) {
		t.Helper()
		for _, p := range []string{
			filepath.Join(dir, ".opencode", "plugins", "xaf.js"),
			filepath.Join(dir, ".opencode", "skills", "xaf"),
		} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("legacy artifact survived: %s (err=%v)", p, err)
			}
		}
	}

	// Install migrates a project off the old name.
	dirInstall := t.TempDir()
	seedLegacy(dirInstall)
	if err := InstallOpencode(dirInstall, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	assertGone(dirInstall)
	// The current plugin is still installed (cleanup didn't nuke xfa.js).
	if _, err := os.Stat(filepath.Join(dirInstall, ".opencode", "plugins", "xfa.js")); err != nil {
		t.Errorf("current xfa.js missing after install: %v", err)
	}

	// Uninstall also clears legacy artifacts.
	dirUninstall := t.TempDir()
	seedLegacy(dirUninstall)
	if err := UninstallOpencode(dirUninstall); err != nil {
		t.Fatal(err)
	}
	assertGone(dirUninstall)
}

func TestInstallOpencodeIdempotentAndUpgrades(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/a/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := InstallOpencode(dir, "/b/xfa"); err != nil { // upgrade path
		t.Fatal(err)
	}
	js, _ := os.ReadFile(filepath.Join(dir, ".opencode", "plugins", "xfa.js"))
	if strings.Count(string(js), "hook session-start") != 1 {
		t.Errorf("duplicated hook invocation:\n%s", js)
	}
	if !strings.Contains(string(js), "/b/xfa") || strings.Contains(string(js), "/a/xfa") {
		t.Errorf("old exe path survived upgrade:\n%s", js)
	}
}

// Review Critical 1 + Important 2: the digest must be prepended to an existing
// text part (a pushed object literal would lack the id/sessionID/messageID the
// Part schema requires), and the xfa call must be bounded by a timeout so a
// hung xfa (e.g. locked DB) never stalls the user's first message.
func TestInstallOpencodePluginInjectionShape(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	js, _ := os.ReadFile(filepath.Join(dir, ".opencode", "plugins", "xfa.js"))
	if strings.Contains(string(js), "parts.push(") {
		t.Errorf("plugin pushes a bare part object (schema-invalid):\n%s", js)
	}
	if !strings.Contains(string(js), `first.text = out + "\n\n" + first.text`) {
		t.Errorf("plugin does not prepend the digest to an existing text part:\n%s", js)
	}
	if !strings.Contains(string(js), "Promise.race") {
		t.Errorf("plugin has no timeout guard around the xfa call:\n%s", js)
	}
	// The mid-session user-prompt branch obeys the same shape: it only fires
	// once the session has been seen, prepends to an existing text part, passes
	// the session id on stdin, and is bounded/fail-open like the digest call.
	if !strings.Contains(string(js), "seen.has(sid)") || !strings.Contains(string(js), "seen.add(sid)") {
		t.Errorf("plugin lost the once-per-process session guard:\n%s", js)
	}
	if !strings.Contains(string(js), `later.text = out + "\n\n" + later.text`) {
		t.Errorf("mid-session nudge does not prepend to an existing text part:\n%s", js)
	}
	if !strings.Contains(string(js), `JSON.stringify({ session_id: sid, cwd: process.cwd() })`) {
		t.Errorf("mid-session nudge does not pass the session id on stdin:\n%s", js)
	}
	if !strings.Contains(string(js), "echo ${payload} | ") {
		t.Errorf("mid-session nudge does not pipe the hook payload to stdin:\n%s", js)
	}
	if strings.Count(string(js), "Promise.race") != 2 || strings.Count(string(js), ".quiet().nothrow().text()") != 2 {
		t.Errorf("both hook invocations must be timeout-bounded and fail-open:\n%s", js)
	}
	if strings.Count(string(js), "} catch {}") != 2 {
		t.Errorf("both hook invocations must be wrapped in a fail-open try/catch:\n%s", js)
	}
}

// A byte-identical re-install must not rewrite files (no mtime churn).
func TestInstallOpencodeSkipsIdenticalRewrite(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	pp := filepath.Join(dir, ".opencode", "plugins", "xfa.js")
	before, err := os.Stat(pp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pp, before.ModTime().Add(-time.Hour), before.ModTime().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(pp)
	if !after.ModTime().Equal(before.ModTime().Add(-time.Hour)) {
		t.Error("byte-identical plugin was rewritten")
	}
}

// Mirror of claude.go finding 3: paths with spaces are quoted in the generated
// Bun shell command.
func TestInstallOpencodeQuotedSpacePath(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/Users/me/My Tools/xfa"); err != nil {
		t.Fatal(err)
	}
	js, _ := os.ReadFile(filepath.Join(dir, ".opencode", "plugins", "xfa.js"))
	if !strings.Contains(string(js), `"/Users/me/My Tools/xfa" hook session-start`) {
		t.Errorf("space path not quoted:\n%s", js)
	}
}

// Mirror of claude.go: a newer on-disk skill is never downgraded, but the
// plugin still installs.
func TestInstallOpencodeSkipsNewerSkill(t *testing.T) {
	t.Cleanup(stubVersion("1.0.0"))
	dir := t.TempDir()
	sdir := filepath.Join(dir, ".opencode", "skills", "xfa")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, ".xfa_version"), []byte("9.9.9"), 0o644)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("newer content"), 0o644)
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(sdir, "SKILL.md"))
	if string(raw) != "newer content" {
		t.Errorf("newer on-disk skill was overwritten:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode", "plugins", "xfa.js")); err != nil {
		t.Error("plugin not installed when skill copy was skipped")
	}
}

func TestUninstallOpencodeCleans(t *testing.T) {
	dir := t.TempDir()
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallOpencode(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode", "plugins", "xfa.js")); !os.IsNotExist(err) {
		t.Error("plugin survived uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode", "skills", "xfa")); !os.IsNotExist(err) {
		t.Error("skill dir survived uninstall")
	}
	// Everything init created is gone, including now-empty parents.
	if _, err := os.Stat(filepath.Join(dir, ".opencode")); !os.IsNotExist(err) {
		t.Error("empty .opencode dir survived uninstall")
	}
}

// Foreign plugins, user config, and a non-empty .opencode must survive.
func TestUninstallOpencodePreservesForeignFiles(t *testing.T) {
	dir := t.TempDir()
	pdir := filepath.Join(dir, ".opencode", "plugins")
	os.MkdirAll(pdir, 0o755)
	os.WriteFile(filepath.Join(pdir, "other.js"), []byte("export const O = async () => ({})"), 0o644)
	userCfg := []byte(`{"$schema":"https://opencode.ai/config.json","theme":"dark"}`)
	os.WriteFile(filepath.Join(dir, ".opencode", "opencode.json"), userCfg, 0o644)
	if err := InstallOpencode(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallOpencode(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pdir, "other.js")); err != nil {
		t.Error("foreign plugin removed by uninstall")
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".opencode", "opencode.json"))
	if err != nil || string(raw) != string(userCfg) {
		t.Errorf("user opencode.json not preserved byte-identically:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join(pdir, "xfa.js")); !os.IsNotExist(err) {
		t.Error("xfa plugin survived uninstall")
	}
}

func TestUninstallOpencodeDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	if err := UninstallOpencode(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode")); !os.IsNotExist(err) {
		t.Error("uninstall created .opencode")
	}
}
