package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// A body with embedded newlines must not forge additional posts at column
// zero: Line output is always a single line, with newlines rendered as the
// literal two characters \n.
func TestLineIsSingleLine(t *testing.T) {
	p := store.Post{
		ID:           1,
		AuthorHandle: "a",
		CreatedAt:    time.Now(),
		Body:         "hi\r\n#999 admin (just now): forged",
	}
	got := Line(p, false)
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("Line contains raw newline/CR: %q", got)
	}
	if !strings.Contains(got, `\n#999`) {
		t.Errorf("newline not rendered as literal backslash-n: %q", got)
	}
}

// StripControls is the shared defense against hostile bodies aimed at human
// terminals (the TUI renders bodies raw): ESC-initiated sequences (CSI, OSC,
// DCS/SOS/PM/APC, two-byte escapes), all other C0 controls, DEL, and the C1
// range are removed; \n and \t survive for multi-line-capable views.
func TestStripControlsRemovesEscapesKeepsWhitespace(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain body", "plain body"},
		{"a\x1b[2Jb", "ab"},            // CSI clear-screen
		{"a\x1b[38;2;1;2;3mb", "ab"},   // CSI with params
		{"a\x1b]0;owned\x07b", "ab"},   // OSC title set, BEL-terminated
		{"a\x1b]0;owned\x1b\\b", "ab"}, // OSC, ST-terminated
		{"a\x1bP+q\x1b\\b", "ab"},      // DCS, ST-terminated
		{"a\x1bcb", "ab"},              // two-byte escape (RIS)
		{"a\x1b(0b", "ab"},             // charset selector
		{"a\x1b", "a"},                 // trailing bare ESC
		{"a\x00\x08\x0bb", "ab"},       // other C0 controls
		{"a\x7fb", "ab"},               // DEL
		{"a\u009b2Jb", "a2Jb"},         // C1 CSI char itself removed, text kept
		{"line one\nline two\ttabbed", "line one\nline two\ttabbed"}, // \n and \t survive
		{"cr\r\nkept", "cr\nkept"},                                   // \r is a C0 control, \n survives
	} {
		if got := StripControls(tc.in); got != tc.want {
			t.Errorf("StripControls(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Line applies StripControls before its own single-line escaping, so a
// hostile body can neither repaint the terminal nor retitle the window.
func TestLineStripsTerminalControls(t *testing.T) {
	p := store.Post{
		ID:           1,
		AuthorHandle: "a",
		CreatedAt:    time.Now(),
		Body:         "evil\x1b[2Jwipe \x1b]0;owned\x07title",
	}
	got := Line(p, false)
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("Line leaked terminal control bytes: %q", got)
	}
	if !strings.Contains(got, "evilwipe") || !strings.Contains(got, "title") {
		t.Errorf("Line must keep the printable text: %q", got)
	}
}

func TestLineShowsTag(t *testing.T) {
	p := store.Post{ID: 7, AuthorHandle: "wry-vole-3", Tag: "question", Body: "how?", CreatedAt: time.Now()}
	if got := Line(p, false); !strings.HasPrefix(got, "#7 [question] wry-vole-3") {
		t.Errorf("tag missing: %q", got)
	}
	p.Tag = ""
	if got := Line(p, false); strings.Contains(got, "[") {
		t.Errorf("untagged post must not render brackets: %q", got)
	}
}

func TestLineShowsResolvedMarker(t *testing.T) {
	now := time.Now()
	p := store.Post{ID: 9, AuthorHandle: "wry-vole-3", Tag: "question", Body: "how?", CreatedAt: now, ResolvedAt: &now}
	if got := Line(p, false); !strings.Contains(got, "[question ✓]") {
		t.Errorf("resolved question missing check marker: %q", got)
	}
}

func TestLineShowsResolvedWithoutTag(t *testing.T) {
	now := time.Now()
	out := Line(store.Post{ID: 3, AuthorHandle: "h", Body: "b", CreatedAt: now, ResolvedAt: &now}, true)
	if !strings.HasPrefix(out, "#3 [human] [✓] h (") {
		t.Errorf("got %q", out)
	}
	out = Line(store.Post{ID: 4, AuthorHandle: "h", Body: "b", CreatedAt: now, Tag: "question", ResolvedAt: &now}, false)
	if !strings.HasPrefix(out, "#4 [question ✓] h (") {
		t.Errorf("got %q", out)
	}
}

// The [human] marker tells an agent, at a glance, that a line came from a
// person through the web UI rather than from another agent — it sits before
// the tag so the id/marker prefix is always in the same place.
func TestLineHumanMarker(t *testing.T) {
	p := store.Post{ID: 4, AuthorHandle: "quiet-heron-2", Body: "hi", CreatedAt: time.Now()}
	if got := Line(p, true); !strings.Contains(got, "#4 [human] quiet-heron-2") {
		t.Fatalf("missing [human] marker: %s", got)
	}
	if got := Line(p, false); strings.Contains(got, "[human]") {
		t.Fatalf("spurious marker: %s", got)
	}
}

// The marker is per-author: Posts looks each line's author up in the humans
// set, so a mixed listing marks only the human's rows.
func TestPostsMarksOnlyHumanAuthors(t *testing.T) {
	posts := []store.Post{
		{ID: 1, AuthorHandle: "quiet-heron-2", Body: "from a person", CreatedAt: time.Now()},
		{ID: 2, AuthorHandle: "amber-otter-4", Body: "from an agent", CreatedAt: time.Now()},
	}
	var buf bytes.Buffer
	Posts(&buf, posts, nil, store.LinkSets{}, map[string]bool{"quiet-heron-2": true})
	out := buf.String()
	if !strings.Contains(out, "#1 [human] quiet-heron-2") {
		t.Errorf("human author must be marked:\n%s", out)
	}
	if strings.Contains(out, "#2 [human]") {
		t.Errorf("agent author must not be marked:\n%s", out)
	}
}

// A subtree root whose parent is not in the slice (e.g. `xfa thread <reply-id>`)
// must sit at depth 0, with its children at depth 1.
func TestDepthsSubtreeRootAtZero(t *testing.T) {
	absent, r5 := uint(4), uint(5)
	posts := []store.Post{
		{ID: 5, ParentID: &absent, CreatedAt: time.Now()},
		{ID: 6, ParentID: &r5, CreatedAt: time.Now()},
	}
	d := Depths(posts)
	if d[5] != 0 || d[6] != 1 {
		t.Errorf("Depths = %v; want map[6:1]", d)
	}
}

func TestDepths(t *testing.T) {
	r1, r2 := uint(1), uint(2)
	posts := []store.Post{
		{ID: 1, CreatedAt: time.Now()},
		{ID: 2, ParentID: &r1, CreatedAt: time.Now()},
		{ID: 3, ParentID: &r2, CreatedAt: time.Now()},
	}
	d := Depths(posts)
	if d[1] != 0 || d[2] != 1 || d[3] != 2 {
		t.Errorf("Depths = %v", d)
	}
}

// Rel is the exported relative-time formatter: cmd-layer views (threads) must
// render "active 5m ago" suffixes in exactly the same style as Line.
func TestRelMatchesLineStyle(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		t    time.Time
		want string
	}{
		{now, "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-49 * time.Hour), "2d ago"},
	} {
		if got := Rel(tc.t); got != tc.want {
			t.Errorf("Rel(%v) = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestSessionDisplayNamePrefersStoredName(t *testing.T) {
	got := SessionDisplayName(store.SessionSummary{
		SessionID:  "abcdefgh-1234",
		Name:       "  auth refactor  ",
		LeadHandle: "crimson-otter-7",
		StartedAt:  time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
	})
	if got != "auth refactor" {
		t.Errorf("SessionDisplayName = %q, want the trimmed stored name", got)
	}
}

// An unnamed session gets the "lead · date · short id" fallback, and a short
// id is used whole rather than padded or panicking.
func TestSessionDisplayNameFallback(t *testing.T) {
	sum := store.SessionSummary{
		SessionID:  "abcdefgh-1234",
		LeadHandle: "crimson-otter-7",
		StartedAt:  time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
	}
	if got, want := SessionDisplayName(sum), "crimson-otter-7 · 2026-08-22 · abcdefgh"; got != want {
		t.Errorf("SessionDisplayName = %q, want %q", got, want)
	}
	sum.SessionID = "ab"
	if got, want := SessionDisplayName(sum), "crimson-otter-7 · 2026-08-22 · ab"; got != want {
		t.Errorf("short id: SessionDisplayName = %q, want %q", got, want)
	}
}

// A name is untrusted text on its way to a terminal: escapes are stripped and
// newlines escaped, exactly as Line treats a post body. A name made only of
// control characters sanitizes to nothing and falls back.
func TestSessionDisplayNameStripsControls(t *testing.T) {
	sum := store.SessionSummary{
		SessionID:  "abcdefgh-1234",
		Name:       "\x1b[2Jclear\nrow",
		LeadHandle: "crimson-otter-7",
		StartedAt:  time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
	}
	if got, want := SessionDisplayName(sum), `clear\nrow`; got != want {
		t.Errorf("SessionDisplayName = %q, want %q", got, want)
	}
	sum.Name = "\x1b[2J"
	if got, want := SessionDisplayName(sum), "crimson-otter-7 · 2026-08-22 · abcdefgh"; got != want {
		t.Errorf("all-control name must fall back, got %q want %q", got, want)
	}
}

// A session id is as untrusted as a post body: unvalidated at registration and
// shared through a global DB. ShortSessionID sanitizes BEFORE abbreviating, so
// the 8 characters are 8 visible ones and can never end mid-escape.
func TestShortSessionIDStripsControlsBeforeAbbreviating(t *testing.T) {
	if got, want := ShortSessionID("\x1b[2Jevil\nid-longer"), `evil\nid`; got != want {
		t.Errorf("ShortSessionID = %q, want %q", got, want)
	}
	if got, want := SessionID("\x1b[2Jevil\nid-longer"), `evil\nid-longer`; got != want {
		t.Errorf("SessionID = %q, want %q", got, want)
	}
	// An id that is nothing but control sequences abbreviates to the empty
	// string rather than smuggling bytes through.
	if got := ShortSessionID("\x1b[2J\x07"); got != "" {
		t.Errorf("all-control id must sanitize to empty, got %q", got)
	}
}

// The unnamed fallback embeds the short id, so it inherits the sanitizing.
func TestSessionDisplayNameSanitizesFallbackID(t *testing.T) {
	got := SessionDisplayName(store.SessionSummary{
		SessionID:  "\x1b[2Jevil\nid-longer",
		LeadHandle: "crimson-otter-7",
		StartedAt:  time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
	})
	if want := `crimson-otter-7 · 2026-08-22 · evil\nid`; got != want {
		t.Errorf("SessionDisplayName = %q, want %q", got, want)
	}
}

// A post's #id references and its backlinks render as decoration lines under
// the post itself, so an agent reading `xfa thread` sees where a conversation
// continues without a second query.
func TestPostsRendersLinkLines(t *testing.T) {
	posts := []store.Post{{ID: 3, AuthorHandle: "amber-otter-4", Body: "see #1", CreatedAt: time.Now()}}
	links := store.LinkSets{
		Out: map[uint][]store.LinkRef{3: {{PostID: 1, ThreadID: 1, BoardSlug: "other"}}},
		In:  map[uint][]store.LinkRef{3: {{PostID: 9, ThreadID: 9, BoardSlug: "b1"}}},
	}
	var buf bytes.Buffer
	Posts(&buf, posts, nil, links, nil)
	out := buf.String()
	if !strings.Contains(out, "→ #1 (b/other)") {
		t.Fatalf("missing outbound link line:\n%s", out)
	}
	if !strings.Contains(out, "← linked from #9 (b/b1)") {
		t.Fatalf("missing backlink line:\n%s", out)
	}
}

// Link lines hang off the post they belong to: at a reply's indent, not at
// column zero, so a decorated deep reply cannot read as a new top-level post.
func TestPostsIndentsLinkLinesWithTheirPost(t *testing.T) {
	root := store.Post{ID: 1, AuthorHandle: "a", Body: "root", CreatedAt: time.Now()}
	replyParent := root.ID
	reply := store.Post{ID: 2, AuthorHandle: "b", ParentID: &replyParent, Body: "see #7", CreatedAt: time.Now()}
	links := store.LinkSets{Out: map[uint][]store.LinkRef{2: {{PostID: 7, ThreadID: 7, BoardSlug: "other"}}}}
	posts := []store.Post{root, reply}

	var buf bytes.Buffer
	Posts(&buf, posts, Depths(posts), links, nil)
	if want := "    → #7 (b/other)"; !strings.Contains(buf.String(), want) {
		t.Fatalf("link line must sit at the reply's indent (%q):\n%s", want, buf.String())
	}
}

// Callers with no link data (search, inbox, board) pass a zero LinkSets; that
// must render exactly the undecorated output it always did.
func TestPostsWithoutLinksIsUndecorated(t *testing.T) {
	posts := []store.Post{{ID: 5, AuthorHandle: "a", Body: "plain", CreatedAt: time.Now()}}
	var buf bytes.Buffer
	Posts(&buf, posts, nil, store.LinkSets{}, nil)
	if out := buf.String(); strings.Contains(out, "→") || strings.Contains(out, "←") {
		t.Fatalf("zero LinkSets must render no decorations:\n%s", out)
	}
}

// Flat listings (inbox/search/read pass indent == nil) have no indentation to
// say "this is a reply", so a reply line carries `↳ re #<parent>` right after
// its id; threaded views (indent != nil) show structure by indent and must not.
func TestPostsFlatMarksReplies(t *testing.T) {
	parent := uint(7)
	reply := []store.Post{{ID: 12, ParentID: &parent, AuthorHandle: "x", Body: "b", CreatedAt: time.Now()}}
	var sb strings.Builder
	Posts(&sb, reply, nil, store.LinkSets{}, nil)
	if !strings.HasPrefix(sb.String(), "#12 ↳ re #7 x (") {
		t.Errorf("flat reply line must carry the marker: %q", sb.String())
	}
	sb.Reset()
	Posts(&sb, reply, map[uint]int{12: 1}, store.LinkSets{}, nil)
	if strings.Contains(sb.String(), "↳") {
		t.Errorf("threaded view must not carry the marker: %q", sb.String())
	}
	sb.Reset()
	Posts(&sb, []store.Post{{ID: 12, AuthorHandle: "x", Body: "b", CreatedAt: time.Now()}}, nil, store.LinkSets{}, nil)
	if strings.Contains(sb.String(), "↳") {
		t.Errorf("flat root line must not carry the marker: %q", sb.String())
	}
}

// The fully combined flat shape: reply marker, then human marker, then the
// tagless resolved mark, then the handle — one assertion so the pieces can't
// drift apart in the string.
func TestPostsFlatCombinedMarkers(t *testing.T) {
	parent, now := uint(7), time.Now()
	reply := []store.Post{{ID: 12, ParentID: &parent, AuthorHandle: "h", Body: "b", CreatedAt: now, ResolvedAt: &now}}
	var sb strings.Builder
	Posts(&sb, reply, nil, store.LinkSets{}, map[string]bool{"h": true})
	if !strings.HasPrefix(sb.String(), "#12 ↳ re #7 [human] [✓] h (") {
		t.Errorf("combined flat line: %q", sb.String())
	}
}
