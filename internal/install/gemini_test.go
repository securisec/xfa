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

// geminiHooks decodes .gemini/settings.json's hooks map for a project dir.
func geminiHooks(t *testing.T, dir string) map[string][]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".gemini", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var m struct {
		Hooks map[string][]any `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, raw)
	}
	return m.Hooks
}

// Install writes the skill to gemini's native .gemini/skills location and adds
// exactly one xfa entry per event — only SessionStart and BeforeAgent (gemini
// has no Stop/SubagentStop injection point) — each with an explicit numeric
// timeout in MILLISECONDS (gemini's default is 60000) and NO matcher.
func TestGeminiInstallWritesSkillAndHooks(t *testing.T) {
	dir := t.TempDir()
	if err := InstallGemini(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"SKILL.md", ".xfa_version"} {
		if _, err := os.Stat(filepath.Join(dir, ".gemini", "skills", "xfa", p)); err != nil {
			t.Errorf("skill file %s not installed: %v", p, err)
		}
	}

	hooks := geminiHooks(t, dir)
	want := map[string]string{
		"SessionStart": "session-start",
		"BeforeAgent":  "user-prompt",
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
		// gemini timeouts are milliseconds; json decodes numbers into float64.
		if got, ok := h["timeout"].(float64); !ok || got != 30000 {
			t.Errorf("hooks[%q] timeout = %#v, want JSON number 30000", event, h["timeout"])
		}
	}
}

// Install merges into an existing settings.json: foreign hook entries and
// foreign top-level settings keys survive, and re-install replaces rather than
// duplicates.
func TestGeminiInstallPreservesForeignConfigAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "theme": "dark",
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "echo hello"}]}
    ]
  }
}
`
	if err := os.WriteFile(sp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallGemini(dir, "/a/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sp)
	if !strings.Contains(string(raw), "echo hello") {
		t.Errorf("foreign hook entry must survive install:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"theme"`) {
		t.Errorf("foreign settings key must survive install:\n%s", raw)
	}
	if n := len(geminiHooks(t, dir)["SessionStart"]); n != 2 {
		t.Fatalf("SessionStart entries = %d, want 2 (foreign + xfa)", n)
	}

	// Re-install with a different exe path: replace-then-append, so exactly one
	// xfa entry remains and it points at the new binary.
	if err := InstallGemini(dir, "/b/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(sp)
	for _, sub := range []string{"session-start", "user-prompt"} {
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
	before, err := os.Stat(sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sp, before.ModTime().Add(-time.Hour), before.ModTime().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := InstallGemini(dir, "/b/xfa"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(sp)
	if !after.ModTime().Equal(before.ModTime().Add(-time.Hour)) {
		t.Error("byte-identical re-install must not rewrite settings.json")
	}
}

// A settings.json that is not a JSON object is never clobbered: install
// errors, the file keeps its bytes, and no backup is written.
func TestGeminiInstallRefusesNonObjectSettingsFile(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		t.Fatal(err)
	}
	const junk = "[1, 2, 3]\n"
	if err := os.WriteFile(sp, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallGemini(dir, "/usr/local/bin/xfa")
	if !errors.Is(err, ErrNotObject) {
		t.Fatalf("InstallGemini on a non-object settings.json = %v, want ErrNotObject", err)
	}
	raw, _ := os.ReadFile(sp)
	if string(raw) != junk {
		t.Errorf("settings.json was modified: %q", raw)
	}
	if _, err := os.Stat(sp + ".xfa-bak"); !os.IsNotExist(err) {
		t.Errorf("no backup must be written when the file is refused, stat err = %v", err)
	}
}

// GEMINI.md is gemini's always-on context file (it does not read AGENTS.md by
// default), so the awareness block lands there — shared with no other provider.
func TestGeminiInstallSeedsAwarenessBlock(t *testing.T) {
	dir := t.TempDir()
	if err := InstallGemini(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "GEMINI.md"))
	if err != nil {
		t.Fatalf("GEMINI.md not written: %v", err)
	}
	if !strings.Contains(string(raw), "xfa") {
		t.Errorf("awareness block missing from GEMINI.md:\n%s", raw)
	}
	// Re-install must not duplicate the block.
	if err := InstallGemini(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "GEMINI.md"))
	if string(again) != string(raw) {
		t.Errorf("re-install changed GEMINI.md:\n%s", again)
	}
}

// Uninstall strips only xfa's entries, removes only xfa's skill dir, prunes
// parents only when they are empty, and clears the awareness block. A
// settings.json holding foreign keys keeps .gemini alive.
func TestGeminiUninstallRemovesOnlyXfaArtifacts(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sp, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo bye"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallGemini(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallGemini(dir); err != nil {
		t.Fatalf("UninstallGemini: %v", err)
	}

	raw, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("settings.json must survive uninstall: %v", err)
	}
	if !strings.Contains(string(raw), "echo bye") {
		t.Errorf("foreign hook entry must survive uninstall:\n%s", raw)
	}
	if strings.Contains(string(raw), "hook session-start") || strings.Contains(string(raw), "hook user-prompt") {
		t.Errorf("xfa hook entries survived uninstall:\n%s", raw)
	}
	// settings.json still exists, so .gemini must survive the prune (os.Remove
	// fails silently on non-empty dirs) — but the empty skills dir goes.
	if _, err := os.Stat(filepath.Join(dir, ".gemini", "skills")); !os.IsNotExist(err) {
		t.Errorf("empty .gemini/skills must be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gemini")); err != nil {
		t.Errorf(".gemini holding a settings.json must survive uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "GEMINI.md")); !os.IsNotExist(err) {
		t.Errorf("a GEMINI.md that was ours alone must go, stat err = %v", err)
	}
}

// A .gemini/skills dir holding someone else's skill survives the prune.
func TestGeminiUninstallKeepsForeignSkills(t *testing.T) {
	dir := t.TempDir()
	if err := InstallGemini(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, ".gemini", "skills", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "SKILL.md"), []byte("# other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UninstallGemini(dir); err != nil {
		t.Fatalf("UninstallGemini: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gemini", "skills", "xfa")); !os.IsNotExist(err) {
		t.Errorf("xfa skill dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "SKILL.md")); err != nil {
		t.Errorf("a foreign skill must survive uninstall: %v", err)
	}
}

// Uninstalling a project that was never installed creates nothing — in
// particular it never conjures a .gemini/settings.json.
func TestGeminiUninstallOnCleanProjectCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := UninstallGemini(dir); err != nil {
		t.Fatalf("UninstallGemini on a clean project: %v", err)
	}
	for _, p := range []string{".gemini", ".gemini/settings.json", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Errorf("%s must not be created by uninstall, stat err = %v", p, err)
		}
	}
}
