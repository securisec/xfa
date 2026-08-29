package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotObject = errors.New("config file is not a JSON object; refusing to modify it")

// ErrMalformedConfig is returned when an existing config value has an
// unexpected shape (e.g. "hooks" is not an object); xfa refuses to clobber it.
var ErrMalformedConfig = errors.New("existing config has an unexpected shape; refusing to modify it")

func ReadJSONObject(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // UTF-8 BOM
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil // empty/whitespace-only file: treat as {}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, ErrNotObject
	}
	if m == nil {
		return nil, ErrNotObject // JSON "null" decodes to a nil map with no error
	}
	return m, nil
}

func WriteJSONWithBackup(path string, data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	mode := os.FileMode(0o644)
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr // never overwrite a file we could not read
	}
	if readErr == nil {
		if bytes.Equal(existing, out) {
			return nil // byte-identical: no write, no backup churn
		}
		if fi, statErr := os.Stat(path); statErr == nil {
			mode = fi.Mode().Perm() // preserve existing permissions
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

// writeFileIfChanged atomically writes data to path unless the file already
// holds exactly these bytes (no rewrite, no mtime churn on re-init).
func writeFileIfChanged(path string, data []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return WriteFileAtomic(path, data, mode)
}

func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".xfa-tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// UpsertHookEntry removes prior xfa entries under hooks.<event>, then appends
// entry. It refuses (with an error wrapping ErrMalformedConfig) to touch a
// settings map whose "hooks" value is not an object or whose hooks[event]
// value is not a list.
func UpsertHookEntry(settings map[string]any, event string, entry map[string]any) error {
	hooksRaw, present := settings["hooks"]
	hooks, isMap := hooksRaw.(map[string]any)
	if present && !isMap {
		return fmt.Errorf("settings key %q is not an object: %w", "hooks", ErrMalformedConfig)
	}
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	listRaw, present := hooks[event]
	list, isList := listRaw.([]any)
	if present && !isList {
		return fmt.Errorf("hooks key %q is not a list: %w", event, ErrMalformedConfig)
	}
	hooks[event] = append(withoutXfaEntries(list), any(entry))
	return nil
}

// RemoveHookEntries strips all xfa entries under hooks.<event>, deleting the
// event key when its list becomes empty and the hooks key when it becomes empty.
func RemoveHookEntries(settings map[string]any, event string) {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	list, ok := hooks[event].([]any)
	if !ok {
		return
	}
	kept := withoutXfaEntries(list)
	if len(kept) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
}

// withoutXfaEntries filters out hook entries that invoke xfa's hook subcommands.
func withoutXfaEntries(list []any) []any {
	kept := make([]any, 0, len(list))
	for _, e := range list {
		if !isXfaEntry(e) {
			kept = append(kept, e)
		}
	}
	return kept
}

// isXfaEntry walks the entry's hooks[].command strings structurally; the entry
// is ours when any of its commands is an xfa hook invocation.
func isXfaEntry(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok && isXfaHookCommand(cmd) {
			return true
		}
	}
	return false
}

// isXfaHookCommand reports whether cmd invokes xfa's hook subcommands: it must
// contain " hook session-start", " hook stop", " hook subagent-stop", or
// " hook user-prompt", and the whitespace-delimited token immediately before
// " hook " must mention "xfa". This recognizes /path/to/xfa, "quoted path with
// spaces/xfa", xfa.exe, and xfa-dev, but not prose like
// `echo 'my xfa hook wrapper' >> log`. Every subcommand xfa installs must be
// listed here, or re-init duplicates its entry and uninstall orphans it.
func isXfaHookCommand(cmd string) bool {
	var exe string
	// Order is not load-bearing — no entry is a substring of another (the
	// leading " hook " never aligns inside a longer subcommand). Membership is:
	// a subcommand missing from this list is unrecognized on re-init.
	for _, sub := range []string{" hook session-start", " hook subagent-stop", " hook stop", " hook user-prompt"} {
		if i := strings.Index(cmd, sub); i >= 0 {
			exe = cmd[:i]
			break
		}
	}
	if exe == "" {
		return false
	}
	fields := strings.Fields(exe)
	if len(fields) == 0 {
		return false
	}
	return strings.Contains(fields[len(fields)-1], "xfa")
}
