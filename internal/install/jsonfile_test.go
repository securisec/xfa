package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadJSONObjectRefusesNonObject(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`["not","an","object"]`), 0o644)
	if _, err := ReadJSONObject(p); !errors.Is(err, ErrNotObject) {
		t.Errorf("want ErrNotObject, got %v", err)
	}
}

func TestReadJSONObjectMissingFileIsEmpty(t *testing.T) {
	m, err := ReadJSONObject(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(m) != 0 {
		t.Errorf("got %v, %v", m, err)
	}
}

func TestReadJSONObjectNullIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte("null"), 0o644)
	m, err := ReadJSONObject(p)
	if !errors.Is(err, ErrNotObject) {
		t.Errorf("want ErrNotObject for null, got map=%v err=%v", m, err)
	}
}

func TestReadJSONObjectEmptyOrWhitespaceIsEmpty(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"empty.json":      "",
		"whitespace.json": " \n\t\n",
		"bom-only.json":   "\xef\xbb\xbf",
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o644)
		m, err := ReadJSONObject(p)
		if err != nil || m == nil || len(m) != 0 {
			t.Errorf("%s: want empty map, nil error; got map=%v err=%v", name, m, err)
		}
	}
}

func TestWriteJSONWithBackupSkipsIdenticalAndBacksUp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if err := WriteJSONWithBackup(p, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	if err := WriteJSONWithBackup(p, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("identical write should be skipped")
	}
	if err := WriteJSONWithBackup(p, map[string]any{"a": 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".xfa-bak"); err != nil {
		t.Error("backup missing after real change")
	}
}

func TestWriteJSONWithBackupPropagatesReadError(t *testing.T) {
	// Path is a directory: reading it fails with an error that is not
	// ErrNotExist; the write must abort rather than rename over it.
	dir := filepath.Join(t.TempDir(), "iamadir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONWithBackup(dir, map[string]any{"a": 1}); err == nil {
		t.Error("want error when existing path is unreadable, got nil")
	}
	if _, err := os.Stat(dir + ".xfa-bak"); err == nil {
		t.Error("no backup should be written when the original could not be read")
	}

	// Unreadable regular file: abort with the read error, never overwrite.
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are not enforced")
	}
	p := filepath.Join(t.TempDir(), "locked.json")
	if err := os.WriteFile(p, []byte(`{"keep":"me"}`), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONWithBackup(p, map[string]any{"a": 1}); err == nil {
		t.Error("want error when existing file is unreadable, got nil")
	}
	os.Chmod(p, 0o644)
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"keep":"me"}` {
		t.Errorf("unreadable file was overwritten: %q", got)
	}
	if _, err := os.Stat(p + ".xfa-bak"); err == nil {
		t.Error("no backup should be written when the original could not be read")
	}
}

func TestWriteJSONWithBackupPreservesMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(p, []byte("{\n  \"a\": 1\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONWithBackup(p, map[string]any{"a": 2}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("rewritten file mode = %o, want 600", got)
	}
	bi, err := os.Stat(p + ".xfa-bak")
	if err != nil {
		t.Fatal(err)
	}
	if got := bi.Mode().Perm(); got != 0o600 {
		t.Errorf("backup mode = %o, want 600", got)
	}
}

func TestUpsertHookEntryReplacesOwnEntries(t *testing.T) {
	settings := map[string]any{}
	old := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/old/xfa hook session-start"}}}
	if err := UpsertHookEntry(settings, "SessionStart", old); err != nil {
		t.Fatal(err)
	}
	fresh := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/new/xfa hook session-start"}}}
	if err := UpsertHookEntry(settings, "SessionStart", fresh); err != nil {
		t.Fatal(err)
	}
	list := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(list) != 1 {
		t.Fatalf("duplicated: %d entries", len(list))
	}
}

func TestUpsertHookEntryRefusesMalformedShapes(t *testing.T) {
	entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/bin/xfa hook session-start"}}}

	// settings["hooks"] exists but is not a map: error, value untouched.
	settings := map[string]any{"hooks": "surprise, a string"}
	if err := UpsertHookEntry(settings, "SessionStart", entry); err == nil {
		t.Error("want error when hooks is not an object, got nil")
	}
	if settings["hooks"] != "surprise, a string" {
		t.Errorf("malformed hooks value was clobbered: %v", settings["hooks"])
	}

	// hooks[event] exists but is not a list: error, value untouched.
	settings2 := map[string]any{"hooks": map[string]any{"SessionStart": "not a list"}}
	if err := UpsertHookEntry(settings2, "SessionStart", entry); err == nil {
		t.Error("want error when hooks[event] is not a list, got nil")
	}
	if settings2["hooks"].(map[string]any)["SessionStart"] != "not a list" {
		t.Errorf("malformed event value was clobbered: %v", settings2["hooks"])
	}
}

func TestRemoveHookEntriesStripsXfaAndPrunes(t *testing.T) {
	xfaEntry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/bin/xfa hook session-start"}}}
	otherEntry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/bin/other-tool"}}}

	// Event with both xfa and non-xfa entries: xfa removed, other preserved.
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{xfaEntry, otherEntry},
			"Stop":         []any{xfaEntry},
		},
		"model": "opus",
	}
	RemoveHookEntries(settings, "SessionStart")
	list := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 surviving entry, got %d", len(list))
	}
	if list[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "/bin/other-tool" {
		t.Errorf("non-xfa entry not preserved: %v", list[0])
	}

	// Event whose list becomes empty: event key deleted; hooks key survives (Stop remains).
	settings2 := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{xfaEntry},
			"Stop":         []any{otherEntry},
		},
	}
	RemoveHookEntries(settings2, "SessionStart")
	hooks2 := settings2["hooks"].(map[string]any)
	if _, ok := hooks2["SessionStart"]; ok {
		t.Error("empty event key should be deleted")
	}
	if _, ok := hooks2["Stop"]; !ok {
		t.Error("unrelated event key must survive")
	}

	// hooks map becomes empty: "hooks" key itself deleted.
	settings3 := map[string]any{
		"hooks": map[string]any{"SessionStart": []any{xfaEntry}},
	}
	RemoveHookEntries(settings3, "SessionStart")
	if _, ok := settings3["hooks"]; ok {
		t.Error("empty hooks key should be deleted")
	}

	// Absent hooks: no panic, settings untouched.
	settings4 := map[string]any{"model": "opus"}
	RemoveHookEntries(settings4, "SessionStart")
	if len(settings4) != 1 {
		t.Errorf("settings without hooks should be untouched: %v", settings4)
	}
}

func TestIsXfaHookCommand(t *testing.T) {
	ours := []string{
		"/usr/local/bin/xfa hook session-start",
		"/bin/xfa hook stop",
		`"/Users/me/My Tools/xfa" hook session-start`,
		`C:\bin\xfa.exe hook stop`,
		"/opt/xfa-dev hook session-start",
		`"/usr/local/bin/xfa" hook user-prompt`,
		"/bin/xfa hook subagent-stop",
	}
	for _, cmd := range ours {
		if !isXfaHookCommand(cmd) {
			t.Errorf("want ours: %q", cmd)
		}
	}
	foreign := []string{
		"echo 'my xfa hook wrapper' >> log",
		"other-tool notify",
		"/bin/other hook session-start", // right subcommand, not our binary
		"/bin/other hook user-prompt",   // right subcommand, not our binary
		"xfa post something",            // our binary, not a hook invocation
		" hook stop",                    // no exe token at all
	}
	for _, cmd := range foreign {
		if isXfaHookCommand(cmd) {
			t.Errorf("want foreign: %q", cmd)
		}
	}
}
