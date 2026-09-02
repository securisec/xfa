package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func claudeSettingsPath(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "settings.json")
}

func claudeSkillDir(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "skills", "xfa")
}

func InstallClaude(projectDir, exePath string) error {
	// Skill: version check, then atomic write.
	if err := installSkillFiles(claudeSkillDir(projectDir)); err != nil {
		return err
	}

	// Hooks: read-merge-write with all defensive protections. The exe path is
	// shell-quoted so paths containing spaces survive the hook shell.
	settings, err := ReadJSONObject(claudeSettingsPath(projectDir))
	if err != nil {
		return err
	}
	if err := UpsertHookEntry(settings, "SessionStart", map[string]any{
		"matcher": "startup|resume|clear|compact",
		"hooks": []any{map[string]any{
			"type": "command", "command": `"` + exePath + `" hook session-start`,
		}},
	}); err != nil {
		return err
	}
	if err := UpsertHookEntry(settings, "Stop", map[string]any{
		"hooks": []any{map[string]any{
			"type": "command", "command": `"` + exePath + `" hook stop`,
		}},
	}); err != nil {
		return err
	}
	if err := UpsertHookEntry(settings, "SubagentStop", map[string]any{
		"hooks": []any{map[string]any{
			"type": "command", "command": `"` + exePath + `" hook subagent-stop`,
		}},
	}); err != nil {
		return err
	}
	if err := UpsertHookEntry(settings, "UserPromptSubmit", map[string]any{
		"hooks": []any{map[string]any{
			"type": "command", "command": `"` + exePath + `" hook user-prompt`,
		}},
	}); err != nil {
		return err
	}
	if err := WriteJSONWithBackup(claudeSettingsPath(projectDir), settings); err != nil {
		return err
	}
	// Clear any pre-rename ("xaf") skill dir so re-init migrates the project.
	_ = removeLegacyXafArtifacts(projectDir, filepath.Join(".claude", "skills", "xaf"))
	// Seed the xfa awareness block into the project's CLAUDE.md so every agent
	// — including nested subagents that never see the SessionStart preamble —
	// learns the board exists and to propagate that awareness downward.
	return upsertAwarenessBlock(projectDir, "CLAUDE.md")
}

// versionNewer reports whether stamp is a strictly newer dotted-integer version
// than cur. An empty or unparseable stamp counts as older, and an unparseable
// cur (an unstamped "dev" build) never defers to the on-disk copy, so in both
// cases the install proceeds.
func versionNewer(stamp, cur string) bool {
	sv, ok := parseVersion(stamp)
	if !ok {
		return false
	}
	cv, ok := parseVersion(cur)
	if !ok {
		return false
	}
	for i := 0; i < len(sv) || i < len(cv); i++ {
		var a, b int
		if i < len(sv) {
			a = sv[i]
		}
		if i < len(cv) {
			b = cv[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func parseVersion(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		nums[i] = n
	}
	return nums, true
}

// removeXfaHooks strips xfa hook entries from the settings file at path.
// A missing or empty file is left untouched (never created), and a file that
// holds no xfa entries is not rewritten (no reformat, no backup churn).
func removeXfaHooks(path string) error {
	settings, err := ReadJSONObject(path)
	if err != nil || len(settings) == 0 {
		return err
	}
	before, err := json.Marshal(settings) // map keys marshal sorted: deterministic
	if err != nil {
		return err
	}
	// Every event any provider installs under; stripping an event a file never
	// had is a no-op (an unchanged file is not rewritten). BeforeAgent is
	// gemini's UserPromptSubmit analogue.
	for _, event := range []string{"SessionStart", "Stop", "SubagentStop", "UserPromptSubmit", "BeforeAgent"} {
		RemoveHookEntries(settings, event)
	}
	after, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	return WriteJSONWithBackup(path, settings)
}

// UninstallClaude is best-effort: a malformed settings file is reported but
// never blocks cleaning the other file or removing the skill directory.
func UninstallClaude(projectDir string) error {
	var errs []error
	for _, f := range []string{"settings.json", "settings.local.json"} {
		p := filepath.Join(projectDir, ".claude", f)
		if err := removeXfaHooks(p); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}
	if err := os.RemoveAll(claudeSkillDir(projectDir)); err != nil {
		errs = append(errs, err)
	}
	// Prune now-empty parents (skills/, but never .claude itself if it has content).
	_ = os.Remove(filepath.Join(projectDir, ".claude", "skills"))
	// Strip the awareness block from CLAUDE.md (or remove the file if it was
	// ours alone); a CLAUDE.md without our markers is left untouched.
	if err := removeAwarenessBlock(projectDir, "CLAUDE.md"); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", filepath.Join(projectDir, "CLAUDE.md"), err))
	}
	// Clean up any pre-rename skill dir too.
	_ = removeLegacyXafArtifacts(projectDir, filepath.Join(".claude", "skills", "xaf"))
	return errors.Join(errs...)
}
