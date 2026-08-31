package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The xfa awareness block is delimited by HTML-comment markers so it is
// invisible in rendered markdown and can be found, upgraded, and removed
// without touching anything a user wrote around it. Everything between the
// markers is owned by `xfa init` and replaced verbatim on re-init. The same
// block seeds Claude Code's CLAUDE.md and opencode's AGENTS.md.
const (
	awarenessBeginMarker = "<!-- xfa:begin (managed by `xfa init` — edits between the markers are overwritten on re-init) -->"
	awarenessEndMarker   = "<!-- xfa:end -->"
)

// awarenessBlock is the managed block written into a project's rules file
// (CLAUDE.md for claude, AGENTS.md for opencode). It mirrors the skill's
// propagation and reply guidance so every agent — at every nesting depth —
// learns the board exists and hands that awareness down the spawn tree. Kept
// short on purpose: a rules file is always-on context.
const awarenessBlock = awarenessBeginMarker + `
## xfa — the agent message board

This project has an ` + "`xfa`" + ` board: a shared message board agents use to ask, answer, and record what they learn. A few rules keep it working:

- **Every agent uses xfa** — the main session, orchestrators, workers, tech leads, and any subagent at any depth. An agent that isn't on the board can't answer questions, never shares what it learns, and hides everything its own subagents find.
- **Awareness does not arrive on its own.** Nothing tells a spawned agent that xfa exists — the agent that spawns it must say so. Whenever you spawn a subagent, in its prompt tell it to register (` + "`xfa register --parent <your-handle> --session <session-id>`" + `, the same session id as yours), catch up with ` + "`xfa read --unread`" + `, and do exactly the same for every agent IT spawns. Skip this for one agent and its whole branch of the tree goes dark.
- **Respond in-thread.** Answering, confirming, or correcting a specific post is ` + "`xfa reply <id>`" + ` — never a new top-level post that @mentions the author. Replies thread and land in the inbox; broadcasts scatter the conversation and never resolve. Announce a multi-step task once; status updates on it are replies on that announcement, not new posts.
` + awarenessEndMarker

// upsertAwarenessBlock writes or refreshes the xfa managed block in the named
// rules file under projectDir (e.g. "CLAUDE.md" or "AGENTS.md"), creating the
// file when it does not exist. It replaces the block in place when the markers
// are already present (upgrade), appends it after existing content otherwise,
// and never alters bytes outside the markers. Defensive like the settings
// writer: it refuses to overwrite a file it cannot read, skips a byte-identical
// write, backs the original up to `*.xfa-bak`, preserves the file mode, and
// writes atomically.
func upsertAwarenessBlock(projectDir, filename string) error {
	path := filepath.Join(projectDir, filename)
	existing, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err // never overwrite a file we could not read
	}

	var out []byte
	if missing {
		out = []byte(awarenessBlock + "\n")
	} else {
		out = []byte(mergeAwarenessBlock(string(existing)))
		if bytes.Equal(existing, out) {
			return nil // byte-identical: no write, no backup churn
		}
	}

	mode := os.FileMode(0o644)
	if !missing {
		if fi, statErr := os.Stat(path); statErr == nil {
			mode = fi.Mode().Perm()
		}
		if err := WriteFileAtomic(path+".xfa-bak", existing, mode); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return WriteFileAtomic(path, out, mode)
}

// mergeAwarenessBlock returns content with the xfa block replaced in place if
// its markers are present, or appended after a blank-line separator if not.
func mergeAwarenessBlock(content string) string {
	if b, e, ok := blockSpan(content); ok {
		return content[:b] + awarenessBlock + content[e:]
	}
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed == "" {
		return awarenessBlock + "\n"
	}
	return trimmed + "\n\n" + awarenessBlock + "\n"
}

// removeAwarenessBlock strips the xfa block from the named rules file under
// projectDir. A file that held only our block (init created it) is removed; a
// file with other content keeps that content with just the block excised. A
// file without our markers is left completely untouched. Backs up before any
// change.
func removeAwarenessBlock(projectDir, filename string) error {
	path := filepath.Join(projectDir, filename)
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(existing)
	b, e, ok := blockSpan(content)
	if !ok {
		return nil // not ours: never touch a file we didn't write into
	}
	stripped := strings.TrimSpace(content[:b] + content[e:])

	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := WriteFileAtomic(path+".xfa-bak", existing, mode); err != nil {
		return err
	}
	if stripped == "" {
		return os.Remove(path) // the file was ours alone
	}
	return WriteFileAtomic(path, []byte(stripped+"\n"), mode)
}

// blockSpan locates the managed block within content, returning the byte range
// [begin, end) that spans the begin marker through the end marker (inclusive of
// the end marker). ok is false when a well-formed block is not present.
func blockSpan(content string) (begin, end int, ok bool) {
	b := strings.Index(content, awarenessBeginMarker)
	if b < 0 {
		return 0, 0, false
	}
	e := strings.Index(content[b:], awarenessEndMarker)
	if e < 0 {
		return 0, 0, false
	}
	return b, b + e + len(awarenessEndMarker), true
}

// removeLegacyXafArtifacts deletes install artifacts left by pre-rename ("xaf")
// versions of this tool, which the current "xfa"-named install/uninstall paths
// do not recognize. opencode auto-loads every plugin in .opencode/plugins, so a
// stale xaf.js keeps shelling out to a binary that no longer exists ("opencode
// still looks for xaf") until it is removed. Best-effort and idempotent: a
// missing artifact is not an error. Run on both install and uninstall so a
// simple re-init migrates a project off the old name. paths are project-root
// relative.
func removeLegacyXafArtifacts(projectDir string, paths ...string) error {
	var errs []error
	for _, rel := range paths {
		if err := os.RemoveAll(filepath.Join(projectDir, rel)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
