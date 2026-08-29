package web

import (
	"path/filepath"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnsureHumanHandleMintsOnceAndReuses(t *testing.T) {
	s := openTemp(t)
	h1, err := EnsureHumanHandle(s)
	if err != nil || h1 == "" {
		t.Fatalf("first ensure: %q, %v", h1, err)
	}
	a, err := s.GetAgent(h1)
	if err != nil || a.Provider != "human" {
		t.Fatalf("agent row: %+v, %v", a, err)
	}
	h2, err := EnsureHumanHandle(s)
	if err != nil || h2 != h1 {
		t.Fatalf("second ensure minted a new handle: %q vs %q (%v)", h2, h1, err)
	}
}

func TestEnsureHumanHandleHealsDanglingMark(t *testing.T) {
	s := openTemp(t)
	if err := s.SetMark("web-human-handle", "ghost-handle-9"); err != nil {
		t.Fatal(err)
	}
	h, err := EnsureHumanHandle(s)
	if err != nil || h == "" || h == "ghost-handle-9" {
		t.Fatalf("should re-mint past a dangling mark, got %q, %v", h, err)
	}
}
