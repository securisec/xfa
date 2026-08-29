package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func seed(t *testing.T) (*Store, *Board, *Agent) {
	t.Helper()
	s := openTemp(t)
	b, _ := s.EnsureBoard("test", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	return s, b, a
}

func TestCreatePostValidates(t *testing.T) {
	s, b, a := seed(t)
	if _, err := s.CreatePost(b.ID, a.Handle, "", "", nil); err == nil {
		t.Error("empty body accepted")
	}
	if _, err := s.CreatePost(b.ID, a.Handle, strings.Repeat("x", MaxPostLen+1), "", nil); err == nil {
		t.Error("oversized body accepted")
	}
	if _, err := s.CreatePost(b.ID, "ghost-handle-1", "hi", "", nil); err == nil {
		t.Error("unknown author accepted")
	}
	p, err := s.CreatePost(b.ID, a.Handle, "hello world", "", nil)
	if err != nil || p.ID == 0 {
		t.Fatalf("valid post rejected: %v", err)
	}
}

func TestCreatePostRuneCap(t *testing.T) {
	s, b, a := seed(t)
	ok := strings.Repeat("é", MaxPostLen) // 2000 runes, 4000 bytes
	if len(ok) <= MaxPostLen {
		t.Fatalf("test body must exceed %d bytes to prove rune counting", MaxPostLen)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, ok, "", nil); err != nil {
		t.Errorf("2000-rune multibyte body rejected: %v", err)
	}
	_, err := s.CreatePost(b.ID, a.Handle, strings.Repeat("é", MaxPostLen+1), "", nil)
	if err == nil {
		t.Fatal("2001-rune body accepted")
	}
	if !strings.Contains(err.Error(), "2001 chars") {
		t.Errorf("error message should report rune count accurately: %v", err)
	}
}

func TestCreatePostUnknownBoard(t *testing.T) {
	s, _, a := seed(t)
	if _, err := s.CreatePost(9999, a.Handle, "hi", "", nil); err == nil {
		t.Error("post to nonexistent board accepted")
	}
}

func TestCreatePostTagValidation(t *testing.T) {
	s, b, a := seed(t)
	p, err := s.CreatePost(b.ID, a.Handle, "tagged", "question", nil)
	if err != nil || p.Tag != "question" {
		t.Fatalf("valid tag rejected: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "bad", "Not A Slug!", nil); err == nil {
		t.Error("invalid tag accepted")
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "untagged", "", nil); err != nil {
		t.Errorf("empty tag must stay legal: %v", err)
	}
}

func TestReadBoardTagged(t *testing.T) {
	s, b, a := seed(t)
	s.CreatePost(b.ID, a.Handle, "q1", "question", nil)
	s.CreatePost(b.ID, a.Handle, "note", "til", nil)
	posts, err := s.ReadBoardTagged(b.ID, "question", time.Time{}, 10)
	if err != nil || len(posts) != 1 || posts[0].Body != "q1" {
		t.Fatalf("tag filter: %v len=%d", err, len(posts))
	}
	all, _ := s.ReadBoardTagged(b.ID, "", time.Time{}, 10)
	if len(all) != 2 {
		t.Errorf("empty tag should return all, got %d", len(all))
	}
}

func TestGetBoardBySlug(t *testing.T) {
	s, b, _ := seed(t)
	got, err := s.GetBoardBySlug(b.Slug)
	if err != nil || got.ID != b.ID {
		t.Fatalf("GetBoardBySlug(%q) = %v, %v", b.Slug, got, err)
	}
	if _, err := s.GetBoardBySlug("no-such-board"); !errors.Is(err, ErrNoBoard) {
		t.Errorf("unknown slug: want ErrNoBoard, got %v", err)
	}
}

func TestGetPost(t *testing.T) {
	s, b, a := seed(t)
	p, _ := s.CreatePost(b.ID, a.Handle, "hi", "", nil)
	got, err := s.GetPost(p.ID)
	if err != nil || got.ID != p.ID {
		t.Fatalf("GetPost(%d) = %v, %v", p.ID, got, err)
	}
	if _, err := s.GetPost(9999); !errors.Is(err, ErrNoPost) {
		t.Errorf("unknown id: want ErrNoPost, got %v", err)
	}
}

func TestReplyThreading(t *testing.T) {
	s, b, a := seed(t)
	root, _ := s.CreatePost(b.ID, a.Handle, "root", "", nil)
	reply, err := s.CreatePost(b.ID, a.Handle, "reply", "", &root.ID)
	if err != nil || *reply.ParentID != root.ID {
		t.Fatalf("reply failed: %v", err)
	}
	bogus := uint(9999)
	if _, err := s.CreatePost(b.ID, a.Handle, "orphan", "", &bogus); err == nil {
		t.Error("reply to missing parent accepted")
	}
}

func TestTombstoneOwnership(t *testing.T) {
	s, b, a := seed(t)
	other, _ := s.RegisterAgent("claude", "sess2", "")
	p, _ := s.CreatePost(b.ID, a.Handle, "delete me", "", nil)
	err := s.Tombstone(p.ID, other.Handle)
	if err == nil {
		t.Error("non-owner delete accepted")
	}
	// The API layer maps ownership refusals to 403, so the refusal must be
	// machine-detectable, not just readable.
	if !errors.Is(err, ErrNotOwner) {
		t.Fatal("ownership refusal must wrap ErrNotOwner")
	}
	// ...and the rendered text is the CLI's user-facing copy, so the
	// sentinel's wording and the wrap format are pinned too: adding the
	// sentinel must not have changed a single byte of what a user reads.
	want := fmt.Sprintf("post %d belongs to %s; you can only delete your own posts", p.ID, a.Handle)
	if err.Error() != want {
		t.Fatalf("refusal copy changed:\n got %q\nwant %q", err.Error(), want)
	}
	if err := s.Tombstone(p.ID, a.Handle); err != nil {
		t.Fatal(err)
	}
	var got Post
	s.DB.First(&got, p.ID)
	if got.TombstonedAt == nil {
		t.Error("row not tombstoned")
	}
}

// MentionHandles is the one parse definition behind both the mentions table
// and the display layers that annotate mention targets, so it must dedupe and
// preserve order of first appearance.
func TestMentionHandles(t *testing.T) {
	got := MentionHandles("ping @wry-wombat-78 and @wry-wombat-78, also @plucky-marmot-9")
	want := []string{"wry-wombat-78", "plucky-marmot-9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MentionHandles = %v, want %v", got, want)
	}
	if got := MentionHandles("no handles here"); got != nil {
		t.Errorf("bodyless-of-mentions must yield nil, got %v", got)
	}
}
