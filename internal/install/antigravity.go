package install

// antigravity (Google Antigravity IDE) provider install/uninstall.
//
// Mechanism verified against antigravity's docs (antigravity.google/docs/ide/
// hooks/, /docs/ide/skills/, /docs/ide/rules/, 2026-08-27):
//   - Hooks live in a project-level .agents/hooks.json keyed at the TOP level
//     by hook NAME, not a "hooks" key: {"<name>": {"enabled": true,
//     "<Event>": [...]}}. The top-level keying means xfa simply owns one
//     "xfa" key wholesale: install upserts the whole key, uninstall deletes
//     exactly that key, and foreign hook-name keys survive both.
//     UpsertHookEntry / RemoveHookEntries / removeXfaHooks assume the
//     "hooks"-key layout and deliberately do NOT apply here.
//   - Event STRUCTURE splits by kind (verified against the agy CLI binary's
//     embedded hooks doc, agy 1.1.22, 2026-08-27 — the website docs never
//     show a lifecycle-event example): only the TOOL events (PreToolUse /
//     PostToolUse) use the grouped {"matcher": ..., "hooks": [...]} wrapper;
//     lifecycle events (PreInvocation, PostInvocation, Stop) take handler
//     objects DIRECTLY in the event array, flat. A lifecycle handler wrapped
//     Claude-style in {"hooks":[...]} silently registers nothing.
//   - There is no SessionStart / UserPromptSubmit / SubagentStop. xfa installs
//     exactly two lifecycle events: PreInvocation (before each model call —
//     the conversation's first invocation carries the session-start preamble,
//     later ones the user-prompt digest) and Stop.
//   - The agy CLI reads .agents/hooks.json as a customization root just like
//     the IDE (confirmed same source), so one install serves both.
//   - Antigravity's hook I/O is not Claude-shaped (camelCase conversationId /
//     workspacePaths in, injectSteps / decision:"continue" out), so the
//     entries run the dedicated `xfa hook antigravity-invoke` /
//     `antigravity-stop` adapters rather than the Claude-shaped events.
//   - Timeouts are SECONDS (default 30); every entry carries an explicit 10
//     because PreInvocation fires on every model call and an xfa hook must
//     never stall the loop.
//   - Skills are auto-discovered from .agents/skills/<name>/SKILL.md — the
//     SAME dir codex installs to, deliberately shared (installSkillFiles is
//     idempotent across the two). Caveat, mirroring the AGENTS.md three-way
//     share: uninstalling either codex or antigravity removes the shared
//     skill; re-running `xfa init` for the survivor restores it.
//   - Always-on context is a workspace rule at .agents/rules/xfa.md carrying
//     the shared awareness block. The frontmatter is Windsurf-style
//     `trigger: always_on` (antigravity is Windsurf-derived; its exact rule
//     frontmatter is undocumented — best-effort: worst case the rule is
//     manually activatable and the PreInvocation injection still carries
//     awareness).
//   - Trust: the docs describe no hook approval workflow (only the "enabled"
//     flag), so no install-time note is printed.
//   - No legacy "xaf" cleanup: antigravity postdates the rename.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func antigravityHooksPath(projectDir string) string {
	return filepath.Join(projectDir, ".agents", "hooks.json")
}

func antigravityRulePath(projectDir string) string {
	return filepath.Join(projectDir, ".agents", "rules", "xfa.md")
}

// antigravityRule is the always-on workspace rule: Windsurf-style frontmatter
// (best-effort — see the mechanism comment above) over the shared awareness
// block. Wholly xfa-owned, byte-stable across re-inits.
const antigravityRule = "---\ntrigger: always_on\n---\n\n" + awarenessBlock + "\n"

// antigravityXfaKey builds the whole top-level "xfa" hooks entry. Lifecycle
// events take handler objects directly in the event array — FLAT, never the
// tool-event {"matcher","hooks"} wrapper (see the mechanism comment: a
// wrapped lifecycle handler silently registers nothing). The exe path is
// shell-quoted so paths containing spaces survive the hook shell, and the
// explicit 10-second timeout undercuts antigravity's 30s default.
func antigravityXfaKey(exePath string) map[string]any {
	handler := func(sub string) []any {
		return []any{map[string]any{
			"type":    "command",
			"command": `"` + exePath + `" hook ` + sub,
			"timeout": 10,
		}}
	}
	return map[string]any{
		"enabled":       true,
		"PreInvocation": handler("antigravity-invoke"),
		"Stop":          handler("antigravity-stop"),
	}
}

func InstallAntigravity(projectDir, exePath string) error {
	// Skill: the codex-shared .agents/skills location (see mechanism comment),
	// version-checked like the other providers.
	if err := installSkillFiles(codexSkillDir(projectDir)); err != nil {
		return err
	}

	// Hooks: own the "xfa" key wholesale, with all defensive protections
	// (refuse a non-object file, back up before writing, skip byte-identical
	// writes; replacing the whole key makes re-init an upgrade by construction).
	hooks, err := ReadJSONObject(antigravityHooksPath(projectDir))
	if err != nil {
		return err
	}
	hooks["xfa"] = antigravityXfaKey(exePath)
	if err := WriteJSONWithBackup(antigravityHooksPath(projectDir), hooks); err != nil {
		return err
	}

	// Always-on workspace rule, so nested agents that never see the
	// PreInvocation digest still learn the board exists.
	rp := antigravityRulePath(projectDir)
	if err := os.MkdirAll(filepath.Dir(rp), 0o755); err != nil {
		return err
	}
	return writeFileIfChanged(rp, []byte(antigravityRule), 0o644)
}

// UninstallAntigravity is best-effort: it removes exactly what
// InstallAntigravity created (the "xfa" hooks key + skill dir + rule file)
// and prunes parents only when empty, so foreign hook-name keys, foreign
// skills, and a non-empty .agents always survive. hooks.json itself is never
// deleted — a file without the xfa key is not even rewritten, and a missing
// one is never created.
func UninstallAntigravity(projectDir string) error {
	var errs []error
	hp := antigravityHooksPath(projectDir)
	if hooks, err := ReadJSONObject(hp); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", hp, err))
	} else if _, ok := hooks["xfa"]; ok {
		delete(hooks, "xfa")
		if err := WriteJSONWithBackup(hp, hooks); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", hp, err))
		}
	}
	if err := os.RemoveAll(codexSkillDir(projectDir)); err != nil {
		errs = append(errs, err)
	}
	if err := os.Remove(antigravityRulePath(projectDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	// os.Remove fails (silently, here) on non-empty dirs, so this only prunes
	// directories the install created and nothing else repopulated — a
	// hooks.json keeps .agents alive.
	for _, d := range []string{
		filepath.Join(projectDir, ".agents", "rules"),
		filepath.Join(projectDir, ".agents", "skills"),
		filepath.Join(projectDir, ".agents"),
	} {
		_ = os.Remove(d)
	}
	return errors.Join(errs...)
}
