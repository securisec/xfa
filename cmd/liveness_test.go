package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// staleNoteFixture seeds a board with a question asked by one handle that is
// also named by an @mention in a second post, then backdates that handle by
// idleFor. Backdating happens after both posts exist because CreatePost
// heartbeats its author. Returns the store, the board, the (possibly stale)
// asker's handle, the fresh reader's handle, and the question post.
func staleNoteFixture(t *testing.T, board string, idleFor time.Duration) (*store.Store, *store.Board, string, string, *store.Post) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard(board, "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	asker, err := s.RegisterAgent("claude", "asker-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	reader, err := s.RegisterAgent("claude", "reader-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	q := mustPostTagged(t, s, b.ID, asker.Handle, "does the WAL checkpoint on close?", "question", nil)
	// A second post names the same handle: the note must still be one line.
	mustPost(t, s, b.ID, reader.Handle, fmt.Sprintf("@%s asked this before", asker.Handle), nil)
	if idleFor > 0 {
		if err := s.DB.Model(&store.Agent{}).Where("handle = ?", asker.Handle).
			Update("last_seen_at", time.Now().Add(-idleFor)).Error; err != nil {
			t.Fatalf("backdate last_seen_at: %v", err)
		}
	}
	return s, b, asker.Handle, reader.Handle, q
}

// A page of posts that addresses an idle handle — as a question's asker, as an
// @mention target, or both — carries one probabilistic note line per handle.
//
// The flags are spelled out on every `read` below because runXfa drives the
// package-global rootCmd: cobra flag values set by an earlier test (e.g.
// `--unread --as <handle>` in read_test.go) stay set for later ones.
func TestReadPrintsStaleNotes(t *testing.T) {
	_, _, asker, _, _ := staleNoteFixture(t, "cmdreadstale", time.Hour)

	out := runXfa(t, "read", "--board", "cmdreadstale", "--json=false", "--unread=false", "--limit", "20")
	want := fmt.Sprintf("note: %s last seen 1h ago — don't wait on them", asker)
	if !strings.Contains(out, want) {
		t.Errorf("read must annotate the idle asker with %q:\n%s", want, out)
	}
	if n := strings.Count(out, "note:"); n != 1 {
		t.Errorf("asker + mention target are the same handle: want 1 note line, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "does the WAL checkpoint on close?") {
		t.Errorf("the posts themselves must still be rendered:\n%s", out)
	}
	for _, banned := range []string{"gone", "dead", "offline"} {
		if strings.Contains(out, banned) {
			t.Errorf("note must stay probabilistic, found %q in:\n%s", banned, out)
		}
	}
}

// Fresh handles get no note, and an @mention of a handle that was never
// registered is not a liveness signal at all — absence is data, not staleness.
func TestStaleNotesSkipFreshAndUnknown(t *testing.T) {
	s, b, _, reader, _ := staleNoteFixture(t, "cmdreadfresh", 0)
	// @ghost-fox-9 was never registered: LastSeenFor simply omits it.
	mustPost(t, s, b.ID, reader, "@ghost-fox-9 might know", nil)

	out := runXfa(t, "read", "--board", "cmdreadfresh", "--json=false", "--unread=false", "--limit", "20")
	if strings.Contains(out, "note:") {
		t.Errorf("fresh asker and unregistered mention target must produce no notes:\n%s", out)
	}
}

// The --unread branch renders its own page and must annotate it too — but only
// when posts were actually shown ("all caught up" stays a bare one-liner).
func TestReadUnreadPrintsStaleNotes(t *testing.T) {
	_, _, asker, reader, _ := staleNoteFixture(t, "cmdunreadstale", time.Hour)

	out := runXfa(t, "read", "--board", "cmdunreadstale", "--unread", "--as", reader, "--json=false", "--limit", "20")
	want := fmt.Sprintf("note: %s last seen 1h ago — don't wait on them", asker)
	if !strings.Contains(out, want) {
		t.Errorf("read --unread must annotate the idle asker with %q:\n%s", want, out)
	}
	// Everything is now read: the caught-up path prints no note lines.
	out = runXfa(t, "read", "--board", "cmdunreadstale", "--unread", "--as", reader, "--json=false", "--limit", "20")
	if strings.Contains(out, "note:") {
		t.Errorf("an empty unread page must carry no notes:\n%s", out)
	}
}

// `thread` annotates the same way as `read`.
func TestThreadPrintsStaleNotes(t *testing.T) {
	s, b, asker, reader, q := staleNoteFixture(t, "cmdthreadstale", time.Hour)
	mustPost(t, s, b.ID, reader, "on the last connection close", &q.ID)

	out := runXfa(t, "thread", fmt.Sprint(q.ID), "--json=false")
	want := fmt.Sprintf("note: %s last seen 1h ago — don't wait on them", asker)
	if !strings.Contains(out, want) {
		t.Errorf("thread must annotate the idle asker with %q:\n%s", want, out)
	}
}

// `inbox` annotates the same way as `read`.
func TestInboxPrintsStaleNotes(t *testing.T) {
	s, b, asker, reader, _ := staleNoteFixture(t, "cmdinboxstale", 0)
	mustPostTagged(t, s, b.ID, asker, fmt.Sprintf("@%s any idea?", reader), "question", nil)
	if err := s.DB.Model(&store.Agent{}).Where("handle = ?", asker).
		Update("last_seen_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}

	out := runXfa(t, "inbox", "--as", reader, "--json=false", "--limit", "20")
	want := fmt.Sprintf("note: %s last seen 1h ago — don't wait on them", asker)
	if !strings.Contains(out, want) {
		t.Errorf("inbox must annotate the idle asker with %q:\n%s", want, out)
	}
	if strings.Contains(out, fmt.Sprintf("note: %s", reader)) {
		t.Errorf("the fresh reader must not be annotated:\n%s", out)
	}
}

// unreadNagFixture seeds a board with `posts` messages written by "other" and
// returns the store, the board and the two handles used by the write-time
// unread nag tests: the other poster and the never-read author.
func unreadNagFixture(t *testing.T, board string, posts int) (*store.Store, *store.Board, string, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard(board, "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	other, err := s.RegisterAgent("claude", board+"-other-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	me, err := s.RegisterAgent("claude", board+"-me-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	for i := 0; i < posts; i++ {
		mustPost(t, s, b.ID, other.Handle, fmt.Sprintf("finding number %d", i), nil)
	}
	return s, b, other.Handle, me.Handle
}

// Every write doubles as a read prompt: posting while behind the board prints
// one trailer with the author's unread count. The author's own brand-new post
// must not be counted — two foreign posts plus mine means "(2 unread ...)".
func TestPostNagsUnread(t *testing.T) {
	_, _, _, me := unreadNagFixture(t, "cmdpostnag", 2)

	out := runXfa(t, "post", "my own discovery", "--board", "cmdpostnag", "--as", me, "--tag", "", "--json=false")
	if !strings.Contains(out, "posted #") {
		t.Errorf("the post confirmation must still be printed:\n%s", out)
	}
	if !strings.Contains(out, "(2 unread — xfa read --unread)") {
		t.Errorf("want the unread trailer counting only foreign posts:\n%s", out)
	}
	if strings.Contains(out, "(3 unread") {
		t.Errorf("the author's own post must never be counted as unread:\n%s", out)
	}
}

// A caught-up author gets nothing: `read --unread` advances the cursor, so the
// next write is silent (N == 0 prints no line at all).
func TestPostNoNagWhenCaughtUp(t *testing.T) {
	_, _, _, me := unreadNagFixture(t, "cmdpostnagcaught", 2)

	runXfa(t, "read", "--board", "cmdpostnagcaught", "--unread", "--as", me, "--json=false", "--limit", "20")
	out := runXfa(t, "post", "my own discovery", "--board", "cmdpostnagcaught", "--as", me, "--tag", "", "--json=false")
	if !strings.Contains(out, "posted #") {
		t.Errorf("the post confirmation must still be printed:\n%s", out)
	}
	if strings.Contains(out, "unread") {
		t.Errorf("a caught-up author must get no unread trailer:\n%s", out)
	}
}

// `reply` carries the same trailer, on the parent's board, after every other
// hint line.
func TestReplyNagsUnread(t *testing.T) {
	s, b, other, me := unreadNagFixture(t, "cmdreplynag", 2)
	var parent store.Post
	if err := s.DB.Where("board_id = ? AND author_handle = ?", b.ID, other).
		Order("id ASC").First(&parent).Error; err != nil {
		t.Fatalf("load parent post: %v", err)
	}

	out := runXfa(t, "reply", fmt.Sprint(parent.ID), "answering here", "--as", me, "--json=false")
	if !strings.Contains(out, fmt.Sprintf("replied #%d -> #%d", parent.ID+2, parent.ID)) {
		t.Errorf("the reply confirmation must still be printed:\n%s", out)
	}
	if !strings.Contains(out, "(2 unread — xfa read --unread)") {
		t.Errorf("want the unread trailer on reply:\n%s", out)
	}
	if !strings.HasSuffix(out, "(2 unread — xfa read --unread)\n") {
		t.Errorf("the nag must be the last line, after every other hint:\n%s", out)
	}
}

// --json output must stay a single well-formed document: no note lines.
func TestStaleNotesAbsentFromJSON(t *testing.T) {
	_, _, _, _, _ = staleNoteFixture(t, "cmdreadjson", time.Hour)

	out := runXfa(t, "read", "--board", "cmdreadjson", "--json", "--unread=false", "--limit", "20")
	if strings.Contains(out, "note:") {
		t.Errorf("--json read must not carry note lines:\n%s", out)
	}
	var posts []store.Post
	if err := json.Unmarshal([]byte(out), &posts); err != nil {
		t.Fatalf("--json read must decode as a bare JSON array: %v (output %q)", err, out)
	}
	if len(posts) != 2 {
		t.Errorf("want 2 posts decoded, got %d", len(posts))
	}
}
