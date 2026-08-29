package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestHardDeletePostRemovesWholeSubtreeDepth3(t *testing.T) {
	s, b, a := seed(t)
	root, _ := s.CreatePost(b.ID, a.Handle, "root", "", nil)
	r1, _ := s.CreatePost(b.ID, a.Handle, "r1", "", &root.ID)
	r2, _ := s.CreatePost(b.ID, a.Handle, "r2", "", &r1.ID)
	r3, _ := s.CreatePost(b.ID, a.Handle, "r3", "", &r2.ID)
	other, _ := s.CreatePost(b.ID, a.Handle, "unrelated thread", "", nil)

	n, err := s.HardDeletePost(root.ID)
	if err != nil {
		t.Fatalf("HardDeletePost: %v", err)
	}
	if n != 4 {
		t.Fatalf("removed count = %d, want 4", n)
	}
	for _, id := range []uint{root.ID, r1.ID, r2.ID, r3.ID} {
		if _, err := s.GetPost(id); !errors.Is(err, ErrNoPost) {
			t.Errorf("post %d still present after hard delete: %v", id, err)
		}
	}
	if _, err := s.GetPost(other.ID); err != nil {
		t.Errorf("unrelated thread was touched: %v", err)
	}
}

func TestHardDeletePostMidThreadRemovesOnlyItsSubtree(t *testing.T) {
	s, b, a := seed(t)
	root, _ := s.CreatePost(b.ID, a.Handle, "root", "", nil)
	doomed, _ := s.CreatePost(b.ID, a.Handle, "doomed branch", "", &root.ID)
	grandchild, _ := s.CreatePost(b.ID, a.Handle, "doomed grandchild", "", &doomed.ID)
	sibling, _ := s.CreatePost(b.ID, a.Handle, "surviving sibling", "", &root.ID)

	n, err := s.HardDeletePost(doomed.ID)
	if err != nil {
		t.Fatalf("HardDeletePost: %v", err)
	}
	if n != 2 {
		t.Fatalf("removed count = %d, want 2", n)
	}
	if _, err := s.GetPost(root.ID); err != nil {
		t.Errorf("root should survive: %v", err)
	}
	if _, err := s.GetPost(sibling.ID); err != nil {
		t.Errorf("sibling should survive: %v", err)
	}
	for _, id := range []uint{doomed.ID, grandchild.ID} {
		if _, err := s.GetPost(id); !errors.Is(err, ErrNoPost) {
			t.Errorf("post %d still present: %v", id, err)
		}
	}
}

func TestHardDeletePostRemovesMentionsButLeavesSurvivorMentions(t *testing.T) {
	s, b, a := seed(t)
	other, _ := s.RegisterAgent("claude", "sess2", "")
	doomed, _ := s.CreatePost(b.ID, a.Handle, "hey @"+other.Handle, "", nil)
	survivor, _ := s.CreatePost(b.ID, a.Handle, "yo @"+other.Handle, "", nil)

	var before int64
	s.DB.Model(&Mention{}).Count(&before)
	if before != 2 {
		t.Fatalf("setup: expected 2 mention rows, got %d", before)
	}

	if _, err := s.HardDeletePost(doomed.ID); err != nil {
		t.Fatalf("HardDeletePost: %v", err)
	}

	var doomedMentions int64
	s.DB.Model(&Mention{}).Where("post_id = ?", doomed.ID).Count(&doomedMentions)
	if doomedMentions != 0 {
		t.Errorf("mentions of deleted post still present: %d", doomedMentions)
	}
	var survivorMentions int64
	s.DB.Model(&Mention{}).Where("post_id = ?", survivor.ID).Count(&survivorMentions)
	if survivorMentions != 1 {
		t.Errorf("mentions of surviving post = %d, want 1", survivorMentions)
	}
}

func TestHardDeletePostRemovesFromSearchIndex(t *testing.T) {
	s, b, a := seed(t)
	doomed, _ := s.CreatePost(b.ID, a.Handle, "kumquat marmalade recipe", "", nil)
	survivor, _ := s.CreatePost(b.ID, a.Handle, "kumquat jam is different", "", nil)

	before, err := s.Search("kumquat", 0, 10)
	if err != nil || len(before) != 2 {
		t.Fatalf("setup search: %v len=%d want 2", err, len(before))
	}

	if _, err := s.HardDeletePost(doomed.ID); err != nil {
		t.Fatalf("HardDeletePost: %v", err)
	}

	// Assert the FTS index itself, not through Search (see comment above):
	// exactly the survivor's row should still match "kumquat".
	var idxCount int64
	if err := s.DB.Raw(`SELECT count(*) FROM posts_fts WHERE posts_fts MATCH 'kumquat'`).Scan(&idxCount).Error; err != nil {
		t.Fatalf("query fts index: %v", err)
	}
	if idxCount != 1 {
		t.Errorf("fts index rows matching kumquat = %d, want 1 (deleted post's row must be gone)", idxCount)
	}
	// fts5's integrity-check command errors with "database disk image is
	// malformed" when the external-content index disagrees with the content
	// table (e.g. a stale row left behind by a missing delete trigger).
	if err := s.DB.Exec(`INSERT INTO posts_fts(posts_fts, rank) VALUES('integrity-check', 1)`).Error; err != nil {
		t.Errorf("fts integrity check failed (stale/orphaned index rows): %v", err)
	}

	after, err := s.Search("kumquat", 0, 10)
	if err != nil || len(after) != 1 || after[0].ID != survivor.ID {
		t.Fatalf("post-delete search: %v len=%d want [survivor %d]", err, len(after), survivor.ID)
	}
}

// A DB created before the posts_fts_ad trigger existed must pick it up on the
// next Open (CREATE TRIGGER IF NOT EXISTS inside ftsSchema), so a hard delete
// against a reopened, previously-migrated DB still keeps the search index
// in sync.
func TestFTSDeleteTriggerAddedOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, _ := s.EnsureBoard("test", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	p, err := s.CreatePost(b.ID, a.Handle, "pre-trigger post about wombats", "", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	// Simulate a DB created before the _ad trigger existed.
	if err := s.DB.Exec(`DROP TRIGGER posts_fts_ad`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// The meaningful assertion for "reopen re-adds the trigger" is against
	// sqlite_master directly, not a round-trip through hard-delete-then-
	// search: that would die uninformatively back at the DROP TRIGGER setup
	// step above in the world where the trigger was never defined at all
	// (nothing to drop), instead of failing here with a clear message.
	var triggerCount int64
	if err := s2.DB.Raw(`SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'posts_fts_ad'`).Scan(&triggerCount).Error; err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if triggerCount != 1 {
		t.Fatalf("posts_fts_ad trigger not present after reopen, count=%d", triggerCount)
	}

	if _, err := s2.HardDeletePost(p.ID); err != nil {
		t.Fatalf("HardDeletePost after reopen: %v", err)
	}

	// Same direct-index checks as TestHardDeletePostRemovesFromSearchIndex:
	// Search alone cannot distinguish a working trigger from a broken one.
	var idxCount int64
	if err := s2.DB.Raw(`SELECT count(*) FROM posts_fts WHERE posts_fts MATCH 'wombats'`).Scan(&idxCount).Error; err != nil {
		t.Fatalf("query fts index: %v", err)
	}
	if idxCount != 0 {
		t.Errorf("fts index rows matching wombats = %d, want 0 after hard delete", idxCount)
	}
	if err := s2.DB.Exec(`INSERT INTO posts_fts(posts_fts, rank) VALUES('integrity-check', 1)`).Error; err != nil {
		t.Errorf("fts integrity check failed after reopen-re-added-trigger hard delete: %v", err)
	}
}

func TestHardDeletePostNotFound(t *testing.T) {
	s, _, _ := seed(t)
	if _, err := s.HardDeletePost(9999); !errors.Is(err, ErrNoPost) {
		t.Errorf("unknown post: want ErrNoPost, got %v", err)
	}
}

// HardDeleteSession must remove every post authored by the session's agents,
// plus any foreign reply nested under a doomed post (subtree death), while
// leaving a foreign root alone even when one of the session's own posts is a
// doomed branch under it. Agent rows, read cursors, and unrelated threads
// survive; the session's name row does not.
func TestHardDeleteSessionRemovesSubtreesAndForeignReplies(t *testing.T) {
	s, b, _ := seed(t)
	a1, _ := s.RegisterAgent("claude", "session-1", "")
	a2, _ := s.RegisterAgent("claude", "session-2", "")

	if err := s.SetSessionName("session-1", "Doomed Session"); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}
	if err := s.MarkReadID(a1.Handle, b.ID, 0); err != nil {
		t.Fatalf("MarkReadID: %v", err)
	}

	// Thread A: root authored by the doomed session; a foreign reply nested
	// under it must die with the subtree, and a further doomed-session leaf
	// nested under that foreign reply must also die.
	rootA, _ := s.CreatePost(b.ID, a1.Handle, "rootA by session-1", "", nil)
	foreignReplyA, _ := s.CreatePost(b.ID, a2.Handle, "session-2 reply under doomed root", "", &rootA.ID)
	leafA, _ := s.CreatePost(b.ID, a1.Handle, "session-1 leaf under foreign reply @"+a2.Handle, "", &foreignReplyA.ID)

	// Thread B: foreign root (session-2) survives even though the doomed
	// session has a reply subtree under it.
	rootB, _ := s.CreatePost(b.ID, a2.Handle, "rootB foreign root", "", nil)
	doomedReplyB, _ := s.CreatePost(b.ID, a1.Handle, "session-1 reply under foreign root", "", &rootB.ID)
	foreignGrandchildB, _ := s.CreatePost(b.ID, a2.Handle, "session-2 grandchild under doomed reply", "", &doomedReplyB.ID)

	// Thread C: entirely session-2, untouched.
	rootC, _ := s.CreatePost(b.ID, a2.Handle, "rootC untouched thread", "", nil)
	survivorMention, _ := s.CreatePost(b.ID, a2.Handle, "ping @"+a1.Handle+" for review", "", &rootC.ID)

	n, err := s.HardDeleteSession("session-1")
	if err != nil {
		t.Fatalf("HardDeleteSession: %v", err)
	}
	if n != 5 {
		t.Fatalf("removed count = %d, want 5", n)
	}

	doomedIDs := []uint{rootA.ID, foreignReplyA.ID, leafA.ID, doomedReplyB.ID, foreignGrandchildB.ID}
	for _, id := range doomedIDs {
		if _, err := s.GetPost(id); !errors.Is(err, ErrNoPost) {
			t.Errorf("post %d should be gone: %v", id, err)
		}
	}
	survivingIDs := []uint{rootB.ID, rootC.ID, survivorMention.ID}
	for _, id := range survivingIDs {
		if _, err := s.GetPost(id); err != nil {
			t.Errorf("post %d should survive: %v", id, err)
		}
	}

	var leafMentions int64
	s.DB.Model(&Mention{}).Where("post_id = ?", leafA.ID).Count(&leafMentions)
	if leafMentions != 0 {
		t.Errorf("mentions of a doomed post survived: %d", leafMentions)
	}
	var survivorMentions int64
	s.DB.Model(&Mention{}).Where("post_id = ?", survivorMention.ID).Count(&survivorMentions)
	if survivorMentions != 1 {
		t.Errorf("mentions of surviving post = %d, want 1", survivorMentions)
	}

	// Agent rows and read cursors survive a session hard delete.
	if _, err := s.GetAgent(a1.Handle); err != nil {
		t.Errorf("agent row for doomed session should survive: %v", err)
	}
	var cursors int64
	s.DB.Model(&ReadCursor{}).Where("handle = ?", a1.Handle).Count(&cursors)
	if cursors != 1 {
		t.Errorf("read cursor for doomed session's agent should survive, count=%d", cursors)
	}

	name, err := s.GetSessionName("session-1")
	if err != nil {
		t.Fatalf("GetSessionName: %v", err)
	}
	if name != "" {
		t.Errorf("session name row should be gone, got %q", name)
	}
}

func TestHardDeleteSessionZeroPostsStillClearsName(t *testing.T) {
	s, _, _ := seed(t)
	if _, err := s.RegisterAgent("claude", "session-empty", ""); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if err := s.SetSessionName("session-empty", "Quiet Session"); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}
	n, err := s.HardDeleteSession("session-empty")
	if err != nil {
		t.Fatalf("HardDeleteSession: %v", err)
	}
	if n != 0 {
		t.Errorf("removed count = %d, want 0", n)
	}
	name, err := s.GetSessionName("session-empty")
	if err != nil || name != "" {
		t.Errorf("name row should be gone: name=%q err=%v", name, err)
	}
}

func TestHardDeleteSessionEmptyID(t *testing.T) {
	s, _, _ := seed(t)
	if _, err := s.HardDeleteSession(""); !errors.Is(err, ErrNoSessionID) {
		t.Errorf("empty session id: want ErrNoSessionID, got %v", err)
	}
	if _, err := s.HardDeleteSession("   "); !errors.Is(err, ErrNoSessionID) {
		t.Errorf("whitespace session id: want ErrNoSessionID, got %v", err)
	}
}

func TestHardDeleteSessionUnknown(t *testing.T) {
	s, _, _ := seed(t)
	if _, err := s.HardDeleteSession("no-such-session"); !errors.Is(err, ErrNoSession) {
		t.Errorf("unknown session: want ErrNoSession, got %v", err)
	}
}
