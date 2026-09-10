package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsRemote(t *testing.T) {
	for in, want := range map[string]bool{
		"http://host:7777":         true,
		"https://host":             true,
		"HTTP://host":              false, // lowercase only, like every marker value we write
		"/abs/path/board.db":       false,
		"":                         false,
		"http://user:pw@host:7777": false, // userinfo refused
		"ftp://host":               false,
	} {
		if got := IsRemote(in); got != want {
			t.Errorf("IsRemote(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMarkerAcceptsURL(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMarker(dir, "http://host:7777"); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://host:7777" {
		t.Fatalf("ResolvePath = %q", got)
	}
}

func TestMarkerRejectsUserinfoAndRelative(t *testing.T) {
	for _, bad := range []string{"http://u:p@host", "relative/board.db"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, MarkerName), []byte(`{"db":"`+bad+`"}`), 0o644)
		_, err := ResolvePath(dir)
		if err == nil || !strings.Contains(err.Error(), `"db" must be an absolute path or http(s) URL`) {
			t.Fatalf("%s: err = %v", bad, err)
		}
	}
}

// Open must refuse every URL-looking string, not just the ones IsRemote
// accepts: a userinfo or uppercase-scheme value is NOT remote, so it would
// otherwise reach MkdirAll and create a literal "http:" directory.
func TestOpenRefusesURLs(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	for _, u := range []string{"http://host:7777", "http://u:p@host", "HTTP://host"} {
		_, err := Open(u)
		if err == nil || !strings.HasPrefix(err.Error(), "cannot open "+u+" as a database") {
			t.Fatalf("%s: err = %v", u, err)
		}
	}
	entries, _ := os.ReadDir(wd)
	if len(entries) != 0 {
		t.Fatalf("Open created %v", entries)
	}
}

func TestEnsureBoardCapsSlug(t *testing.T) {
	s := openTemp(t)
	_, err := s.EnsureBoard(strings.Repeat("a", MaxSlugLen+1), "")
	if err == nil || !strings.Contains(err.Error(), "board slug too long") {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.EnsureBoard(strings.Repeat("a", MaxSlugLen), ""); err != nil {
		t.Fatalf("64-char slug refused: %v", err)
	}
}

func TestNormalizePathExported(t *testing.T) {
	if NormalizePath("/a/b/../c") != "/a/c" {
		t.Fatal("NormalizePath must Clean")
	}
}
