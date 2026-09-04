package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

// The first cmd-level test in the project. Two cmd-layer defects (both flag
// handling that no store test could see) shipped because the CLI wiring had no
// coverage at all, so this drives the real cobra command end to end against a
// temp DB rather than testing the RunE closure's helpers in isolation.
//
// Pins: `read --unread --limit 0` must not panic. The unread branch over-fetches
// limit+1 and then slices posts[:limit]; a non-positive limit made the store
// fall back to its own default (20) while the slice bound stayed 0 or negative,
// so posts[:0] silently dropped everything and posts[:-1] panicked outright.
func TestReadUnreadClampsNonPositiveLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdtest", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	author, err := s.RegisterAgent("claude", "author-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	reader, err := s.RegisterAgent("claude", "reader-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, author.Handle, "clamp me please", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	for _, limit := range []string{"0", "-1"} {
		t.Run("limit="+limit, func(t *testing.T) {
			// Drop the cursor row (= the 24h floor, which the post is inside)
			// so each case sees the post; the command marks it read once shown.
			if err := s.DB.Where("handle = ?", reader.Handle).Delete(&store.ReadCursor{}).Error; err != nil {
				t.Fatalf("reset cursor: %v", err)
			}
			out := runXfa(t, "read", "--board", "cmdtest", "--unread",
				"--as", reader.Handle, "--limit", limit)
			if !strings.Contains(out, "clamp me please") {
				t.Fatalf("--limit %s should behave like the default, got %q", limit, out)
			}
		})
	}
}

// runXfa executes the real root command and returns its combined output. A
// failure to execute is fatal; a panic in a RunE surfaces as a test panic,
// which is exactly what this file is here to catch.
func runXfa(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("xfa %v: %v (output %q)", args, err, buf.String())
	}
	return buf.String()
}

// --human narrows the flat listing to posts written by people through the web
// UI (provider=human agents), and the [human] marker rides along on every
// listing so an unfiltered read still tells an agent which lines came from a
// person. The two exclusions exist for the reasons the other read filters have
// them: --unread would mark filtered-out posts read, and --session is a
// separate axis rather than a composable one.
func TestReadHumanFilter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdhumans", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	// The web UI registers its handle with provider=human; the CLI has no flag
	// for that, so the fixture seeds it through the store like the other cmd
	// tests seed their agents.
	human, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		t.Fatalf("RegisterAgent(human): %v", err)
	}
	agent, err := s.RegisterAgent("claude", "humans-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent(claude): %v", err)
	}
	mustPost(t, s, b.ID, agent.Handle, "agent wrote this", nil)
	mustPost(t, s, b.ID, human.Handle, "human wrote this", nil)

	// pflag values persist across Execute() calls on the shared rootCmd, so
	// neither flag this test sets may leak into the read tests after it.
	t.Cleanup(func() {
		_ = readCmd.Flags().Set("human", "false")
		_ = readCmd.Flags().Set("session", "")
	})

	base := []string{"read", "--board", "cmdhumans", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false"}
	run := func(extra ...string) string {
		return runXfa(t, append(append([]string{}, base...), extra...)...)
	}

	filtered := run("--human=true", "--json=false")
	if !strings.Contains(filtered, "human wrote this") {
		t.Errorf("--human must keep the human post:\n%s", filtered)
	}
	if strings.Contains(filtered, "agent wrote this") {
		t.Errorf("--human must drop agent posts:\n%s", filtered)
	}
	if !strings.Contains(filtered, "[human] "+human.Handle) {
		t.Errorf("--human output must carry the [human] marker:\n%s", filtered)
	}

	all := run("--human=false", "--json=false")
	if !strings.Contains(all, "human wrote this") || !strings.Contains(all, "agent wrote this") {
		t.Errorf("unfiltered read must show both posts:\n%s", all)
	}
	if !strings.Contains(all, "[human] "+human.Handle) {
		t.Errorf("unfiltered read must still mark the human post:\n%s", all)
	}
	if strings.Contains(all, "[human] "+agent.Handle) {
		t.Errorf("agent post must not be marked human:\n%s", all)
	}

	jsonRow := run("--human=true", "--json=true")
	if !strings.Contains(jsonRow, `"human":true`) {
		t.Errorf("--human --json must flag the row:\n%s", jsonRow)
	}

	reader, err := s.RegisterAgent("claude", "human-reader-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent(reader): %v", err)
	}
	out, err := runXfaErr(t, "read", "--board", "cmdhumans", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--json=false",
		"--human=true", "--unread", "--as", reader.Handle)
	if err == nil {
		t.Fatalf("--human with --unread must be refused, got %q", out)
	}
	if !strings.Contains(err.Error(), "--unread reads everything new") ||
		!strings.Contains(err.Error(), "choose one") {
		t.Errorf("want the shared conflict wording, got %v", err)
	}

	out, err = runXfaErr(t, "read", "--board", "cmdhumans", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--json=false",
		"--human=true", "--session", "humans-sess")
	if err == nil {
		t.Fatalf("--human with --session must be refused, got %q", out)
	}
	if !strings.Contains(err.Error(), "choose one") {
		t.Errorf("want the shared conflict wording, got %v", err)
	}
}

// A board with no human posts must SAY so. The hook nudge points agents at
// `xfa read --human`, and a silent exit there reads as a broken command rather
// than as an empty queue.
func TestReadHumanEmptyIsExplicit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdnohumans", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	agent, err := s.RegisterAgent("claude", "nohumans-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	mustPost(t, s, b.ID, agent.Handle, "agents only here", nil)
	t.Cleanup(func() { _ = readCmd.Flags().Set("human", "false") })

	out := runXfa(t, "read", "--board", "cmdnohumans", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--human=true", "--json=false")
	if !strings.Contains(out, "no human posts on b/cmdnohumans") {
		t.Errorf("an empty --human read must say so, got %q", out)
	}

	// --json keeps its machine contract: [], never a prose line.
	out = runXfa(t, "read", "--board", "cmdnohumans", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--human=true", "--json=true")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty --human --json = %q, want []", out)
	}
}

// The [human] marker is not read-only: every agent-facing listing that renders
// a human-authored post marks it, and each one's --json carries the matching
// `human` field, so a text reader and a JSON reader never disagree about who
// wrote a post.
func TestHumanMarkerAcrossListings(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdmarkers", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	human, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		t.Fatalf("RegisterAgent(human): %v", err)
	}
	agent, err := s.RegisterAgent("claude", "markers-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent(claude): %v", err)
	}
	// A human-asked question (roots `questions` and `threads`) and an agent
	// thread whose reply mentions the human (roots `inbox`).
	mustPostTagged(t, s, b.ID, human.Handle, "why does the cache miss", "question", nil)
	agentRoot := mustPost(t, s, b.ID, agent.Handle, "agent root post", nil)
	mustPost(t, s, b.ID, human.Handle, "human reply mentioning @"+agent.Handle, &agentRoot.ID)

	// The marker sits before the tag ("#1 [human] [question] handle ..."), so
	// the check is per line, keyed on the author position ("handle (rel):"),
	// rather than on a fixed "[human] handle" substring.
	authoredBy := func(line, handle string) bool {
		return strings.Contains(line, handle+" (")
	}
	assertMarkers := func(t *testing.T, name, out string) {
		t.Helper()
		var sawHuman bool
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			switch {
			case authoredBy(line, human.Handle):
				sawHuman = true
				if !strings.Contains(line, "[human]") {
					t.Errorf("%s must mark the human author: %q", name, line)
				}
			case authoredBy(line, agent.Handle):
				if strings.Contains(line, "[human]") {
					t.Errorf("%s must not mark the agent author: %q", name, line)
				}
			}
		}
		if !sawHuman {
			t.Errorf("%s listed no human-authored row at all:\n%s", name, out)
		}
	}
	for _, tc := range []struct {
		name       string
		text, json []string
	}{
		{
			name: "questions",
			text: []string{"questions", "--board", "cmdmarkers", "--all=false", "--limit", "20"},
			json: []string{"questions", "--board", "cmdmarkers", "--all=false", "--limit", "20"},
		},
		{
			name: "threads",
			text: []string{"threads", "--board", "cmdmarkers", "--session", "", "--limit", "50"},
			json: []string{"threads", "--board", "cmdmarkers", "--session", "", "--limit", "50"},
		},
		{
			name: "search",
			text: []string{"search", "cache", "--board", "cmdmarkers", "--all=false", "--limit", "20"},
			json: []string{"search", "cache", "--board", "cmdmarkers", "--all=false", "--limit", "20"},
		},
		{
			name: "inbox",
			text: []string{"inbox", "--as", agent.Handle, "--limit", "20"},
			json: []string{"inbox", "--as", agent.Handle, "--limit", "20"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := runXfa(t, append(append([]string{}, tc.text...), "--json=false")...)
			assertMarkers(t, tc.name, out)
			out = runXfa(t, append(append([]string{}, tc.json...), "--json=true")...)
			if !strings.Contains(out, `"human":true`) {
				t.Errorf("%s --json must carry the human field:\n%s", tc.name, out)
			}
		})
	}

	// omitempty: an agent-only listing keeps exactly the wire shape it had.
	out := runXfa(t, "threads", "--board", "cmdmarkers", "--session", "markers-sess",
		"--limit", "50", "--json=true")
	if strings.Contains(out, `"human"`) {
		t.Errorf("agent-only rows must omit the human key:\n%s", out)
	}
}

// --session on the flat `read` listing is authored-by, not participated-in:
// it shows the session's own posts, wherever in a thread they sit.
func TestReadSessionFilter(t *testing.T) {
	seedSessionBoard(t)

	all := runXfa(t, "read", "--board", "cmdsessions", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--json=false")
	if !strings.Contains(all, "alpha thread root") || !strings.Contains(all, "beta thread root") {
		t.Fatalf("no --session must read the whole board:\n%s", all)
	}

	beta := runXfa(t, "read", "--board", "cmdsessions", "--session", sessBeta, "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--json=false")
	if !strings.Contains(beta, "beta thread root") || !strings.Contains(beta, "beta reply in alpha thread") {
		t.Errorf("beta's own posts (root and reply) must both show:\n%s", beta)
	}
	if strings.Contains(beta, "alpha thread root") {
		t.Errorf("alpha's post must not show under beta's filter:\n%s", beta)
	}
}

// --session composes with the other read filters rather than replacing them.
func TestReadSessionFilterComposesWithTagAndLimit(t *testing.T) {
	s, b, _, beta := seedSessionBoard(t)
	mustPostTagged(t, s, b.ID, beta.Handle, "beta asks something", "question", nil)

	tagged := runXfa(t, "read", "--board", "cmdsessions", "--session", sessBeta, "--tag", "question",
		"--since", "", "--limit", "20", "--unread=false", "--json=false")
	if !strings.Contains(tagged, "beta asks something") {
		t.Errorf("--session + --tag must keep the tagged post:\n%s", tagged)
	}
	if strings.Contains(tagged, "beta thread root") {
		t.Errorf("--tag must still filter within the session:\n%s", tagged)
	}

	// Newest-first, so --limit 1 keeps the question and drops the older posts.
	limited := runXfa(t, "read", "--board", "cmdsessions", "--session", sessBeta, "--tag", "",
		"--since", "", "--limit", "1", "--unread=false", "--json=false")
	if strings.Count(strings.TrimRight(limited, "\n"), "\n") != 0 {
		t.Errorf("--limit 1 must yield exactly one line:\n%s", limited)
	}
	if !strings.Contains(limited, "beta asks something") {
		t.Errorf("--limit must keep the newest post:\n%s", limited)
	}

	// A filter that matches nothing is empty, never "everything".
	none := runXfa(t, "read", "--board", "cmdsessions", "--session", "sess-nobody", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--json=false")
	if strings.TrimSpace(none) != "" {
		t.Errorf("an unknown session must match nothing, got:\n%s", none)
	}
}

// --unread and --session are mutually exclusive, for the same reason --tag
// already is: the read cursor is per-(handle, board), never per-session, so a
// session-filtered catch-up would consume — and mark read — the very posts it
// hid from you. The refusal is loud rather than silent.
func TestReadUnreadRefusesConflictingFilters(t *testing.T) {
	s, b, _, _ := seedSessionBoard(t)
	reader, err := s.RegisterAgent("claude", "guard-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	for _, tc := range []struct{ name, flag, value string }{
		{"tag", "--tag", "question"},
		{"since", "--since", "24h"},
		{"session", "--session", sessAlpha},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"read", "--board", "cmdsessions",
				"--session", "", "--tag", "", "--since", "", "--limit", "20",
				"--unread", "--as", reader.Handle, "--json=false",
				tc.flag, tc.value}
			out, err := runXfaErr(t, args...)
			if err == nil {
				t.Fatalf("--unread with %s must be refused, got %q", tc.flag, out)
			}
			if !strings.Contains(err.Error(), tc.flag) ||
				!strings.Contains(err.Error(), "choose one") {
				t.Errorf("want the shared conflict wording naming %s, got %v", tc.flag, err)
			}
		})
	}

	// The refusal must come BEFORE any read: the cursor stays exactly where it
	// was, so a refused command never silently marks anything read.
	before, err := s.UnreadCountFor(b.ID, reader.Handle)
	if err != nil {
		t.Fatalf("UnreadCountFor: %v", err)
	}
	if _, err := runXfaErr(t, "read", "--board", "cmdsessions", "--session", sessAlpha,
		"--tag", "", "--since", "", "--limit", "20", "--unread",
		"--as", reader.Handle, "--json=false"); err == nil {
		t.Fatal("expected the --session refusal")
	}
	after, err := s.UnreadCountFor(b.ID, reader.Handle)
	if err != nil {
		t.Fatalf("UnreadCountFor: %v", err)
	}
	if before != after || after == 0 {
		t.Errorf("a refused --unread must not touch the cursor: %d unread before, %d after", before, after)
	}
}

// Plain --session (no --unread) is untouched by the guard: it is a pure read
// with no cursor involvement at all.
func TestReadSessionWithoutUnreadIsAllowed(t *testing.T) {
	seedSessionBoard(t)

	out := runXfa(t, "read", "--board", "cmdsessions", "--session", sessAlpha,
		"--tag", "", "--since", "", "--limit", "20", "--unread=false", "--json=false")
	if !strings.Contains(out, "alpha thread root") {
		t.Errorf("--session without --unread must still read:\n%s", out)
	}
}

// `read --session --json` on a session that matches nothing must encode an
// empty array, not the bare word `null` — the same [] contract as board and
// sessions --json, so a caller can always json.Unmarshal into a slice type.
func TestReadSessionFilterJSONEmptyIsArrayNotNull(t *testing.T) {
	seedSessionBoard(t)

	out := runXfa(t, "read", "--board", "cmdsessions", "--session", "sess-nobody",
		"--tag", "", "--since", "", "--limit", "20", "--unread=false", "--json=true")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("filtered empty --json = %q, want []", out)
	}
}

// --session is trimmed, so whitespace-only is treated as no filter: it reads
// the whole board rather than matching nothing, and — the case that matters
// most — it does not trip the --unread refusal, since the flag is
// effectively unset.
func TestReadSessionFlagTrimsWhitespace(t *testing.T) {
	s, _, _, _ := seedSessionBoard(t)
	reader, err := s.RegisterAgent("claude", "trim-reader-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	out := runXfa(t, "read", "--board", "cmdsessions", "--session", "   ",
		"--tag", "", "--since", "", "--limit", "20", "--unread=false", "--json=false")
	if !strings.Contains(out, "alpha thread root") || !strings.Contains(out, "beta thread root") {
		t.Errorf("whitespace-only --session must behave like no filter:\n%s", out)
	}

	if out, err := runXfaErr(t, "read", "--board", "cmdsessions", "--session", "   ",
		"--tag", "", "--since", "", "--limit", "20", "--unread",
		"--as", reader.Handle, "--json=false"); err != nil {
		t.Errorf("whitespace-only --session must not trip the --unread refusal: %v (output %q)", err, out)
	}
}

// Pins the id cursor end to end: a truncated catch-up advances the cursor to
// the last post actually shown, so paging is exact — no re-display, no loss —
// and the third run is caught up.
func TestReadUnreadPagesByID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, _ := s.EnsureBoard("cmdpage", "")
	author, _ := s.RegisterAgent("claude", "author-sess", "")
	reader, _ := s.RegisterAgent("claude", "reader-sess", "")
	var ids []uint
	for _, body := range []string{"one", "two", "three"} {
		p, err := s.CreatePost(b.ID, author.Handle, body, "", nil)
		if err != nil {
			t.Fatalf("CreatePost: %v", err)
		}
		ids = append(ids, p.ID)
	}
	args := []string{"read", "--board", "cmdpage", "--session", "", "--tag", "", "--since", "",
		"--unread", "--as", reader.Handle, "--json=false", "--limit", "2"}

	out := runXfa(t, args...)
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") || strings.Contains(out, "three") ||
		!strings.Contains(out, "more unread") {
		t.Fatalf("run 1 should show the first two and say more:\n%s", out)
	}
	out = runXfa(t, args...)
	if strings.Contains(out, "two") || !strings.Contains(out, "three") || strings.Contains(out, "more unread") {
		t.Fatalf("run 2 should show exactly the third:\n%s", out)
	}
	out = runXfa(t, args...)
	if !strings.Contains(out, "all caught up") {
		t.Fatalf("run 3 should be caught up:\n%s", out)
	}
	if n, _ := s.UnreadCountFor(b.ID, reader.Handle); n != 0 {
		t.Fatalf("UnreadCountFor after catch-up = %d, want 0 (cursor at #%d)", n, ids[2])
	}
}

// A positional board slug (`xfa read b/api`) used to be silently ignored on the
// flag-less commands — exit 0 against the cwd board — and to hit cobra's
// "unknown command" on the rest. Every listing command must reject it, and the
// ones that have --board must say so.
func TestListingCommandsRejectPositionalBoard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureBoard("api", ""); err != nil {
		t.Fatal(err)
	}

	// init/uninstall must reject the positional before RunE ever runs (their
	// Args validator fires first), so a safety-net cwd guards against a broken
	// validator actually running init's side effects (creating .xfa/) here.
	cwd := t.TempDir()
	t.Chdir(cwd)

	for _, c := range []string{"read", "threads", "board", "questions", "sessions", "stats", "init"} {
		_, err := runXfaErr(t, c, "b/api")
		if err == nil || !strings.Contains(err.Error(), "--board b/api") {
			t.Errorf("xfa %s b/api: want a --board hint error, got %v", c, err)
		}
	}
	for _, c := range []string{"inbox", "boards", "register", "uninstall"} {
		_, err := runXfaErr(t, c, "b/api")
		if err == nil || !strings.Contains(err.Error(), "no positional") || strings.Contains(err.Error(), "did you mean") {
			t.Errorf("xfa %s b/api: want positional rejection, got %v", c, err)
		}
	}

	if _, err := os.Stat(filepath.Join(cwd, store.LocalDirName)); !os.IsNotExist(err) {
		t.Fatalf("init must not have run RunE (found %s), stat err = %v", store.LocalDirName, err)
	}
}
