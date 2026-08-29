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

// codexHooks decodes .codex/hooks.json's hooks map for a project dir.
func codexHooks(t *testing.T, dir string) map[string][]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var m struct {
		Hooks map[string][]any `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v\n%s", err, raw)
	}
	return m.Hooks
}

// Install writes the skill to codex's native .agents/skills location and adds
// exactly one xfa entry per event, each with an explicit numeric timeout (the
// codex default of 600s would let a stalled hook hang a session) and NO
// matcher (omitting it matches every occurrence).
func TestCodexInstallWritesSkillAndHooks(t *testing.T) {
	dir := t.TempDir()
	if err := InstallCodex(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"SKILL.md", ".xfa_version"} {
		if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "xfa", p)); err != nil {
			t.Errorf("skill file %s not installed: %v", p, err)
		}
	}

	hooks := codexHooks(t, dir)
	want := map[string]string{
		"SessionStart":     "session-start",
		"Stop":             "stop",
		"SubagentStop":     "subagent-stop",
		"UserPromptSubmit": "user-prompt",
	}
	if len(hooks) != len(want) {
		t.Fatalf("hooks map = %v, want exactly %d events", hooks, len(want))
	}
	for event, sub := range want {
		list, ok := hooks[event]
		if !ok || len(list) != 1 {
			t.Fatalf("hooks[%q] = %v, want exactly one entry", event, list)
		}
		entry, ok := list[0].(map[string]any)
		if !ok {
			t.Fatalf("hooks[%q][0] is not an object: %#v", event, list[0])
		}
		if _, present := entry["matcher"]; present {
			t.Errorf("hooks[%q] entry must not carry a matcher: %#v", event, entry)
		}
		inner, ok := entry["hooks"].([]any)
		if !ok || len(inner) != 1 {
			t.Fatalf("hooks[%q] entry has no single inner hook: %#v", event, entry)
		}
		h, ok := inner[0].(map[string]any)
		if !ok {
			t.Fatalf("hooks[%q] inner hook is not an object: %#v", event, inner[0])
		}
		if h["type"] != "command" {
			t.Errorf("hooks[%q] inner type = %v, want \"command\"", event, h["type"])
		}
		if got, wantCmd := h["command"], `"/usr/local/bin/xfa" hook `+sub; got != wantCmd {
			t.Errorf("hooks[%q] command = %v, want %q", event, got, wantCmd)
		}
		// json.Unmarshal decodes numbers into float64; a string "30" would not.
		if got, ok := h["timeout"].(float64); !ok || got != 30 {
			t.Errorf("hooks[%q] timeout = %#v, want JSON number 30", event, h["timeout"])
		}
	}
}

// Install merges into an existing hooks.json: foreign hook entries and foreign
// top-level keys survive, and re-install replaces rather than duplicates.
func TestCodexInstallPreservesForeignConfigAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "description": "my codex hooks",
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "echo hello"}]}
    ]
  }
}
`
	if err := os.WriteFile(hp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(dir, "/a/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(hp)
	if !strings.Contains(string(raw), "echo hello") {
		t.Errorf("foreign hook entry must survive install:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"description"`) {
		t.Errorf("foreign top-level key must survive install:\n%s", raw)
	}
	if n := len(codexHooks(t, dir)["SessionStart"]); n != 2 {
		t.Fatalf("SessionStart entries = %d, want 2 (foreign + xfa)", n)
	}

	// Re-install with a different exe path: replace-then-append, so exactly one
	// xfa entry remains and it points at the new binary.
	if err := InstallCodex(dir, "/b/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(hp)
	for _, sub := range []string{"session-start", "stop", "subagent-stop", "user-prompt"} {
		if c := strings.Count(string(raw), "hook "+sub); c != 1 {
			t.Errorf("hook %s appears %d times, want 1:\n%s", sub, c, raw)
		}
	}
	if strings.Contains(string(raw), "/a/xfa") || !strings.Contains(string(raw), "/b/xfa") {
		t.Errorf("re-install must swap the exe path:\n%s", raw)
	}
	if !strings.Contains(string(raw), "echo hello") {
		t.Errorf("foreign hook entry must survive re-install:\n%s", raw)
	}

	// Third install, byte-identical: no rewrite at all (no mtime churn).
	before, err := os.Stat(hp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hp, before.ModTime().Add(-time.Hour), before.ModTime().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(dir, "/b/xfa"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(hp)
	if !after.ModTime().Equal(before.ModTime().Add(-time.Hour)) {
		t.Error("byte-identical re-install must not rewrite hooks.json")
	}
}

// A hooks.json that is not a JSON object is never clobbered: install errors,
// the file keeps its bytes, and no backup is written.
func TestCodexInstallRefusesNonObjectHooksFile(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatal(err)
	}
	const junk = "[1, 2, 3]\n"
	if err := os.WriteFile(hp, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallCodex(dir, "/usr/local/bin/xfa")
	if !errors.Is(err, ErrNotObject) {
		t.Fatalf("InstallCodex on a non-object hooks.json = %v, want ErrNotObject", err)
	}
	raw, _ := os.ReadFile(hp)
	if string(raw) != junk {
		t.Errorf("hooks.json was modified: %q", raw)
	}
	if _, err := os.Stat(hp + ".xfa-bak"); !os.IsNotExist(err) {
		t.Errorf("no backup must be written when the file is refused, stat err = %v", err)
	}
}

// AGENTS.md is codex's always-on rules file, so the awareness block lands there
// (the same file opencode and pi seed).
func TestCodexInstallSeedsAwarenessBlock(t *testing.T) {
	dir := t.TempDir()
	if err := InstallCodex(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not written: %v", err)
	}
	if !strings.Contains(string(raw), "xfa") {
		t.Errorf("awareness block missing from AGENTS.md:\n%s", raw)
	}
	// Re-install must not duplicate the block.
	if err := InstallCodex(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if string(again) != string(raw) {
		t.Errorf("re-install changed AGENTS.md:\n%s", again)
	}
}

// Uninstall strips only xfa's entries, removes only xfa's skill dir, prunes
// parents only when they are empty, and clears the awareness block.
func TestCodexUninstallRemovesOnlyXfaArtifacts(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hp, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo bye"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallCodex(dir); err != nil {
		t.Fatalf("UninstallCodex: %v", err)
	}

	raw, err := os.ReadFile(hp)
	if err != nil {
		t.Fatalf("hooks.json must survive uninstall: %v", err)
	}
	if !strings.Contains(string(raw), "echo bye") {
		t.Errorf("foreign hook entry must survive uninstall:\n%s", raw)
	}
	if strings.Contains(string(raw), "hook session-start") || strings.Contains(string(raw), "hook stop") ||
		strings.Contains(string(raw), "hook subagent-stop") || strings.Contains(string(raw), "hook user-prompt") {
		t.Errorf("xfa hook entries survived uninstall:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents")); !os.IsNotExist(err) {
		t.Errorf("empty .agents must be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("an AGENTS.md that was ours alone must go, stat err = %v", err)
	}
}

// A .agents dir holding someone else's skill survives the prune.
func TestCodexUninstallKeepsNonEmptyAgentsDir(t *testing.T) {
	dir := t.TempDir()
	if err := InstallCodex(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, ".agents", "skills", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "SKILL.md"), []byte("# other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UninstallCodex(dir); err != nil {
		t.Fatalf("UninstallCodex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "xfa")); !os.IsNotExist(err) {
		t.Errorf("xfa skill dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "SKILL.md")); err != nil {
		t.Errorf("a foreign skill must survive uninstall: %v", err)
	}
}

// Uninstalling a project that was never installed creates nothing — in
// particular it never conjures a .codex/hooks.json.
func TestCodexUninstallOnCleanProjectCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := UninstallCodex(dir); err != nil {
		t.Fatalf("UninstallCodex on a clean project: %v", err)
	}
	for _, p := range []string{".codex", ".codex/hooks.json", ".agents", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Errorf("%s must not be created by uninstall, stat err = %v", p, err)
		}
	}
}
