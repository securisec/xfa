package install

// codex provider install/uninstall.
//
// Mechanism verified against codex's docs (learn.chatgpt.com/docs/hooks,
// 2026-08-26):
//   - Hooks live in a project-level .codex/hooks.json whose "hooks" key maps
//     event names to matcher groups — byte-shape-identical to Claude Code's
//     settings.json, so the existing ReadJSONObject / UpsertHookEntry /
//     RemoveHookEntries / WriteJSONWithBackup helpers apply unchanged.
//   - SessionStart, UserPromptSubmit, Stop and SubagentStop all exist — exactly
//     the four events xfa installs for claude. Omitting "matcher" matches every
//     occurrence, so xfa omits it rather than guessing codex's matcher tokens.
//   - Hook input is JSON on stdin with session_id / cwd / hook_event_name and
//     output supports hookSpecificOutput.additionalContext — the same contract
//     `xfa hook` already implements, so codex needs zero hook-runtime changes.
//   - The default hook timeout is 600s (claude's is 60), so every installed
//     entry carries an explicit "timeout": 30 — an xfa hook must never stall a
//     session.
//   - Skills are auto-discovered from .agents/skills/<name>/SKILL.md with
//     Claude-compatible frontmatter, so skill.Content installs verbatim and
//     nothing has to be registered in config.
//   - AGENTS.md is codex's always-on rules file, so the awareness block goes
//     there — the same file opencode and pi seed.
//   - Trust: codex makes the human review and approve each hook definition
//     (/hooks) before it runs. Nothing to do at install time; install prints a
//     one-line note so the human knows to.
//   - No legacy "xaf" cleanup: codex postdates the rename.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func codexHooksPath(projectDir string) string {
	return filepath.Join(projectDir, ".codex", "hooks.json")
}

func codexSkillDir(projectDir string) string {
	return filepath.Join(projectDir, ".agents", "skills", "xfa")
}

// codexHookEntry builds one matcher-less hook group invoking `xfa hook <sub>`.
// The exe path is shell-quoted so paths containing spaces survive the hook
// shell, and the explicit timeout overrides codex's 600s default.
func codexHookEntry(exePath, sub string) map[string]any {
	return map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": `"` + exePath + `" hook ` + sub,
			"timeout": 30,
		}},
	}
}

func InstallCodex(projectDir, exePath string) error {
	// Skill: codex's native .agents/skills location, version-checked like the
	// other providers.
	if err := installSkillFiles(codexSkillDir(projectDir)); err != nil {
		return err
	}

	// Hooks: read-merge-write with all defensive protections (refuse a
	// non-object file, back up before writing, skip byte-identical writes,
	// replace-then-append so re-init upgrades instead of duplicating).
	hooks, err := ReadJSONObject(codexHooksPath(projectDir))
	if err != nil {
		return err
	}
	for _, ev := range []struct{ event, sub string }{
		{"SessionStart", "session-start"},
		{"Stop", "stop"},
		{"SubagentStop", "subagent-stop"},
		{"UserPromptSubmit", "user-prompt"},
	} {
		if err := UpsertHookEntry(hooks, ev.event, codexHookEntry(exePath, ev.sub)); err != nil {
			return err
		}
	}
	if err := WriteJSONWithBackup(codexHooksPath(projectDir), hooks); err != nil {
		return err
	}

	// AGENTS.md is codex's always-on rules file, so nested agents that never
	// see the SessionStart digest still learn the board exists. Shared with
	// opencode and pi — the upsert is idempotent across all three.
	if err := upsertAwarenessBlock(projectDir, "AGENTS.md"); err != nil {
		return err
	}

	// Codex will not run a hook the human has not trusted, and nothing at
	// install time can grant that trust — so say so.
	fmt.Println("note: codex requires approving hooks before they run — run /hooks inside codex to trust them.")
	return nil
}

// UninstallCodex is best-effort: it removes exactly what InstallCodex created
// (xfa's hook entries + skill dir + awareness block) and prunes parents only
// when empty, so foreign hooks, foreign skills, and a non-empty .agents always
// survive. hooks.json itself is never deleted — a file with no xfa entries is
// not even rewritten, and a missing one is never created.
func UninstallCodex(projectDir string) error {
	var errs []error
	hp := codexHooksPath(projectDir)
	if err := removeXfaHooks(hp); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", hp, err))
	}
	if err := os.RemoveAll(codexSkillDir(projectDir)); err != nil {
		errs = append(errs, err)
	}
	// Strip the awareness block from AGENTS.md (or remove the file if it was
	// ours alone); an AGENTS.md without our markers is left untouched.
	if err := removeAwarenessBlock(projectDir, "AGENTS.md"); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", filepath.Join(projectDir, "AGENTS.md"), err))
	}
	// os.Remove fails (silently, here) on non-empty dirs, so this only prunes
	// directories the install created and nothing else repopulated.
	for _, d := range []string{
		filepath.Join(projectDir, ".agents", "skills"),
		filepath.Join(projectDir, ".agents"),
	} {
		_ = os.Remove(d)
	}
	return errors.Join(errs...)
}
