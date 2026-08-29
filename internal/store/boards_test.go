package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"agents_messageboard": "agents-messageboard",
		"My Cool  Repo!":      "my-cool-repo",
		"---x---":             "x",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureBoardIdempotent(t *testing.T) {
	s := openTemp(t)
	a, _ := s.EnsureBoard("general", "misc")
	b, err := s.EnsureBoard("general", "ignored on second call")
	if err != nil || a.ID != b.ID {
		t.Fatalf("EnsureBoard not idempotent: %v %v err=%v", a, b, err)
	}
}

func TestResolveBoardWalksUp(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("proj", "")
	root := t.TempDir()
	if err := s.RegisterProject(root, b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveBoard(filepath.Join(root, "deep", "nested"))
	if err != nil || got.Slug != "proj" {
		t.Fatalf("ResolveBoard = %v, %v", got, err)
	}
	if _, err := s.ResolveBoard(t.TempDir()); !errors.Is(err, ErrNoBoard) {
		t.Errorf("want ErrNoBoard, got %v", err)
	}
}

func TestSlugifyDegenerate(t *testing.T) {
	// Documents that names with no ASCII alphanumerics slugify to "".
	// Callers must handle the empty result (EnsureBoard rejects it).
	for _, in := range []string{"!!!", "日本語プロジェクト", "Кириллица"} {
		if got := Slugify(in); got != "" {
			t.Errorf("Slugify(%q) = %q, want \"\"", in, got)
		}
	}
}

func TestEnsureBoardEmptySlugErrors(t *testing.T) {
	s := openTemp(t)
	if b, err := s.EnsureBoard("", "desc"); err == nil {
		t.Fatalf("EnsureBoard(\"\") = %v, want error", b)
	}
}

func TestResolveBoardThroughSymlink(t *testing.T) {
	s := openTemp(t)
	b, err := s.EnsureBoard("sym", "")
	if err != nil {
		t.Fatal(err)
	}
	real := t.TempDir()
	if err := s.RegisterProject(real, b.ID); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := s.ResolveBoard(link)
	if err != nil || got.Slug != "sym" {
		t.Fatalf("ResolveBoard(symlink) = %v, %v; want board sym", got, err)
	}
}

func TestResolveBoardDanglingBoardID(t *testing.T) {
	s := openTemp(t)
	root := t.TempDir()
	if err := s.RegisterProject(root, 9999); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveBoard(root)
	if !errors.Is(err, ErrNoBoard) {
		t.Fatalf("want ErrNoBoard, got %v", err)
	}
	if got != nil {
		t.Errorf("want nil board on error, got %v", got)
	}
}

func TestListBoards(t *testing.T) {
	s := openTemp(t)
	for _, slug := range []string{"zeta", "alpha", "mid"} {
		if _, err := s.EnsureBoard(slug, "desc-"+slug); err != nil {
			t.Fatal(err)
		}
	}
	bs, err := s.ListBoards()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range bs {
		got = append(got, b.Slug)
	}
	want := []string{"alpha", "mid", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListBoards order = %v, want %v", got, want)
	}
	if bs[0].Description != "desc-alpha" {
		t.Errorf("Description = %q, want %q", bs[0].Description, "desc-alpha")
	}
}

func TestRegisterProjectRepoints(t *testing.T) {
	s := openTemp(t)
	a, err := s.EnsureBoard("aaa", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureBoard("bbb", "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := s.RegisterProject(root, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterProject(root, b.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := s.DB.Model(&Project{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("project rows = %d, want 1", count)
	}
	got, err := s.ResolveBoard(root)
	if err != nil || got.ID != b.ID {
		t.Fatalf("ResolveBoard after repoint = %v, %v; want board %d", got, err, b.ID)
	}
}
