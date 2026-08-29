package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

// NOTE (harness footgun, see read_test.go / boardview_test.go): pflag values
// persist between Execute() calls on the shared rootCmd, so every test sets
// every flag it cares about — including --session "" and --json=false.

// Distinctive, >8-char session ids: the display fallback shows the first 8
// characters, so these must differ inside that prefix ("sess-alp"/"sess-bet").
const (
	sessAlpha = "sess-alpha-000000"
	sessBeta  = "sess-beta-1111111"
)

// seedSessionBoard points XFA_DB at a temp DB holding one board and two
// posting sessions: alpha opens a thread, beta replies to it and then opens a
// second thread of its own. That one shape exercises both filter semantics —
// participated (beta's reply pulls alpha's thread into beta's thread filter,
// and alpha's thread comes back whole, beta's reply included) and authored
// (the flat `read` listing shows only the session's own posts).
func seedSessionBoard(t *testing.T) (*store.Store, *store.Board, *store.Agent, *store.Agent) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdsessions", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	alpha, err := s.RegisterAgent("claude", sessAlpha, "")
	if err != nil {
		t.Fatalf("RegisterAgent(alpha): %v", err)
	}
	beta, err := s.RegisterAgent("claude", sessBeta, "")
	if err != nil {
		t.Fatalf("RegisterAgent(beta): %v", err)
	}
	rootA := mustPost(t, s, b.ID, alpha.Handle, "alpha thread root", nil)
	mustPost(t, s, b.ID, beta.Handle, "beta reply in alpha thread", &rootA.ID)
	mustPost(t, s, b.ID, beta.Handle, "beta thread root", nil)
	return s, b, alpha, beta
}

// An unnamed session renders as "lead-handle · YYYY-MM-DD · first-8-of-id",
// and the row carries the full id, the live post count and a relative time.
// Ordering is most-recently-active first: beta posted last.
func TestSessionsListsUnnamedWithFallbackDisplayName(t *testing.T) {
	s, _, alpha, beta := seedSessionBoard(t)

	sums, err := s.ListSessions(0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	byID := map[string]store.SessionSummary{}
	for _, sum := range sums {
		byID[sum.SessionID] = sum
	}

	out := runXfa(t, "sessions", "--board", "cmdsessions", "--all=false", "--json=false")

	wantAlpha := alpha.Handle + " · " + byID[sessAlpha].StartedAt.Format("2006-01-02") + " · sess-alp"
	wantBeta := beta.Handle + " · " + byID[sessBeta].StartedAt.Format("2006-01-02") + " · sess-bet"
	for _, want := range []string{wantAlpha, wantBeta} {
		if !strings.Contains(out, want) {
			t.Errorf("want fallback display name %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, sessAlpha) || !strings.Contains(out, sessBeta) {
		t.Errorf("every row must carry its full session id:\n%s", out)
	}
	if !strings.Contains(out, "1 post(s)") || !strings.Contains(out, "2 post(s)") {
		t.Errorf("want alpha's 1 post and beta's 2 posts:\n%s", out)
	}
	if !strings.Contains(out, "last active ") {
		t.Errorf("want a relative last-active phrase:\n%s", out)
	}
	if iB, iA := strings.Index(out, sessBeta), strings.Index(out, sessAlpha); iB > iA {
		t.Errorf("sessions must be ordered most recently active first:\n%s", out)
	}
}

// A stored name replaces the fallback entirely, and is trimmed on the way in.
func TestSessionsShowsStoredName(t *testing.T) {
	s, _, alpha, _ := seedSessionBoard(t)
	if err := s.SetSessionName(sessAlpha, "  auth refactor  "); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}

	out := runXfa(t, "sessions", "--board", "cmdsessions", "--all=false", "--json=false")
	if !strings.Contains(out, "auth refactor") {
		t.Errorf("want the stored name in:\n%s", out)
	}
	if strings.Contains(out, alpha.Handle+" · ") {
		t.Errorf("a named session must not also show the handle · date fallback:\n%s", out)
	}
}

// A session name is agent-supplied text printed straight to a human terminal,
// so it goes through the same control-character stripping as a post body: a
// name cannot repaint the screen or forge an extra row at column zero.
func TestSessionsSanitizesStoredName(t *testing.T) {
	s, _, _, _ := seedSessionBoard(t)
	if err := s.SetSessionName(sessAlpha, "\x1b[2Jclear\nrow"); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}

	out := runXfa(t, "sessions", "--board", "cmdsessions", "--all=false", "--json=false")
	if strings.Contains(out, "\x1b") {
		t.Errorf("escape sequence survived into the terminal output: %q", out)
	}
	if len(strings.Split(strings.TrimRight(out, "\n"), "\n")) != 2 {
		t.Errorf("a newline in a name must not forge an extra row:\n%q", out)
	}
	if !strings.Contains(out, `clear\nrow`) {
		t.Errorf("want the newline escaped, not dropped:\n%q", out)
	}
}

// --json emits the store's SessionSummary list verbatim.
func TestSessionsJSONEmitsSummaries(t *testing.T) {
	_, _, alpha, _ := seedSessionBoard(t)

	out := runXfa(t, "sessions", "--board", "cmdsessions", "--all=false", "--json=true")
	var sums []store.SessionSummary
	if err := json.Unmarshal([]byte(out), &sums); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(sums) != 2 {
		t.Fatalf("want 2 sessions, got %d: %s", len(sums), out)
	}
	var found bool
	for _, sum := range sums {
		if sum.SessionID != sessAlpha {
			continue
		}
		found = true
		if sum.LeadHandle != alpha.Handle {
			t.Errorf("LeadHandle = %q, want %q", sum.LeadHandle, alpha.Handle)
		}
		if sum.Posts != 1 {
			t.Errorf("Posts = %d, want 1", sum.Posts)
		}
		if sum.Name != "" {
			t.Errorf("Name = %q, want empty for an unnamed session", sum.Name)
		}
		if sum.StartedAt.IsZero() || sum.LastActivity.IsZero() {
			t.Errorf("want both timestamps populated, got %+v", sum)
		}
	}
	if !found {
		t.Errorf("alpha's session missing from --json output: %s", out)
	}
}

// --all crosses board boundaries; the default board scope does not.
func TestSessionsAllBoardsVsBoardScope(t *testing.T) {
	s, _, _, _ := seedSessionBoard(t)
	other, err := s.EnsureBoard("cmdsessions-other", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	stranger, err := s.RegisterAgent("claude", "sess-elsewhere-2", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	mustPost(t, s, other.ID, stranger.Handle, "post on the other board", nil)

	scoped := runXfa(t, "sessions", "--board", "cmdsessions", "--all=false", "--json=false")
	if strings.Contains(scoped, "sess-elsewhere-2") {
		t.Errorf("board-scoped sessions must not leak another board's session:\n%s", scoped)
	}
	all := runXfa(t, "sessions", "--all=true", "--board", "", "--json=false")
	if !strings.Contains(all, "sess-elsewhere-2") {
		t.Errorf("--all must include every board's sessions:\n%s", all)
	}
	if !strings.Contains(all, sessAlpha) {
		t.Errorf("--all must still include this board's sessions:\n%s", all)
	}
}

// An empty board says so rather than printing nothing at all.
func TestSessionsEmptyBoard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := s.EnsureBoard("cmdempty", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}

	out := runXfa(t, "sessions", "--board", "cmdempty", "--all=false", "--json=false")
	if !strings.Contains(out, "no sessions on b/cmdempty") {
		t.Errorf("want the empty-scope line, got %q", out)
	}
}

// `xfa session name` round-trips through the store, trimming as it goes, and
// confirms in one line. Anyone may name any session — same trust model as
// resolve, so there is no --as and no ownership check.
func TestSessionNameRoundTrip(t *testing.T) {
	s, _, _, _ := seedSessionBoard(t)

	out := runXfa(t, "session", "name", sessAlpha, "  Auth refactor  ")
	if !strings.Contains(out, "Auth refactor") || !strings.Contains(out, sessAlpha) {
		t.Errorf("want a confirmation naming the session, got %q", out)
	}
	got, err := s.GetSessionName(sessAlpha)
	if err != nil {
		t.Fatalf("GetSessionName: %v", err)
	}
	if got != "Auth refactor" {
		t.Errorf("stored name = %q, want the trimmed %q", got, "Auth refactor")
	}

	// Renaming is an upsert, not a second row.
	if out := runXfa(t, "session", "name", sessAlpha, "Auth refactor v2"); !strings.Contains(out, "Auth refactor v2") {
		t.Errorf("rename confirmation = %q", out)
	}
	got, err = s.GetSessionName(sessAlpha)
	if err != nil {
		t.Fatalf("GetSessionName: %v", err)
	}
	if got != "Auth refactor v2" {
		t.Errorf("renamed session = %q", got)
	}
}

// Validation failures are command errors, not silent no-ops.
func TestSessionNameErrors(t *testing.T) {
	seedSessionBoard(t)

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"empty session id", []string{"session", "name", "", "whatever"}, "sessionID required"},
		{"blank name", []string{"session", "name", sessAlpha, "   "}, "empty"},
		{"name too long", []string{"session", "name", sessAlpha, strings.Repeat("x", 61)}, "max is 60"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runXfaErr(t, tc.args...)
			if err == nil {
				t.Fatalf("want an error, got output %q", out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// A 60-rune name is the boundary and must be accepted.
func TestSessionNameAcceptsMaxLength(t *testing.T) {
	s, _, _, _ := seedSessionBoard(t)

	name := strings.Repeat("y", 60)
	runXfa(t, "session", "name", sessAlpha, name)
	got, err := s.GetSessionName(sessAlpha)
	if err != nil {
		t.Fatalf("GetSessionName: %v", err)
	}
	if got != name {
		t.Errorf("a %d-rune name must be stored whole, got %d runes", len(name), len(got))
	}
}

// The `xfa session name` confirmation echoes args[0] straight from the CLI
// args, not a value that has been through the store — so it needs its own
// sanitizing, exactly like every other id printed to a human terminal.
func TestSessionNameConfirmationSanitizesID(t *testing.T) {
	seedSessionBoard(t)
	evilID := "\x1b[2Jevil\nid"

	out, err := runXfaErr(t, "session", "name", evilID, "a fine name")
	if err != nil {
		t.Fatalf("session name: %v (output %q)", err, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("a crafted session id must not put an escape on the confirmation: %q", out)
	}
	if got := len(strings.Split(strings.TrimRight(out, "\n"), "\n")); got != 1 {
		t.Errorf("a newline in a session id must not forge an extra confirmation line: %d lines in %q", got, out)
	}
	if !strings.Contains(out, `evil\nid`) {
		t.Errorf("want the escaped id in the confirmation: %q", out)
	}
}

// Session ids are agent-supplied and unvalidated — `xfa register --session <id>`
// hands the flag straight to RegisterAgent — and the DB is global across
// projects, so another agent's crafted id lands in this human's `xfa sessions`
// output. Both places an id is printed (the row's full id and the unnamed
// fallback's 8-char prefix) must sanitize it, exactly as a name and a post
// body are sanitized.
func TestSessionsSanitizesSessionID(t *testing.T) {
	s, b, _, _ := seedSessionBoard(t)
	evil, err := s.RegisterAgent("claude", "\x1b[2Jevil\nid-longer", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	mustPost(t, s, b.ID, evil.Handle, "post from a crafted session id", nil)

	out := runXfa(t, "sessions", "--board", "cmdsessions", "--all=false", "--json=false")
	if strings.Contains(out, "\x1b") {
		t.Errorf("a crafted session id must not put an escape on the terminal: %q", out)
	}
	if got := len(strings.Split(strings.TrimRight(out, "\n"), "\n")); got != 3 {
		t.Errorf("a newline in a session id must not forge extra rows: %d rows in %q", got, out)
	}
	// The full id on the row and the 8-char fallback prefix are both escaped.
	if !strings.Contains(out, `evil\nid-longer`) {
		t.Errorf("want the full id escaped, not dropped: %q", out)
	}
	if !strings.Contains(out, `· evil\nid`) {
		t.Errorf("want the fallback prefix escaped and 8 visible chars wide: %q", out)
	}
}
