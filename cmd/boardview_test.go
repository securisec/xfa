package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// NOTE (harness footgun, see read_test.go): pflag values persist between
// Execute() calls on the shared rootCmd — every flag a subtest cares about is
// set explicitly on every run, including --json=false on text runs.

// mustPost creates a post via the store or fails the test.
func mustPost(t *testing.T, s *store.Store, boardID uint, author, body string, parentID *uint) *store.Post {
	t.Helper()
	p, err := s.CreatePost(boardID, author, body, "", parentID)
	if err != nil {
		t.Fatalf("CreatePost(%q): %v", body, err)
	}
	time.Sleep(2 * time.Millisecond) // distinct ms (ms-quantized timestamps)
	return p
}

func TestThreadsShowsReplyCountsAndActivityOrder(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdthreads", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "threads-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Thread A is the OLDER root but has the newest activity via a nested
	// grandchild reply; thread B is a newer, reply-less root. Ordering must be
	// by last activity (A first), and A's count must be the subtree size minus
	// one (2: child + grandchild), not the direct-children count (1).
	rootA := mustPost(t, s, b.ID, a.Handle, "alpha root", nil)
	rootB := mustPost(t, s, b.ID, a.Handle, "beta root", nil)
	child := mustPost(t, s, b.ID, a.Handle, "alpha child", &rootA.ID)
	mustPost(t, s, b.ID, a.Handle, "alpha grandchild", &child.ID)
	_ = rootB

	out := runXfa(t, "threads", "--board", "cmdthreads", "--limit", "50", "--json=false")
	iA := strings.Index(out, "alpha root")
	iB := strings.Index(out, "beta root")
	if iA < 0 || iB < 0 || iA > iB {
		t.Fatalf("threads must order by last activity (alpha before beta):\n%s", out)
	}
	if !strings.Contains(out, "2 replies") {
		t.Errorf("thread A must count the whole subtree (2 replies):\n%s", out)
	}
	if !strings.Contains(out, "0 replies") {
		t.Errorf("thread B must show 0 replies:\n%s", out)
	}
	if !strings.Contains(out, "active ") {
		t.Errorf("threads must show last activity:\n%s", out)
	}

	// --limit 1 keeps only the most recently active thread; non-positive
	// limits clamp to the default instead of slicing to nothing.
	out = runXfa(t, "threads", "--board", "cmdthreads", "--limit", "1", "--json=false")
	if !strings.Contains(out, "alpha root") || strings.Contains(out, "beta root") {
		t.Errorf("--limit 1 must keep only the most active thread:\n%s", out)
	}
	out = runXfa(t, "threads", "--board", "cmdthreads", "--limit", "0", "--json=false")
	if !strings.Contains(out, "beta root") {
		t.Errorf("--limit 0 must clamp to the default, not drop everything:\n%s", out)
	}

	// JSON: replies + last activity ride along with the root post.
	out = runXfa(t, "threads", "--board", "cmdthreads", "--limit", "50", "--json")
	var got []struct {
		ID           uint
		Body         string
		Replies      int
		LastActivity time.Time
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad --json output %q: %v", out, err)
	}
	if len(got) != 2 || got[0].Body != "alpha root" || got[0].Replies != 2 || got[0].LastActivity.IsZero() {
		t.Fatalf("--json = %+v; want alpha root first with 2 replies and a last activity", got)
	}

	// Empty board: [] under --json (never null), a friendly line otherwise.
	if _, err := s.EnsureBoard("threadless", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	out = runXfa(t, "threads", "--board", "threadless", "--limit", "50", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty board --json = %q, want []", out)
	}
	out = runXfa(t, "threads", "--board", "threadless", "--limit", "50", "--json=false")
	if !strings.Contains(out, "no threads on b/threadless") {
		t.Errorf("empty board text = %q", out)
	}
}

func TestBoardRendersEveryThreadIndented(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdboard", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "board-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	root1 := mustPost(t, s, b.ID, a.Handle, "first topic", nil)
	reply := mustPost(t, s, b.ID, a.Handle, "secret reply", &root1.ID)
	mustPost(t, s, b.ID, a.Handle, "second topic", nil)
	if err := s.Tombstone(reply.ID, a.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	out := runXfa(t, "board", "--board", "cmdboard", "--json=false")
	i1 := strings.Index(out, "first topic")
	i2 := strings.Index(out, "second topic")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Fatalf("roots must render in chronological order:\n%s", out)
	}
	if !strings.Contains(out, "\n  #") {
		t.Errorf("replies must be indented under their root:\n%s", out)
	}
	if !strings.Contains(out, "[deleted]") || strings.Contains(out, "secret reply") {
		t.Errorf("tombstoned reply must appear masked, never filtered or leaked:\n%s", out)
	}
	if !strings.Contains(out, "\n\n") {
		t.Errorf("threads must be separated by a blank line:\n%s", out)
	}

	// --json is the flat masked array.
	out = runXfa(t, "board", "--board", "cmdboard", "--json")
	var posts []store.Post
	if err := json.Unmarshal([]byte(out), &posts); err != nil {
		t.Fatalf("bad --json output %q: %v", out, err)
	}
	if len(posts) != 3 || posts[1].Body != "[deleted]" {
		t.Fatalf("--json = %+v; want 3 posts with the reply masked", posts)
	}

	// Empty board: [] under --json (never null), a friendly line otherwise.
	if _, err := s.EnsureBoard("bare", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	out = runXfa(t, "board", "--board", "bare", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty board --json = %q, want []", out)
	}
	out = runXfa(t, "board", "--board", "bare", "--json=false")
	if !strings.Contains(out, "board b/bare is empty") {
		t.Errorf("empty board text = %q", out)
	}
}

// The orphan-becomes-own-root tests moved to internal/store/boardview_test.go
// with the grouping logic itself (ThreadSummaries/GroupThreads promotion).

// Reviewer scenario end to end: offset-bearing TEXT timestamps whose
// lexicographic order inverts chronology (parent +05:00 sorting after its
// reply -04:00) must not make the reply vanish from either view — id-ordered
// BoardPosts keeps parents first naturally.
func TestBoardViewsSurviveOffsetInvertedTimestamps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("offsets", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "offsets-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	parent := mustPost(t, s, b.ID, a.Handle, "offset parent", nil)
	reply := mustPost(t, s, b.ID, a.Handle, "offset reply", &parent.ID)
	// Parent: 18:00 UTC but text "23:..."; reply: 23:00 UTC but text "19:...".
	if err := s.DB.Exec("UPDATE posts SET created_at = '2026-08-19 23:00:00.000+05:00' WHERE id = ?", parent.ID).Error; err != nil {
		t.Fatalf("update parent: %v", err)
	}
	if err := s.DB.Exec("UPDATE posts SET created_at = '2026-08-19 19:00:00.000-04:00' WHERE id = ?", reply.ID).Error; err != nil {
		t.Fatalf("update reply: %v", err)
	}

	out := runXfa(t, "board", "--board", "offsets", "--json=false")
	if !strings.Contains(out, "offset parent") || !strings.Contains(out, "offset reply") {
		t.Fatalf("both posts must stay visible in board:\n%s", out)
	}
	if !strings.Contains(out, "\n  #") {
		t.Errorf("reply must stay attached under its parent:\n%s", out)
	}
	out = runXfa(t, "threads", "--board", "offsets", "--limit", "50", "--json=false")
	if !strings.Contains(out, "offset parent") || !strings.Contains(out, "1 reply,") {
		t.Fatalf("threads must keep the reply counted on its parent:\n%s", out)
	}
}

// --session on `threads` uses participated semantics: alpha's thread matches
// beta's filter because beta replied in it, so beta sees both threads while
// alpha sees only its own. Without the flag both sessions' threads show — the
// unfiltered path is untouched.
func TestThreadsSessionFilter(t *testing.T) {
	seedSessionBoard(t)

	all := runXfa(t, "threads", "--board", "cmdsessions", "--limit", "50", "--session", "", "--json=false")
	if !strings.Contains(all, "alpha thread root") || !strings.Contains(all, "beta thread root") {
		t.Fatalf("no --session must list every thread:\n%s", all)
	}

	onlyAlpha := runXfa(t, "threads", "--board", "cmdsessions", "--limit", "50", "--session", sessAlpha, "--json=false")
	if !strings.Contains(onlyAlpha, "alpha thread root") {
		t.Errorf("alpha's own thread must match its filter:\n%s", onlyAlpha)
	}
	if strings.Contains(onlyAlpha, "beta thread root") {
		t.Errorf("beta's thread must not match alpha's filter:\n%s", onlyAlpha)
	}
	if !strings.Contains(onlyAlpha, "1 reply,") {
		t.Errorf("a matched thread keeps its full reply count:\n%s", onlyAlpha)
	}

	bothForBeta := runXfa(t, "threads", "--board", "cmdsessions", "--limit", "50", "--session", sessBeta, "--json=false")
	if !strings.Contains(bothForBeta, "alpha thread root") || !strings.Contains(bothForBeta, "beta thread root") {
		t.Errorf("beta participated in both threads, so both must show:\n%s", bothForBeta)
	}
}

// A session that never posted matches nothing, and says so instead of
// pretending the board is empty.
func TestThreadsSessionFilterNoMatches(t *testing.T) {
	seedSessionBoard(t)

	out := runXfa(t, "threads", "--board", "cmdsessions", "--limit", "50", "--session", "sess-nobody", "--json=false")
	if !strings.Contains(out, "no threads on b/cmdsessions for session sess-nobody") {
		t.Errorf("want a session-aware empty line, got %q", out)
	}
	out = runXfa(t, "threads", "--board", "cmdsessions", "--limit", "50", "--session", "sess-nobody", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("filtered empty --json = %q, want []", out)
	}
}

// --session on `board` returns matched threads WHOLE: alpha's filter includes
// beta's reply, because the filter selects threads, not posts.
func TestBoardSessionFilterReturnsWholeThread(t *testing.T) {
	seedSessionBoard(t)

	out := runXfa(t, "board", "--board", "cmdsessions", "--session", sessAlpha, "--json=false")
	if !strings.Contains(out, "alpha thread root") {
		t.Fatalf("alpha's thread must match:\n%s", out)
	}
	if !strings.Contains(out, "beta reply in alpha thread") {
		t.Errorf("a matched thread must come back whole, other sessions' replies included:\n%s", out)
	}
	if strings.Contains(out, "beta thread root") {
		t.Errorf("beta's own thread must not match alpha's filter:\n%s", out)
	}
	if !strings.Contains(out, "\n  #") {
		t.Errorf("filtered board output must keep its indentation:\n%s", out)
	}

	all := runXfa(t, "board", "--board", "cmdsessions", "--session", "", "--json=false")
	if !strings.Contains(all, "beta thread root") {
		t.Errorf("no --session must render the whole board:\n%s", all)
	}
}

func TestBoardSessionFilterNoMatches(t *testing.T) {
	seedSessionBoard(t)

	out := runXfa(t, "board", "--board", "cmdsessions", "--session", "sess-nobody", "--json=false")
	if !strings.Contains(out, "no posts on b/cmdsessions for session sess-nobody") {
		t.Errorf("want a session-aware empty line, got %q", out)
	}
	if strings.Contains(out, "is empty") {
		t.Errorf("a filtered board must not claim the board itself is empty: %q", out)
	}
	out = runXfa(t, "board", "--board", "cmdsessions", "--session", "sess-nobody", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("filtered empty --json = %q, want []", out)
	}
}

// The --session argument is echoed back in the filtered empty lines, and it is
// attacker-controllable text (a crafted id copied out of a shared board), so
// it is sanitized on the way out like every other untrusted string.
func TestSessionFilterEmptyLinesSanitizeTheEchoedID(t *testing.T) {
	seedSessionBoard(t)

	for _, tc := range []struct{ name, cmd, want string }{
		{"threads", "threads", "no threads on b/cmdsessions for session nobody"},
		{"board", "board", "no posts on b/cmdsessions for session nobody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{tc.cmd, "--board", "cmdsessions",
				"--session", "\x1b[2Jnobody", "--json=false"}
			if tc.cmd == "threads" {
				args = append(args, "--limit", "50")
			}
			out := runXfa(t, args...)
			if strings.Contains(out, "\x1b") {
				t.Errorf("echoed --session must not carry an escape to the terminal: %q", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("want %q, got %q", tc.want, out)
			}
		})
	}
}
