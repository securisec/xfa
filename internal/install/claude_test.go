package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/skill"
)

func TestInstallClaudeWritesSkillAndHooks(t *testing.T) {
	dir := t.TempDir()
	if err := InstallClaude(dir, "/usr/local/bin/xfa"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "xfa", "SKILL.md")); err != nil {
		t.Error("skill not installed")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	// Exe path is shell-quoted (finding 3): "<exe>" hook <sub>.
	if !strings.Contains(string(raw), `\"/usr/local/bin/xfa\" hook session-start`) ||
		!strings.Contains(string(raw), `\"/usr/local/bin/xfa\" hook stop`) ||
		!strings.Contains(string(raw), `\"/usr/local/bin/xfa\" hook subagent-stop`) ||
		!strings.Contains(string(raw), `\"/usr/local/bin/xfa\" hook user-prompt`) {
		t.Errorf("quoted hooks missing:\n%s", raw)
	}
	// The subagent-stop / user-prompt entries live under their own events.
	var events struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &events); err != nil || events.Hooks["SubagentStop"] == nil {
		t.Errorf("SubagentStop event missing from hooks: err=%v\n%s", err, raw)
	}
	if events.Hooks["UserPromptSubmit"] == nil {
		t.Errorf("UserPromptSubmit event missing from hooks:\n%s", raw)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		t.Error("settings.json is not valid JSON")
	}
}

func TestInstallClaudeIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := InstallClaude(dir, "/a/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/b/xfa"); err != nil { // upgrade path
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if strings.Count(string(raw), "hook session-start") != 1 {
		t.Errorf("duplicated hook entries:\n%s", raw)
	}
	if strings.Count(string(raw), "hook subagent-stop") != 1 {
		t.Errorf("duplicated subagent-stop hook entries:\n%s", raw)
	}
	if strings.Count(string(raw), "hook user-prompt") != 1 {
		t.Errorf("duplicated user-prompt hook entries:\n%s", raw)
	}
	if !strings.Contains(string(raw), "/b/xfa") || strings.Contains(string(raw), "/a/xfa") {
		t.Errorf("old exe path survived upgrade:\n%s", raw)
	}
}

// Task 11 review finding 4: skill writes go through writeFileIfChanged, so a
// byte-identical re-init must not rewrite SKILL.md (no mtime churn).
func TestInstallClaudeSkipsIdenticalSkillRewrite(t *testing.T) {
	dir := t.TempDir()
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(dir, ".claude", "skills", "xfa", "SKILL.md")
	before, err := os.Stat(sp)
	if err != nil {
		t.Fatal(err)
	}
	old := before.ModTime().Add(-time.Hour)
	if err := os.Chtimes(sp, old, old); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(sp)
	if !after.ModTime().Equal(old) {
		t.Error("byte-identical SKILL.md was rewritten on re-init")
	}
}

func TestInstallClaudePreservesUserSettings(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Bash(ls:*)"]}}`), 0o644)
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if !strings.Contains(string(raw), "Bash(ls:*)") {
		t.Errorf("user settings clobbered:\n%s", raw)
	}
}

// Ruling 1: UpsertHookEntry errors on malformed shapes and InstallClaude must
// propagate that (errors.Is-able ErrMalformedConfig) without clobbering the file.
func TestInstallClaudeRefusesMalformedHooks(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	orig := []byte(`{"hooks":["not-an-object"]}`)
	os.WriteFile(filepath.Join(cfgDir, "settings.json"), orig, 0o644)
	err := InstallClaude(dir, "/x/xfa")
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("want ErrMalformedConfig, got %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if string(raw) != string(orig) {
		t.Errorf("malformed settings were modified:\n%s", raw)
	}
}

// stubVersion pins skill.Version (a "dev" build otherwise never defers to an
// on-disk copy) and returns the restore func.
func stubVersion(v string) func() {
	old := skill.Version
	skill.Version = v
	return func() { skill.Version = old }
}

func TestInstallClaudeSkipsNewerSkill(t *testing.T) {
	t.Cleanup(stubVersion("1.0.0"))
	dir := t.TempDir()
	sdir := filepath.Join(dir, ".claude", "skills", "xfa")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, ".xfa_version"), []byte("9.9.9"), 0o644)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("newer content"), 0o644)
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(sdir, "SKILL.md"))
	if string(raw) != "newer content" {
		t.Errorf("newer on-disk skill was overwritten:\n%s", raw)
	}
	ver, _ := os.ReadFile(filepath.Join(sdir, ".xfa_version"))
	if string(ver) != "9.9.9" {
		t.Errorf("newer version stamp was overwritten: %s", ver)
	}
}

func TestUninstallClaudeCleans(t *testing.T) {
	dir := t.TempDir()
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if strings.Contains(string(raw), "xfa") {
		t.Errorf("hook entries survived uninstall:\n%s", raw)
	}
	for _, sub := range []string{"hook session-start", "hook stop", "hook subagent-stop", "hook user-prompt"} {
		if strings.Contains(string(raw), sub) {
			t.Errorf("%q survived uninstall:\n%s", sub, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "xfa")); !os.IsNotExist(err) {
		t.Error("skill dir survived uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); !os.IsNotExist(err) {
		t.Error("empty skills dir survived uninstall")
	}
}

// Ruling 2: uninstall uses RemoveHookEntries — foreign hook entries must survive.
func TestUninstallClaudePreservesForeignHooks(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "settings.json"),
		[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other-tool notify"}]}]}}`), 0o644)
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if !strings.Contains(string(raw), "other-tool notify") {
		t.Errorf("foreign hook removed by uninstall:\n%s", raw)
	}
	if strings.Contains(string(raw), "xfa hook") {
		t.Errorf("xfa hooks survived uninstall:\n%s", raw)
	}
}

func TestUninstallClaudeDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"settings.json", "settings.local.json"} {
		if _, err := os.Stat(filepath.Join(dir, ".claude", f)); !os.IsNotExist(err) {
			t.Errorf("uninstall created %s", f)
		}
	}
}

// Finding 1: uninstall must not rewrite (or back up) a file it didn't change.
func TestUninstallClaudeLeavesForeignOnlyFileUntouched(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	// Deliberately compact formatting: a rewrite would reformat it.
	orig := []byte(`{"permissions":{"allow":["Bash(ls:*)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other-tool notify"}]}]}}`)
	p := filepath.Join(cfgDir, "settings.json")
	os.WriteFile(p, orig, 0o644)
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != string(orig) {
		t.Errorf("untouched settings were rewritten:\n%s", raw)
	}
	if _, err := os.Stat(p + ".xfa-bak"); !os.IsNotExist(err) {
		t.Error("spurious .xfa-bak created for a no-op uninstall")
	}
}

// Finding 2: a malformed settings.json must not abort the rest of uninstall.
func TestUninstallClaudeIsBestEffort(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "settings.json"), []byte(`[1,2,3]`), 0o644)
	os.WriteFile(filepath.Join(cfgDir, "settings.local.json"),
		[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/xfa hook stop"}]}]}}`), 0o644)
	sdir := filepath.Join(cfgDir, "skills", "xfa")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("x"), 0o644)

	err := UninstallClaude(dir)
	if err == nil {
		t.Fatal("want error for malformed settings.json, got nil")
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("error does not mention the malformed file: %v", err)
	}
	local, _ := os.ReadFile(filepath.Join(cfgDir, "settings.local.json"))
	if strings.Contains(string(local), "xfa hook") {
		t.Errorf("settings.local.json not cleaned despite malformed settings.json:\n%s", local)
	}
	if _, err := os.Stat(sdir); !os.IsNotExist(err) {
		t.Error("skill dir not removed despite malformed settings.json")
	}
}

// Finding 3: exe paths with spaces are quoted and still recognized on uninstall.
func TestInstallClaudeQuotedSpacePathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exe := "/Users/me/My Tools/xfa"
	if err := InstallClaude(dir, exe); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(string(raw), `\"/Users/me/My Tools/xfa\" hook session-start`) {
		t.Errorf("space path not quoted:\n%s", raw)
	}
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if strings.Contains(string(raw), "hook session-start") || strings.Contains(string(raw), "hook stop") {
		t.Errorf("quoted-space-path hooks survived uninstall:\n%s", raw)
	}
}

// Finding 4: differently named xfa binaries (xfabin, xfa.exe) are still ours —
// re-init replaces instead of duplicating.
func TestInstallClaudeUpgradesAcrossExeNames(t *testing.T) {
	dir := t.TempDir()
	if err := InstallClaude(dir, "/bin/xfabin"); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(dir, "/bin/xfa.exe"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if strings.Count(string(raw), "hook session-start") != 1 {
		t.Errorf("duplicated hook entries across exe names:\n%s", raw)
	}
	if !strings.Contains(string(raw), "xfa.exe") || strings.Contains(string(raw), "xfabin") {
		t.Errorf("old exe name survived upgrade:\n%s", raw)
	}
}

// Finding 4 flip: a foreign hook mentioning "xfa hook" as prose is NOT ours.
func TestForeignProseXfaHookSurvives(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	prose := `echo 'my xfa hook wrapper' >> log`
	os.WriteFile(filepath.Join(cfgDir, "settings.json"),
		[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+prose+`"}]}]}}`), 0o644)
	if err := InstallClaude(dir, "/x/xfa"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if !strings.Contains(string(raw), "my xfa hook wrapper") {
		t.Errorf("prose hook eaten by install:\n%s", raw)
	}
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if !strings.Contains(string(raw), "my xfa hook wrapper") {
		t.Errorf("prose hook eaten by uninstall:\n%s", raw)
	}
}

// Finding 5: version stamp is trimmed; empty/garbage stamps never block install.
func TestInstallClaudeVersionStampTrimmedAndLenient(t *testing.T) {
	for _, stamp := range []string{"0.1.0\n", "garbage", "  ", ""} {
		dir := t.TempDir()
		sdir := filepath.Join(dir, ".claude", "skills", "xfa")
		os.MkdirAll(sdir, 0o755)
		os.WriteFile(filepath.Join(sdir, ".xfa_version"), []byte(stamp), 0o644)
		os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("stale"), 0o644)
		if err := InstallClaude(dir, "/x/xfa"); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(sdir, "SKILL.md"))
		if string(raw) == "stale" {
			t.Errorf("stamp %q blocked skill upgrade", stamp)
		}
	}
}

// Task 2 (QA remediation): a pre-existing SubagentStop entry from an older
// install is cleaned even when uninstall runs without a prior install call,
// proving the event is on the uninstall event list, not just implied by install.
func TestUninstallClaudeRemovesSubagentStopHook(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "settings.json"),
		[]byte(`{"hooks":{"SubagentStop":[{"hooks":[{"type":"command","command":"\"/x/xfa\" hook subagent-stop"}]}]},"keep":true}`), 0o644)
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if strings.Contains(string(raw), "hook subagent-stop") {
		t.Errorf("SubagentStop hook survived uninstall:\n%s", raw)
	}
	if !strings.Contains(string(raw), "keep") {
		t.Errorf("unrelated keys lost:\n%s", raw)
	}
}

// Task 9: a pre-existing UserPromptSubmit entry from an older install is cleaned
// even when uninstall runs without a prior install call, proving the event is on
// the uninstall event list, not just implied by install.
func TestUninstallClaudeRemovesUserPromptHook(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "settings.json"),
		[]byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"\"/x/xfa\" hook user-prompt"}]}]},"keep":true}`), 0o644)
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if strings.Contains(string(raw), "hook user-prompt") {
		t.Errorf("UserPromptSubmit hook survived uninstall:\n%s", raw)
	}
	if !strings.Contains(string(raw), "keep") {
		t.Errorf("unrelated keys lost:\n%s", raw)
	}
}

func TestUninstallClaudeCleansLocalSettings(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".claude")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "settings.local.json"),
		[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/xfa hook stop"}]}],"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"/x/xfa hook session-start"}]}]},"keep":true}`), 0o644)
	if err := UninstallClaude(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "settings.local.json"))
	if strings.Contains(string(raw), "xfa hook") {
		t.Errorf("xfa hooks survived in settings.local.json:\n%s", raw)
	}
	if !strings.Contains(string(raw), "keep") {
		t.Errorf("unrelated keys lost from settings.local.json:\n%s", raw)
	}
}
