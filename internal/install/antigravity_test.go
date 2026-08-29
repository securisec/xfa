package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// antigravityHooksMap decodes .agents/hooks.json (top-level keyed by hook
// name, not a "hooks" key) for a project dir.
func antigravityHooksMap(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".agents", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v\n%s", err, raw)
	}
	return m
}

// Install writes the skill to the codex-shared .agents/skills location, owns
// exactly one top-level "xfa" hooks key (enabled + PreInvocation + Stop, each
// a FLAT handler object — lifecycle events take no matcher/hooks wrapper —
// with a 10-SECOND timeout), and writes the always-on rule file.
func TestAntigravityInstallWritesSkillHooksAndRule(t *testing.T) {
	dir := t.TempDir()
	if err := InstallAntigravity(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"SKILL.md", ".xfa_version"} {
		if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "xfa", p)); err != nil {
			t.Errorf("skill file %s not installed: %v", p, err)
		}
	}

	m := antigravityHooksMap(t, dir)
	if len(m) != 1 {
		t.Fatalf("hooks.json top level = %v, want exactly the \"xfa\" key", m)
	}
	xfa, ok := m["xfa"].(map[string]any)
	if !ok {
		t.Fatalf("\"xfa\" key is not an object: %#v", m["xfa"])
	}
	if xfa["enabled"] != true {
		t.Errorf("xfa.enabled = %#v, want true", xfa["enabled"])
	}
	want := map[string]string{
		"PreInvocation": "antigravity-invoke",
		"Stop":          "antigravity-stop",
	}
	if len(xfa) != len(want)+1 { // events + "enabled"
		t.Fatalf("xfa key = %v, want enabled + exactly %d events", xfa, len(want))
	}
	for event, sub := range want {
		list, ok := xfa[event].([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("xfa[%q] = %#v, want exactly one handler", event, xfa[event])
		}
		h, ok := list[0].(map[string]any)
		if !ok {
			t.Fatalf("xfa[%q][0] is not an object: %#v", event, list[0])
		}
		// Lifecycle events are FLAT: the handler sits directly in the event
		// array, never inside a {"matcher","hooks"} wrapper (that grouped shape
		// is tool-events-only, and a wrapped handler silently registers nothing).
		for _, k := range []string{"matcher", "hooks"} {
			if _, present := h[k]; present {
				t.Errorf("xfa[%q] handler must be flat, found %q key: %#v", event, k, h)
			}
		}
		if h["type"] != "command" {
			t.Errorf("xfa[%q] inner type = %v, want \"command\"", event, h["type"])
		}
		if got, wantCmd := h["command"], `"/usr/local/bin/xfa" hook `+sub; got != wantCmd {
			t.Errorf("xfa[%q] command = %v, want %q", event, got, wantCmd)
		}
		// antigravity timeouts are seconds; json decodes numbers into float64.
		if got, ok := h["timeout"].(float64); !ok || got != 10 {
			t.Errorf("xfa[%q] timeout = %#v, want JSON number 10", event, h["timeout"])
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".agents", "rules", "xfa.md"))
	if err != nil {
		t.Fatalf("rule file not written: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\ntrigger: always_on\n---\n") {
		t.Errorf("rule file must start with the always_on frontmatter:\n%s", raw)
	}
	if !strings.Contains(string(raw), "xfa") || !strings.Contains(string(raw), "message board") {
		t.Errorf("rule file must carry the awareness content:\n%s", raw)
	}
}

// Install merges into an existing hooks.json: foreign top-level hook-name keys
// survive, re-install replaces the xfa key, and a byte-identical re-install
// (rule file included) rewrites nothing.
func TestAntigravityInstallPreservesForeignKeysAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "linter": {"enabled": true, "PostToolUse": [{"hooks": [{"type": "command", "command": "echo hello"}]}]}
}
`
	if err := os.WriteFile(hp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallAntigravity(dir, "/a/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(hp)
	if !strings.Contains(string(raw), "echo hello") {
		t.Errorf("foreign hook-name key must survive install:\n%s", raw)
	}

	// Re-install with a different exe path: the xfa key is replaced wholesale.
	if err := InstallAntigravity(dir, "/b/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(hp)
	for _, sub := range []string{"antigravity-invoke", "antigravity-stop"} {
		if c := strings.Count(string(raw), "hook "+sub); c != 1 {
			t.Errorf("hook %s appears %d times, want 1:\n%s", sub, c, raw)
		}
	}
	if strings.Contains(string(raw), "/a/xfa") || !strings.Contains(string(raw), "/b/xfa") {
		t.Errorf("re-install must swap the exe path:\n%s", raw)
	}
	if !strings.Contains(string(raw), "echo hello") {
		t.Errorf("foreign key must survive re-install:\n%s", raw)
	}

	// Third install, byte-identical: no rewrite at all (no mtime churn), for
	// the hooks file and the rule file both.
	rp := filepath.Join(dir, ".agents", "rules", "xfa.md")
	old := time.Now().Add(-2 * time.Hour)
	for _, p := range []string{hp, rp} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := InstallAntigravity(dir, "/b/xfa"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{hp, rp} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !fi.ModTime().Equal(old) {
			t.Errorf("byte-identical re-install must not rewrite %s", p)
		}
	}
}

// A hooks.json that is not a JSON object is never clobbered: install errors,
// the file keeps its bytes, and no backup is written.
func TestAntigravityInstallRefusesNonObjectHooksFile(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatal(err)
	}
	const junk = "[1, 2, 3]\n"
	if err := os.WriteFile(hp, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallAntigravity(dir, "/usr/local/bin/xfa")
	if !errors.Is(err, ErrNotObject) {
		t.Fatalf("InstallAntigravity on a non-object hooks.json = %v, want ErrNotObject", err)
	}
	raw, _ := os.ReadFile(hp)
	if string(raw) != junk {
		t.Errorf("hooks.json was modified: %q", raw)
	}
	if _, err := os.Stat(hp + ".xfa-bak"); !os.IsNotExist(err) {
		t.Errorf("no backup must be written when the file is refused, stat err = %v", err)
	}
}

// Uninstall deletes exactly the "xfa" key, the skill dir, and the rule file;
// foreign hook-name keys and hooks.json itself survive (which keeps .agents
// alive), and empty rules/skills dirs are pruned.
func TestAntigravityUninstallRemovesOnlyXfaArtifacts(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hp, []byte(`{"linter":{"enabled":true,"Stop":[{"type":"command","command":"echo bye"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallAntigravity(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallAntigravity(dir); err != nil {
		t.Fatalf("UninstallAntigravity: %v", err)
	}

	m := antigravityHooksMap(t, dir)
	if _, ok := m["xfa"]; ok {
		t.Errorf("the xfa key survived uninstall: %v", m)
	}
	if _, ok := m["linter"]; !ok {
		t.Errorf("foreign hook-name key must survive uninstall: %v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "xfa")); !os.IsNotExist(err) {
		t.Errorf("xfa skill dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "rules", "xfa.md")); !os.IsNotExist(err) {
		t.Errorf("rule file must be removed, stat err = %v", err)
	}
	for _, d := range []string{".agents/rules", ".agents/skills"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(d))); !os.IsNotExist(err) {
			t.Errorf("empty %s must be pruned, stat err = %v", d, err)
		}
	}
	// hooks.json still exists, so .agents survives the prune.
	if _, err := os.Stat(filepath.Join(dir, ".agents")); err != nil {
		t.Errorf(".agents holding a hooks.json must survive uninstall: %v", err)
	}
}

// A hooks.json whose only key was ours is left as an empty object — the file
// is never deleted — and a foreign skill under .agents/skills keeps the dirs
// alive.
func TestAntigravityUninstallKeepsForeignSkills(t *testing.T) {
	dir := t.TempDir()
	if err := InstallAntigravity(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, ".agents", "skills", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "SKILL.md"), []byte("# other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UninstallAntigravity(dir); err != nil {
		t.Fatalf("UninstallAntigravity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "xfa")); !os.IsNotExist(err) {
		t.Errorf("xfa skill dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "SKILL.md")); err != nil {
		t.Errorf("a foreign skill must survive uninstall: %v", err)
	}
	if m := antigravityHooksMap(t, dir); len(m) != 0 {
		t.Errorf("hooks.json should be an empty object after uninstall: %v", m)
	}
}

// Uninstalling a project that was never installed creates nothing — in
// particular it never conjures a .agents/hooks.json.
func TestAntigravityUninstallOnCleanProjectCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := UninstallAntigravity(dir); err != nil {
		t.Fatalf("UninstallAntigravity on a clean project: %v", err)
	}
	for _, p := range []string{".agents", ".agents/hooks.json", ".agents/rules/xfa.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Errorf("%s must not be created by uninstall, stat err = %v", p, err)
		}
	}
}
