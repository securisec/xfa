package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/securisec/xfa/internal/store"
)

// Ascii profile: View() output carries no ANSI escapes, so substring
// assertions see the plain text.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

// seedTUI builds a real store on a temp DB (no mocks): board "tuiboard" with
// a resolved multi-line question thread (reply + tombstoned reply) and a
// second, empty board for the picker.
func seedTUI(t *testing.T) (*store.Store, *store.Board) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath) // hygiene: nothing may touch the real DB
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("tuiboard", "the tui board")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	if _, err := s.EnsureBoard("otherboard", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "tui-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	root, err := s.CreatePost(b.ID, a.Handle, "does WAL survive rsync?\nasking because backups", "question", nil)
	if err != nil {
		t.Fatalf("CreatePost root: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "yes, if the sidecars come too", "", &root.ID); err != nil {
		t.Fatalf("CreatePost reply: %v", err)
	}
	doomed, err := s.CreatePost(b.ID, a.Handle, "secret hot take", "", &root.ID)
	if err != nil {
		t.Fatalf("CreatePost doomed: %v", err)
	}
	if err := s.Tombstone(doomed.ID, a.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}
	if err := s.Resolve(root.ID, a.Handle); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return s, b
}

// drive feeds msgs through Update, returning the evolved model.
func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

// press sends one key and returns the new model plus any command.
func press(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

// runCmd executes a data command and feeds its message back through Update —
// what the bubbletea runtime would do.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return drive(t, m, cmd())
}

// loaded builds a model on the seeded board, sized, with init data applied.
func loaded(t *testing.T, s *store.Store, b *store.Board) Model {
	t.Helper()
	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return runCmd(t, m, m.Init())
}

func TestInitLoadsThreadListWithBadgesAndCounts(t *testing.T) {
	s, b := seedTUI(t)
	m := loaded(t, s, b)
	if m.view != viewThreadList {
		t.Fatalf("view = %d, want threadList after init load", m.view)
	}
	out := m.View()
	for _, want := range []string{
		"b/tuiboard",
		"[question]",              // tag badge
		"✓",                       // resolved marker
		"does WAL survive rsync?", // root body first line only
		"2 replies",               // whole-subtree count, incl. the tombstone
		"active ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("thread list must contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "asking because backups") {
		t.Errorf("list rows must show only the body's first line:\n%s", out)
	}
}

func TestEnterOpensDetailWithRawBodiesAndEscReturns(t *testing.T) {
	s, b := seedTUI(t)
	m := loaded(t, s, b)

	m, _ = press(t, m, "enter")
	if m.view != viewThreadDetail {
		t.Fatalf("enter must open the thread detail, view = %d", m.view)
	}
	out := m.View()
	for _, want := range []string{
		"does WAL survive rsync?",
		"asking because backups", // second body line: raw newlines allowed HERE
		"yes, if the sidecars come too",
		"[deleted]", // tombstoned reply masked, never filtered
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail must contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret hot take") {
		t.Errorf("tombstoned body leaked:\n%s", out)
	}

	m, _ = press(t, m, "esc")
	if m.view != viewThreadList {
		t.Fatalf("esc must return to the thread list, view = %d", m.view)
	}
}

// TestThreadViewShowsLinkLines seeds a target thread and a source thread
// whose root references the target via #id, then checks the detail viewport
// renders an outbound line on the source thread and an inbound line on the
// target thread.
func TestThreadViewShowsLinkLines(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-2", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	target, err := s.CreatePost(b.ID, a.Handle, "a standalone finding worth citing", "", nil)
	if err != nil {
		t.Fatalf("CreatePost target: %v", err)
	}
	src, err := s.CreatePost(b.ID, a.Handle, fmt.Sprintf("see #%d for context", target.ID), "", nil)
	if err != nil {
		t.Fatalf("CreatePost src: %v", err)
	}

	m := loaded(t, s, b)

	findCursor := func(root uint) int {
		for i, sum := range m.summaries {
			if sum.Root.ID == root {
				return i
			}
		}
		t.Fatalf("thread rooted at #%d not found in summaries", root)
		return -1
	}

	// open the target's thread: it is the linked-to post, so the detail view
	// must show an inbound line.
	m.cursor = findCursor(target.ID)
	m, _ = press(t, m, "enter")
	out := m.View()
	if !strings.Contains(out, "← linked from #") {
		t.Errorf("target thread must show an inbound link line:\n%s", out)
	}
	m, _ = press(t, m, "esc")

	// open the source's thread: its root references #target, so the detail
	// view must show an outbound line.
	m.cursor = findCursor(src.ID)
	m, _ = press(t, m, "enter")
	out = m.View()
	if !strings.Contains(out, "→ #") {
		t.Errorf("source thread must show an outbound link line:\n%s", out)
	}
}

func TestBoardPickReachableAndOpensBoards(t *testing.T) {
	s, b := seedTUI(t)
	m := loaded(t, s, b)

	m, cmd := press(t, m, "b")
	if m.view != viewBoardPick {
		t.Fatalf("b must switch to the board picker, view = %d", m.view)
	}
	m = runCmd(t, m, cmd)
	out := m.View()
	for _, want := range []string{"b/tuiboard", "(3 posts)", "b/otherboard", "(0 posts)"} {
		if !strings.Contains(out, want) {
			t.Errorf("board picker must list boards with post counts, missing %q:\n%s", want, out)
		}
	}

	// esc from the thread list also lands on the picker
	m2 := loaded(t, s, b)
	m2, _ = press(t, m2, "esc")
	if m2.view != viewBoardPick {
		t.Errorf("esc from thread list must open the picker, view = %d", m2.view)
	}

	// enter on the selected board loads its threads (slug order puts
	// b/otherboard first; j selects b/tuiboard)
	m, _ = press(t, m, "j")
	m, cmd = press(t, m, "enter")
	m = runCmd(t, m, cmd)
	if m.view != viewThreadList {
		t.Fatalf("enter on a board must open its thread list, view = %d", m.view)
	}
	if out := m.View(); !strings.Contains(out, "does WAL survive rsync?") {
		t.Errorf("opened board must show its threads:\n%s", out)
	}
}

// Reviewer scenario (I1): scroll deep in a big board, switch to a smaller
// board via the picker — the new board must not inherit the stale scroll
// offset (which would render a blank list under a "1 thread" header).
func TestBoardSwitchResetsScrollOffset(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-scroll", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := s.CreatePost(b.ID, a.Handle, fmt.Sprintf("filler thread %d", i), "", nil); err != nil {
			t.Fatalf("CreatePost filler: %v", err)
		}
	}
	other, err := s.GetBoardBySlug("otherboard")
	if err != nil {
		t.Fatalf("GetBoardBySlug: %v", err)
	}
	if _, err := s.CreatePost(other.ID, a.Handle, "the only thread here", "", nil); err != nil {
		t.Fatalf("CreatePost other: %v", err)
	}

	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 8}) // 5 content rows
	m = runCmd(t, m, m.Init())
	for i := 0; i < 10; i++ { // scroll well past the first window
		m, _ = press(t, m, "j")
	}
	if m.offset == 0 {
		t.Fatalf("test premise broken: scrolling must advance the offset")
	}

	m, cmd := press(t, m, "b")
	m = runCmd(t, m, cmd) // boardsMsg
	m, cmd = press(t, m, "enter")
	m = runCmd(t, m, cmd) // boardMsg for b/otherboard (first in slug order)
	if m.view != viewThreadList || m.board == nil || m.board.Slug != "otherboard" {
		t.Fatalf("expected b/otherboard's thread list, got view=%d board=%+v", m.view, m.board)
	}
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("board switch must reset cursor/offset, got cursor=%d offset=%d", m.cursor, m.offset)
	}
	out := m.View()
	if !strings.Contains(out, "the only thread here") {
		t.Errorf("the new board's thread must be visible, not scrolled out:\n%s", out)
	}
	if !strings.Contains(out, "> ") {
		t.Errorf("the selection marker must be visible on the new board:\n%s", out)
	}
}

// Same-board refresh keeps scroll state but clamps it to the fresh list.
func TestSameBoardRefreshClampsButKeepsScroll(t *testing.T) {
	s, b := seedTUI(t)
	m := loaded(t, s, b)
	m.offset = 99 // simulate a stale offset beyond the list
	m.cursor = 99
	m, cmd := press(t, m, "r")
	m = runCmd(t, m, cmd)
	if m.cursor != len(m.summaries)-1 {
		t.Errorf("refresh must clamp the cursor, got %d", m.cursor)
	}
	if maxOff := max(0, len(m.summaries)-m.contentHeight()); m.offset > maxOff {
		t.Errorf("refresh must clamp the offset, got %d (max %d)", m.offset, maxOff)
	}
}

// Ruled-in minor: esc at the board picker is a no-op — bubbles' default list
// keymap would otherwise quit the whole program.
func TestEscAtBoardPickerIsNoOp(t *testing.T) {
	s, _ := seedTUI(t)
	m := New(s, nil)
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = runCmd(t, m, m.Init())
	m, cmd := press(t, m, "esc")
	if m.view != viewBoardPick {
		t.Errorf("esc at the picker must stay on the picker, view = %d", m.view)
	}
	if cmd != nil {
		if _, quits := cmd().(tea.QuitMsg); quits {
			t.Error("esc at the picker must not quit the program")
		}
	}
}

// I3: hostile bodies aimed at the terminal (clear-screen CSI, title-setting
// OSC) are stripped in both the list row and the detail view; legitimate
// newlines and tabs survive in the detail view only.
func TestControlSequencesStrippedFromListAndDetail(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-evil", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	body := "evil\x1b[2Jwipe \x1b]0;owned\x07title\nsecond\tline"
	if _, err := s.CreatePost(b.ID, a.Handle, body, "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	m := loaded(t, s, b) // hostile post is newest -> cursor 0

	out := m.View()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("thread list leaked terminal control bytes:\n%q", out)
	}
	if !strings.Contains(out, "evilwipe title") {
		t.Errorf("list row must keep the printable first line:\n%s", out)
	}
	if strings.Contains(out, "second") {
		t.Errorf("list row must stay first-line-only:\n%s", out)
	}

	m, _ = press(t, m, "enter")
	out = m.View()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("detail view leaked terminal control bytes:\n%q", out)
	}
	// the real newline survives: both body lines are visible in the detail
	// view (the viewport expands the surviving tab to spaces at render time)
	if !strings.Contains(out, "evilwipe title") || !strings.Contains(out, "second") {
		t.Errorf("detail must keep printable text across real newlines:\n%s", out)
	}
	// the tab survives StripControls (asserted directly in render's tests);
	// lipgloss then renders it as its 4-space tab expansion, so the detail
	// keeps the tab's whitespace rather than dropping it
	content := m.threadContent(m.groups[m.detailRoot])
	if !strings.Contains(content, "second    line") {
		t.Errorf("the tab's whitespace must survive into the detail content:\n%q", content)
	}
	if !strings.Contains(content, "evilwipe title\n") {
		t.Errorf("the newline must survive as a real newline in the detail content:\n%q", content)
	}
}

// maxLineWidth returns the widest printable line in a View() (ANSI-ignored,
// grapheme-aware via x/ansi).
func maxLineWidth(s string) int {
	w := 0
	for _, line := range strings.Split(s, "\n") {
		if lw := ansi.StringWidth(line); lw > w {
			w = lw
		}
	}
	return w
}

// User bug report: TUI lines overflowed the terminal. The detail view must
// word-wrap bodies to the width (data stays readable), with continuation
// lines holding the post's indent.
func TestDetailWrapsNarrowWidth(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-wrap", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	body := "alpha bravo charlie delta echo foxtrot golf hotel\n\nindia juliet kilo lima mike november oscar papa"
	root, err := s.CreatePost(b.ID, a.Handle, body, "", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "indented reply that is also quite long and must wrap", "", &root.ID); err != nil {
		t.Fatalf("CreatePost reply: %v", err)
	}

	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 30, Height: 24})
	m = runCmd(t, m, m.Init())
	m, _ = press(t, m, "enter") // newest thread = the long post
	if m.view != viewThreadDetail {
		t.Fatalf("expected detail view, got %d", m.view)
	}
	out := m.View()
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("detail view overflows: widest line %d > 30:\n%s", w, out)
	}
	for _, word := range strings.Fields("alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa") {
		if !strings.Contains(out, word) {
			t.Errorf("wrapped detail must keep the full body, missing %q:\n%s", word, out)
		}
	}
	// continuation lines of the depth-1 reply keep its 2-space indent
	content := m.threadContent(m.groups[m.detailRoot])
	var replyLines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "indented reply") || strings.Contains(line, "must wrap") {
			replyLines = append(replyLines, line)
		}
	}
	if len(replyLines) < 2 {
		t.Fatalf("long reply must wrap onto multiple lines, got %q", replyLines)
	}
	for _, line := range replyLines {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("wrapped reply line lost its indent: %q", line)
		}
	}
}

// Thread-list rows stay single-line: truncated to the width with an ellipsis,
// the full content readable by pressing enter. Help bar and header too.
func TestListRowTruncatesWithEllipsis(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-trunc", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "start "+strings.Repeat("mid ", 20)+"endword", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if _, err := s.EnsureBoard("longdesc", strings.Repeat("a very long board description ", 4)); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}

	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 30, Height: 24})
	m = runCmd(t, m, m.Init())
	out := m.View()
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("thread list overflows: widest line %d > 30:\n%s", w, out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("long row must end in an ellipsis:\n%s", out)
	}
	if strings.Contains(out, "endword") {
		t.Errorf("truncated row must cut the overflow, not wrap it:\n%s", out)
	}
	// the full body is still reachable: enter shows it wrapped
	m, _ = press(t, m, "enter")
	if out := m.View(); !strings.Contains(out, "endword") {
		t.Errorf("full content must be readable in the detail view:\n%s", out)
	}

	// board picker rows truncate too (bubbles' delegate + our chrome)
	m, cmd := press(t, m, "b")
	m = runCmd(t, m, cmd)
	out = m.View()
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("board picker overflows: widest line %d > 30:\n%s", w, out)
	}
}

// rowFor picks the single View() line belonging to post id out of the render,
// so assertions can target one row instead of the whole screen.
func rowFor(t *testing.T, view string, id uint) string {
	t.Helper()
	want := fmt.Sprintf("#%d ", id)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no row for post %s in:\n%s", want, view)
	return ""
}

// The reply count is why a collapsed row exists — it must survive a body
// long enough to reach the terminal edge: the BODY gets the ellipsis, not the
// trailing meta.
func TestListRowKeepsReplyCountWhenBodyOverflows(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-meta", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	root, err := s.CreatePost(b.ID, a.Handle, strings.Repeat("overlong ", 25), "", nil)
	if err != nil {
		t.Fatalf("CreatePost root: %v", err)
	}
	// 3 replies, so the count cannot be confused with the seeded thread's 2
	for i := 0; i < 3; i++ {
		if _, err := s.CreatePost(b.ID, a.Handle, fmt.Sprintf("reply %d", i), "", &root.ID); err != nil {
			t.Fatalf("CreatePost reply: %v", err)
		}
	}

	// width 70, not 60: handles are minted 10-19 chars, and 60 leaves the body
	// as little as 4 columns — enough to pass, too tight to be a stable premise
	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})
	m = runCmd(t, m, m.Init())
	out := m.View()
	if w := maxLineWidth(out); w > 70 {
		t.Errorf("thread list overflows: widest line %d > 70:\n%s", w, out)
	}
	// assert on THIS row, not the whole view: the seeded question row also
	// truncates here, so a view-wide ellipsis check would prove nothing
	row := rowFor(t, out, root.ID)
	if !strings.Contains(row, "3 replies") {
		t.Errorf("overflowing row must keep its reply count: %q", row)
	}
	cut, meta := strings.Index(row, "…"), strings.Index(row, "· 3 replies")
	if cut < 0 {
		t.Errorf("overflowing row must be truncated somewhere: %q", row)
	} else if meta < 0 || cut > meta {
		t.Errorf("the body, not the meta, must carry the ellipsis: %q", row)
	}

	// a short body still renders body then meta, untouched — on a width-100
	// model, so no handle length can make "short one" legitimately truncate
	wide := drive(t, New(s, b), tea.WindowSizeMsg{Width: 100, Height: 24})
	root.Body = "short one"
	short := wide.threadRow(store.ThreadSummary{Root: *root, Replies: 3, LastActivity: root.CreatedAt}, false)
	if !strings.Contains(short, "short one") || strings.Contains(short, "…") {
		t.Errorf("short row must render whole, unellipsised: %q", short)
	}
	if !strings.Contains(short, "3 replies") {
		t.Errorf("short row lost its meta: %q", short)
	}

	// width 0 (no WindowSizeMsg yet): nothing is truncated
	root.Body = strings.Repeat("overlong ", 25)
	unsized := New(s, b).threadRow(store.ThreadSummary{Root: *root, Replies: 3, LastActivity: root.CreatedAt}, false)
	if strings.Contains(unsized, "…") || !strings.Contains(unsized, "3 replies") {
		t.Errorf("unsized model must not truncate: %q", unsized)
	}
}

// Resizing re-wraps the open detail: a body wrapped at 30 joins back into
// one line at 80.
func TestResizeRewrapsDetail(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-rewrap", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	phrase := "alpha bravo charlie delta echo foxtrot"
	if _, err := s.CreatePost(b.ID, a.Handle, phrase, "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 30, Height: 24})
	m = runCmd(t, m, m.Init())
	m, _ = press(t, m, "enter")
	if out := m.View(); strings.Contains(out, phrase) {
		t.Fatalf("test premise broken: %q must be wrapped at width 30:\n%s", phrase, out)
	}
	m = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	out := m.View()
	if !strings.Contains(out, phrase) {
		t.Errorf("resize wider must re-wrap the open detail onto one line:\n%s", out)
	}
	if w := maxLineWidth(out); w > 80 {
		t.Errorf("detail view overflows after resize: widest line %d > 80", w)
	}
}

func TestNilInitialBoardStartsOnPicker(t *testing.T) {
	s, _ := seedTUI(t)
	m := New(s, nil)
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.view != viewBoardPick {
		t.Fatalf("no initial board must start on the picker, view = %d", m.view)
	}
	m = runCmd(t, m, m.Init())
	if out := m.View(); !strings.Contains(out, "b/tuiboard") {
		t.Errorf("init must load the board list:\n%s", out)
	}
}

// seedSessions adds two more sessions to the seeded board: one named (the
// picker shows the stored name) and one left unnamed (the picker falls back to
// "lead · date · short-id"). Returns the two lead handles.
func seedSessions(t *testing.T, s *store.Store, b *store.Board) (namedLead, unnamedLead string) {
	t.Helper()
	a1, err := s.RegisterAgent("claude", "sess-alpha", "")
	if err != nil {
		t.Fatalf("RegisterAgent alpha: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a1.Handle, "alpha thread body", "", nil); err != nil {
		t.Fatalf("CreatePost alpha: %v", err)
	}
	if err := s.SetSessionName("sess-alpha", "vault refactor"); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}
	a2, err := s.RegisterAgent("claude", "sess-beta", "")
	if err != nil {
		t.Fatalf("RegisterAgent beta: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a2.Handle, "beta thread body", "", nil); err != nil {
		t.Fatalf("CreatePost beta: %v", err)
	}
	return a1.Handle, a2.Handle
}

// openSessions presses s on the thread list and runs the resulting load.
func openSessions(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := press(t, m, "s")
	if m.view != viewSessionPick {
		t.Fatalf("s must open the session picker, view = %d", m.view)
	}
	return runCmd(t, m, cmd)
}

// pickSession moves the open picker's selection onto sessionID's row (empty
// sessionID = the "All sessions" row), presses enter and runs the load.
func pickSession(t *testing.T, m Model, sessionID string) Model {
	t.Helper()
	want := -1
	for i, it := range m.sessionList.Items() {
		si, ok := it.(sessionItem)
		if !ok {
			continue
		}
		if (sessionID == "" && si.all) || (!si.all && si.sum.SessionID == sessionID) {
			want = i
			break
		}
	}
	if want < 0 {
		t.Fatalf("no picker row for session %q", sessionID)
	}
	for m.sessionList.Index() < want {
		m, _ = press(t, m, "j")
	}
	for m.sessionList.Index() > want {
		m, _ = press(t, m, "k")
	}
	m, cmd := press(t, m, "enter")
	if m.view != viewThreadList {
		t.Fatalf("enter in the picker must return to the thread list, view = %d", m.view)
	}
	return runCmd(t, m, cmd)
}

// The picker lists every session on the board plus an always-present
// "All sessions" row; named sessions show their name, unnamed ones the
// lead·date·short-id fallback, both annotated with a live post count.
func TestSessionPickerListsSessionsWithCountsAndAllRow(t *testing.T) {
	s, b := seedTUI(t)
	_, unnamedLead := seedSessions(t, s, b)
	m := openSessions(t, loaded(t, s, b))

	out := m.View()
	for _, want := range []string{
		"All sessions",
		"vault refactor", // stored name
		"(1 post)",       // that session's live post count
		unnamedLead,      // unnamed fallback carries the lead handle
		"sess-bet",       // ...and the short session id
	} {
		if !strings.Contains(out, want) {
			t.Errorf("session picker must contain %q:\n%s", want, out)
		}
	}
	// the seeded board's own session (2 live posts, 1 tombstoned) is listed too
	if !strings.Contains(out, "(2 posts)") {
		t.Errorf("picker must count only live posts:\n%s", out)
	}
}

// Selecting a session reloads the thread list through BoardPostsBySession and
// shows a filter indicator in the header.
func TestSelectSessionFiltersThreadListWithIndicator(t *testing.T) {
	s, b := seedTUI(t)
	seedSessions(t, s, b)
	m := loaded(t, s, b)
	if len(m.summaries) != 3 {
		t.Fatalf("test premise broken: want 3 unfiltered threads, got %d", len(m.summaries))
	}

	m = pickSession(t, openSessions(t, m), "sess-alpha")
	if m.session != "sess-alpha" {
		t.Fatalf("selecting must record the filter, got %q", m.session)
	}
	if len(m.summaries) != 1 {
		t.Fatalf("filtered list must hold 1 thread, got %d", len(m.summaries))
	}
	out := m.View()
	if !strings.Contains(out, "alpha thread body") {
		t.Errorf("filtered list must show the session's thread:\n%s", out)
	}
	for _, gone := range []string{"beta thread body", "does WAL survive rsync?"} {
		if strings.Contains(out, gone) {
			t.Errorf("filtered list must hide other sessions' threads (%q):\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "vault refactor") {
		t.Errorf("header must show the active session filter:\n%s", out)
	}
}

// The "All sessions" row and esc both clear the filter back to the untouched
// unfiltered load path; s reopens the picker while filtered.
func TestClearSessionFilterRestoresAllThreads(t *testing.T) {
	s, b := seedTUI(t)
	seedSessions(t, s, b)

	// via the "All sessions" row: s reopens the picker while filtered
	m := pickSession(t, openSessions(t, loaded(t, s, b)), "sess-alpha")
	m = pickSession(t, openSessions(t, m), "")
	if m.session != "" {
		t.Fatalf("the All sessions row must clear the filter, got %q", m.session)
	}
	if len(m.summaries) != 3 {
		t.Fatalf("cleared list must restore every thread, got %d", len(m.summaries))
	}
	out := m.View()
	if !strings.Contains(out, "does WAL survive rsync?") || !strings.Contains(out, "beta thread body") {
		t.Errorf("cleared list must show every thread again:\n%s", out)
	}
	if strings.Contains(out, "vault refactor") {
		t.Errorf("cleared list must drop the filter indicator:\n%s", out)
	}

	// via esc on a filtered thread list: clears the filter, stays on the list
	m2 := pickSession(t, openSessions(t, loaded(t, s, b)), "sess-alpha")
	m2, cmd := press(t, m2, "esc")
	if m2.view != viewThreadList {
		t.Fatalf("esc while filtered must stay on the thread list, view = %d", m2.view)
	}
	m2 = runCmd(t, m2, cmd)
	if m2.session != "" || len(m2.summaries) != 3 {
		t.Errorf("esc must clear the filter, got session=%q threads=%d", m2.session, len(m2.summaries))
	}

	// esc on an UNFILTERED thread list keeps its existing meaning: boards
	m3, _ := press(t, loaded(t, s, b), "esc")
	if m3.view != viewBoardPick {
		t.Errorf("unfiltered esc must still open the board picker, view = %d", m3.view)
	}

	// esc inside the picker also clears back to unfiltered
	m4 := openSessions(t, pickSession(t, openSessions(t, loaded(t, s, b)), "sess-alpha"))
	m4, cmd = press(t, m4, "esc")
	if m4.view != viewThreadList {
		t.Fatalf("esc in the picker must return to the thread list, view = %d", m4.view)
	}
	m4 = runCmd(t, m4, cmd)
	if m4.session != "" || len(m4.summaries) != 3 {
		t.Errorf("esc in the picker must clear the filter, got session=%q threads=%d", m4.session, len(m4.summaries))
	}
}

// A board nobody has posted to has no sessions: the picker still offers
// "All sessions" and says so, and choosing it is a plain unfiltered load.
func TestSessionPickerOnBoardWithoutSessions(t *testing.T) {
	s, _ := seedTUI(t)
	other, err := s.GetBoardBySlug("otherboard")
	if err != nil {
		t.Fatalf("GetBoardBySlug: %v", err)
	}
	m := openSessions(t, loaded(t, s, other))
	out := m.View()
	if !strings.Contains(out, "All sessions") {
		t.Errorf("empty picker must still offer All sessions:\n%s", out)
	}
	if !strings.Contains(out, "no sessions on this board yet") {
		t.Errorf("empty picker must say the board has no sessions:\n%s", out)
	}
	m = pickSession(t, m, "")
	if m.session != "" || m.view != viewThreadList {
		t.Errorf("All sessions on an empty board must load unfiltered, session=%q view=%d", m.session, m.view)
	}
}

// r refreshes under the active filter (it must not silently drop it), and the
// filtered list picks up that session's new posts only.
func TestRefreshKeepsSessionFilter(t *testing.T) {
	s, b := seedTUI(t)
	seedSessions(t, s, b)
	m := pickSession(t, openSessions(t, loaded(t, s, b)), "sess-alpha")

	a, err := s.RegisterAgent("claude", "sess-alpha", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "later alpha thread", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	m, cmd := press(t, m, "r")
	m = runCmd(t, m, cmd)
	if m.session != "sess-alpha" {
		t.Fatalf("refresh must keep the filter, got %q", m.session)
	}
	out := m.View()
	if !strings.Contains(out, "later alpha thread") {
		t.Errorf("refresh must re-query under the filter:\n%s", out)
	}
	if strings.Contains(out, "beta thread body") {
		t.Errorf("refresh must not widen the filter:\n%s", out)
	}
}

// Opening a session's threads then a thread detail still works, and switching
// boards drops the filter (a board picker load is always unfiltered).
func TestBoardSwitchDropsSessionFilter(t *testing.T) {
	s, b := seedTUI(t)
	seedSessions(t, s, b)
	m := pickSession(t, openSessions(t, loaded(t, s, b)), "sess-alpha")

	m, cmd := press(t, m, "b")
	m = runCmd(t, m, cmd)
	m, _ = press(t, m, "j") // slug order: otherboard, tuiboard
	m, cmd = press(t, m, "enter")
	m = runCmd(t, m, cmd)
	if m.session != "" {
		t.Fatalf("opening a board must clear the session filter, got %q", m.session)
	}
	if len(m.summaries) != 3 {
		t.Errorf("board load must be unfiltered, got %d threads", len(m.summaries))
	}
}

// Session names are agent-supplied text headed for a human terminal: the
// picker must neither leak control sequences (render.SessionDisplayName
// sanitizes) nor overflow the width (the list delegate truncates).
func TestSessionPickerSanitizesAndFitsNames(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "sess-evil", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "evil session post", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if err := s.SetSessionName("sess-evil", "wipe\x1b[2Jme \x1b]0;owned\x07"+strings.Repeat("long", 8)); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}

	m := New(s, b)
	m = drive(t, m, tea.WindowSizeMsg{Width: 30, Height: 24})
	m = runCmd(t, m, m.Init())
	m = openSessions(t, m)
	out := m.View()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("session picker leaked terminal control bytes:\n%q", out)
	}
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("session picker overflows: widest line %d > 30:\n%s", w, out)
	}
	// premise: the row really rendered, with the printable text kept
	if !strings.Contains(out, "wipeme") {
		t.Errorf("session picker must keep the name's printable text:\n%s", out)
	}

	// ...and the header indicator is bounded by the width too
	m = pickSession(t, m, "sess-evil")
	out = m.View()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("filter indicator leaked terminal control bytes:\n%q", out)
	}
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("filtered thread list overflows: widest line %d > 30:\n%s", w, out)
	}
}

func TestQuitKeys(t *testing.T) {
	s, b := seedTUI(t)
	for _, key := range []string{"q", "ctrl+c"} {
		m := loaded(t, s, b)
		_, cmd := press(t, m, key)
		if cmd == nil {
			t.Fatalf("%s must quit, got nil cmd", key)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s must produce tea.Quit, got %T", key, cmd())
		}
	}
}

func TestRefreshRequeriesTheStore(t *testing.T) {
	s, b := seedTUI(t)
	m := loaded(t, s, b)
	if out := m.View(); strings.Contains(out, "fresh thread") {
		t.Fatal("test premise broken: post exists before creation")
	}

	a, err := s.RegisterAgent("claude", "tui-sess-2", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "fresh thread", "til", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	// no polling: the new post is invisible until r re-queries
	if out := m.View(); strings.Contains(out, "fresh thread") {
		t.Fatalf("model must not see new posts without refresh:\n%s", out)
	}
	m, cmd := press(t, m, "r")
	m = runCmd(t, m, cmd)
	out := m.View()
	if !strings.Contains(out, "fresh thread") || !strings.Contains(out, "[til]") {
		t.Errorf("refresh must re-query and show the new post with its badge:\n%s", out)
	}
}

// postHeader's [human] badge marks provider=human authors and only them.
func TestPostHeaderHumanBadge(t *testing.T) {
	p := store.Post{ID: 1, AuthorHandle: "some-handle-1"}
	if !strings.Contains(postHeader(p, store.Agent{Provider: store.ProviderHuman}), "[human]") {
		t.Errorf("human agent must get [human]")
	}
	if strings.Contains(postHeader(p, store.Agent{}), "[human]") {
		t.Errorf("non-human agent must not get [human]")
	}
}

// Untagged posts resolve too (human posts): the header must carry a bare [✓]
// so the TUI agrees with render.Line and the web UI.
func TestPostHeaderResolvedWithoutTag(t *testing.T) {
	now := time.Now()
	p := store.Post{ID: 1, AuthorHandle: "some-handle-1", ResolvedAt: &now}
	if got := postHeader(p, store.Agent{Provider: store.ProviderHuman}); !strings.Contains(got, "[✓]") {
		t.Errorf("untagged resolved post must show [✓]: %q", got)
	}
	p.ResolvedAt = nil
	if got := postHeader(p, store.Agent{}); strings.Contains(got, "✓") {
		t.Errorf("unresolved untagged post must not show ✓: %q", got)
	}
}

// A post authored by a provider=human agent (i.e. the web UI) must show the
// [human] badge when its thread is open in the detail view.
func TestThreadDetailShowsHumanBadge(t *testing.T) {
	s, b := seedTUI(t)
	human, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		t.Fatalf("RegisterAgent human: %v", err)
	}
	if _, err := s.CreatePost(b.ID, human.Handle, "a post from the web ui", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	m := loaded(t, s, b)
	m.cursor = 0 // newest thread first: the human post
	m, _ = press(t, m, "enter")
	if m.view != viewThreadDetail {
		t.Fatalf("enter must open the thread detail, view = %d", m.view)
	}
	out := m.View()
	if !strings.Contains(out, "[human]") {
		t.Errorf("detail view must show the [human] badge for a human-authored post:\n%s", out)
	}
}

func TestCursorMovesAndSelectsSecondThread(t *testing.T) {
	s, b := seedTUI(t)
	a, err := s.RegisterAgent("claude", "tui-sess-3", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "second thread body", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	m := loaded(t, s, b)
	if len(m.summaries) != 2 {
		t.Fatalf("want 2 threads, got %d", len(m.summaries))
	}
	m, _ = press(t, m, "j")
	m, _ = press(t, m, "enter")
	if m.view != viewThreadDetail {
		t.Fatalf("enter after j must open detail, view = %d", m.view)
	}
	// newest activity sorts first, so the second row is the older question
	if out := m.View(); !strings.Contains(out, "does WAL survive rsync?") {
		t.Errorf("j+enter must open the second (older) thread:\n%s", out)
	}
	// k moves back up after esc
	m, _ = press(t, m, "esc")
	m, _ = press(t, m, "k")
	m, _ = press(t, m, "enter")
	if out := m.View(); !strings.Contains(out, "second thread body") {
		t.Errorf("k+enter must open the first thread:\n%s", out)
	}
}

// postHeader shows the author's repo hint right after the handle.
func TestPostHeaderRepo(t *testing.T) {
	p := store.Post{ID: 1, AuthorHandle: "some-handle-1"}
	if got := postHeader(p, store.Agent{Repo: "xfa"}); !strings.Contains(got, "some-handle-1 (xfa)") {
		t.Errorf("repo missing: %q", got)
	}
	if got := postHeader(p, store.Agent{}); strings.Contains(got, "(") {
		t.Errorf("no-repo header must not add parens: %q", got)
	}
}
