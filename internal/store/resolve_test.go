package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ResolvePath takes the directory to resolve FOR as an explicit argument
// (the hook path passes the hook payload's cwd, everything else the process
// cwd), so none of these tests chdir — resolution must not depend on the
// process working directory.

// writeMarkerFile writes raw marker bytes at dir/.xfa.json.
func writeMarkerFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, MarkerName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return path
}

// Precedence rung 1: a non-empty XFA_DB wins over a marker.
func TestResolvePathEnvBeatsMarker(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFile(t, dir, `{"db":"/somewhere/marker.db"}`+"\n")
	t.Setenv("XFA_DB", "/env/wins.db")

	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/env/wins.db" {
		t.Fatalf("env must beat marker, got %q", got)
	}
}

// An EMPTY XFA_DB falls through to the marker, matching DefaultPath's
// treatment of empty-as-unset.
func TestResolvePathEmptyEnvFallsThroughToMarker(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFile(t, dir, `{"db":"/somewhere/marker.db"}`+"\n")
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/somewhere/marker.db" {
		t.Fatalf("empty XFA_DB must fall through to the marker, got %q", got)
	}
}

// Precedence rung 3: no env, no marker anywhere up the tree -> DefaultPath.
func TestResolvePathNoMarkerFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XFA_DB", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg"))

	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if want := DefaultPath(); got != want {
		t.Fatalf("no marker must fall back to DefaultPath %q, got %q", want, got)
	}
}

// The nearest marker walking UP from the given cwd wins, from arbitrarily
// deep inside the project.
func TestResolvePathWalksUpFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	writeMarkerFile(t, root, `{"db":"/root/marker.db"}`+"\n")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(deep)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/root/marker.db" {
		t.Fatalf("walk-up must find the root marker, got %q", got)
	}
}

// A nearer marker shadows an ancestor's marker: first hit wins.
func TestResolvePathNearestMarkerWins(t *testing.T) {
	root := t.TempDir()
	writeMarkerFile(t, root, `{"db":"/outer/marker.db"}`+"\n")
	inner := filepath.Join(root, "nested")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMarkerFile(t, inner, `{"db":"/inner/marker.db"}`+"\n")
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(inner)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/inner/marker.db" {
		t.Fatalf("nearest marker must win, got %q", got)
	}
}

// Corrupt markers are LOUD errors naming the marker path — never a silent
// fall-through to the global DB (that would fork board data).
func TestResolvePathCorruptMarkerErrors(t *testing.T) {
	cases := map[string]string{
		"invalid json":      `{not json`,
		"not an object":     `["db"]`,
		"missing db key":    `{"other":"x"}`,
		"empty db":          `{"db":""}`,
		"non-string db":     `{"db":42}`,
		"relative db path":  `{"db":"relative/board.db"}`,
		"whitespace object": `null`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			marker := writeMarkerFile(t, dir, content)
			t.Setenv("XFA_DB", "")

			_, err := ResolvePath(dir)
			if err == nil {
				t.Fatalf("corrupt marker %q must error, not fall back", content)
			}
			if !strings.Contains(err.Error(), marker) {
				t.Fatalf("error must name the marker path %q, got: %v", marker, err)
			}
		})
	}
}

// makeLocalDir creates dir/.xfa (the conventional project data directory).
func makeLocalDir(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, LocalDirName)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir local dir: %v", err)
	}
	return path
}

// A bare .xfa/ directory pins the project to <dir>/.xfa/board.db — no marker
// file needed. An empty directory is enough; the DB is created on open.
func TestResolvePathLocalDirResolvesToBoardDB(t *testing.T) {
	dir := t.TempDir()
	makeLocalDir(t, dir)
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if want := LocalDBPath(dir); got != want {
		t.Fatalf("ResolvePath = %q, want %q", got, want)
	}
}

// The local dir is found by the same walk up the tree the marker uses.
func TestResolvePathWalksUpToLocalDir(t *testing.T) {
	root := t.TempDir()
	makeLocalDir(t, root)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(sub)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if want := LocalDBPath(root); got != want {
		t.Fatalf("ResolvePath = %q, want %q", got, want)
	}
}

// At the SAME level, the explicit .xfa.json pin beats the conventional dir.
func TestResolvePathMarkerBeatsLocalDirSameLevel(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(t.TempDir(), "custom.db")
	writeMarkerFile(t, dir, `{"db":"`+custom+`"}`+"\n")
	makeLocalDir(t, dir)
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != custom {
		t.Fatalf("marker must win at the same level, got %q want %q", got, custom)
	}
}

// Nearest level wins across KINDS too: a child's .xfa/ shadows a parent's
// .xfa.json.
func TestResolvePathNearerLocalDirBeatsParentMarker(t *testing.T) {
	root := t.TempDir()
	writeMarkerFile(t, root, `{"db":"/outer/marker.db"}`+"\n")
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	makeLocalDir(t, child)
	t.Setenv("XFA_DB", "")

	got, err := ResolvePath(child)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if want := LocalDBPath(child); got != want {
		t.Fatalf("nearer local dir must win, got %q want %q", got, want)
	}
}

// .xfa existing as a plain FILE is a loud error naming the path — the same
// no-silent-fork philosophy as a corrupt marker.
func TestResolvePathLocalDirFileIsLoudError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LocalDirName)
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Setenv("XFA_DB", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg"))

	got, err := ResolvePath(dir)
	if err == nil {
		t.Fatalf("a non-directory .xfa must error, got %q", got)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error must name the path %q, got: %v", path, err)
	}
}

// Precedence rung 1 still wins over the conventional dir.
func TestResolvePathEnvBeatsLocalDir(t *testing.T) {
	dir := t.TempDir()
	makeLocalDir(t, dir)
	t.Setenv("XFA_DB", "/env/wins.db")

	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/env/wins.db" {
		t.Fatalf("env must beat the local dir, got %q", got)
	}
}

// WriteMarker creates a compact one-line JSON object with a trailing newline.
func TestWriteMarkerFreshFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMarker(dir, "/custom/board.db"); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, MarkerName))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got := string(raw); got != `{"db":"/custom/board.db"}`+"\n" {
		t.Fatalf("marker content = %q", got)
	}
}

// Rewriting an existing marker preserves foreign keys and updates db.
func TestWriteMarkerPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFile(t, dir, `{"db":"/old.db","note":"keep me"}`+"\n")
	if err := WriteMarker(dir, "/new.db"); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, MarkerName))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"db":"/new.db"`) {
		t.Fatalf("db not updated: %q", got)
	}
	if !strings.Contains(got, `"note":"keep me"`) {
		t.Fatalf("foreign key dropped: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("missing trailing newline: %q", got)
	}
}

// WriteMarker refuses to clobber a marker that is not a JSON object.
func TestWriteMarkerRefusesCorruptExisting(t *testing.T) {
	dir := t.TempDir()
	marker := writeMarkerFile(t, dir, `{broken`)
	err := WriteMarker(dir, "/new.db")
	if err == nil {
		t.Fatal("WriteMarker over a corrupt marker must error")
	}
	if !strings.Contains(err.Error(), marker) {
		t.Fatalf("error must name the marker path, got: %v", err)
	}
}
