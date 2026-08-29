package install

// gemini (Gemini CLI) provider install/uninstall.
//
// Mechanism verified against gemini's docs (geminicli.com/docs/hooks/,
// 2026-08-27):
//   - Hooks live in the project-level .gemini/settings.json (gemini's general
//     settings file, like .claude/settings.json) under a top-level "hooks" key
//     mapping event names to matcher groups — byte-shape-identical to Claude
//     Code's settings.json, so the existing ReadJSONObject / UpsertHookEntry /
//     RemoveHookEntries / WriteJSONWithBackup helpers apply unchanged.
//   - Only SessionStart and BeforeAgent are installed. BeforeAgent fires after
//     each user prompt, before planning, and its returned
//     hookSpecificOutput.additionalContext is appended to that turn's prompt —
//     UserPromptSubmit semantics, so it runs `xfa hook user-prompt`. There is
//     NO Stop/SubagentStop equivalent: AfterAgent only accepts allow/deny (not
//     the "block" xfa's stop hook emits) and SessionEnd is advisory-only, so
//     the stop nudge is skipped — the pi precedent. Omitting "matcher" matches
//     every occurrence.
//   - Hook input is JSON on stdin with session_id / cwd / hook_event_name and
//     output supports hookSpecificOutput.additionalContext — the same contract
//     `xfa hook` already implements, so gemini needs zero hook-runtime changes.
//   - Hook timeouts are in MILLISECONDS (default 60000), so every installed
//     entry carries an explicit "timeout": 30000 — an xfa hook must never
//     stall a session. This is why codexHookEntry (seconds) is not reused.
//   - Skills are auto-discovered from .gemini/skills/<name>/SKILL.md (Agent
//     Skills open standard, Claude-compatible frontmatter), so skill.Content
//     installs verbatim and nothing has to be registered in config. gemini
//     also reads .agents/skills, but that dir is codex's — sharing it would
//     couple the two providers' uninstalls.
//   - GEMINI.md is gemini's always-on context file (it does not read AGENTS.md
//     by default), so the awareness block goes there — shared with no other
//     provider.
//   - Trust: gemini fingerprints project hooks and warns before running
//     changed/untrusted ones. Nothing to do at install time; install prints a
//     one-line note so the human knows to approve them.
//   - No legacy "xaf" cleanup: gemini postdates the rename.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func geminiSettingsPath(projectDir string) string {
	return filepath.Join(projectDir, ".gemini", "settings.json")
}

func geminiSkillDir(projectDir string) string {
	return filepath.Join(projectDir, ".gemini", "skills", "xfa")
}

// geminiHookEntry builds one matcher-less hook group invoking `xfa hook <sub>`.
// The exe path is shell-quoted so paths containing spaces survive the hook
// shell, and the explicit millisecond timeout overrides gemini's 60s default.
func geminiHookEntry(exePath, sub string) map[string]any {
	return map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": `"` + exePath + `" hook ` + sub,
			"timeout": 30000,
		}},
	}
}

func InstallGemini(projectDir, exePath string) error {
	// Skill: gemini's native .gemini/skills location, version-checked like the
	// other providers.
	if err := installSkillFiles(geminiSkillDir(projectDir)); err != nil {
		return err
	}

	// Hooks: read-merge-write with all defensive protections (refuse a
	// non-object file, back up before writing, skip byte-identical writes,
	// replace-then-append so re-init upgrades instead of duplicating).
	settings, err := ReadJSONObject(geminiSettingsPath(projectDir))
	if err != nil {
		return err
	}
	for _, ev := range []struct{ event, sub string }{
		{"SessionStart", "session-start"},
		{"BeforeAgent", "user-prompt"},
	} {
		if err := UpsertHookEntry(settings, ev.event, geminiHookEntry(exePath, ev.sub)); err != nil {
			return err
		}
	}
	if err := WriteJSONWithBackup(geminiSettingsPath(projectDir), settings); err != nil {
		return err
	}

	// GEMINI.md is gemini's always-on context file, so nested agents that
	// never see the SessionStart digest still learn the board exists.
	if err := upsertAwarenessBlock(projectDir, "GEMINI.md"); err != nil {
		return err
	}

	// Gemini fingerprints project hooks and asks the human before running
	// untrusted ones — nothing at install time can grant that trust, so say so.
	fmt.Println("note: gemini requires approving hooks before they run — use gemini's /hooks panel to trust them.")
	return nil
}

// UninstallGemini is best-effort: it removes exactly what InstallGemini
// created (xfa's hook entries + skill dir + awareness block) and prunes
// parents only when empty, so foreign hooks, foreign settings keys, foreign
// skills, and a non-empty .gemini always survive. settings.json itself is
// never deleted — a file with no xfa entries is not even rewritten, and a
// missing one is never created.
func UninstallGemini(projectDir string) error {
	var errs []error
	sp := geminiSettingsPath(projectDir)
	if err := removeXfaHooks(sp); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", sp, err))
	}
	if err := os.RemoveAll(geminiSkillDir(projectDir)); err != nil {
		errs = append(errs, err)
	}
	// Strip the awareness block from GEMINI.md (or remove the file if it was
	// ours alone); a GEMINI.md without our markers is left untouched.
	if err := removeAwarenessBlock(projectDir, "GEMINI.md"); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", filepath.Join(projectDir, "GEMINI.md"), err))
	}
	// os.Remove fails (silently, here) on non-empty dirs, so this only prunes
	// directories the install created and nothing else repopulated — a
	// settings.json keeps .gemini alive.
	for _, d := range []string{
		filepath.Join(projectDir, ".gemini", "skills"),
		filepath.Join(projectDir, ".gemini"),
	} {
		_ = os.Remove(d)
	}
	return errors.Join(errs...)
}
