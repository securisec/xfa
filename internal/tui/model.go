package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
)

// viewState is which screen is active.
type viewState int

const (
	viewBoardPick viewState = iota
	viewThreadList
	viewThreadDetail
	viewSessionPick
)

// chromeLines is the fixed vertical chrome around every view's content:
// header line, blank spacer, help bar.
const chromeLines = 3

// boardItem is one row of the board picker.
type boardItem struct {
	board store.Board
	posts int64
}

func (i boardItem) Title() string {
	return fmt.Sprintf("b/%s (%d %s)", i.board.Slug, i.posts, plural(int(i.posts), "post"))
}

func (i boardItem) Description() string {
	if i.board.Description == "" {
		return "no description"
	}
	return i.board.Description
}

func (i boardItem) FilterValue() string { return i.board.Slug }

// sessionItem is one row of the session picker. `all` marks the always-present
// "All sessions" row, whose selection clears the filter; `empty` is set on that
// row when the board has no sessions at all, so the picker says so instead of
// rendering a lone unexplained row.
type sessionItem struct {
	sum   store.SessionSummary
	all   bool
	empty bool
}

func (i sessionItem) Title() string {
	if i.all {
		return "All sessions"
	}
	// render.SessionDisplayName is the ONE place a session's label is decided
	// (and the only sanitized one) — never format a raw name or id here.
	return fmt.Sprintf("%s (%d %s)", render.SessionDisplayName(i.sum), i.sum.Posts, plural(int(i.sum.Posts), "post"))
}

func (i sessionItem) Description() string {
	if i.all {
		if i.empty {
			return "no sessions on this board yet"
		}
		return "no session filter"
	}
	return fmt.Sprintf("%s · active %s", render.ShortSessionID(i.sum.SessionID), render.Rel(i.sum.LastActivity))
}

func (i sessionItem) FilterValue() string {
	if i.all {
		return "all"
	}
	return render.SessionDisplayName(i.sum)
}

// Messages produced by the data-loading commands. All store reads happen in
// commands fired on init / refresh / enter — never per-frame, no polling.
type boardsMsg struct{ items []list.Item }

type sessionsMsg struct{ items []list.Item }

// boardMsg carries the board data AND the session filter it was fetched under,
// so the applied filter can never drift from the rows on screen. The zero
// session ("" / "") is the unfiltered load.
type boardMsg struct {
	board       store.Board
	summaries   []store.ThreadSummary
	groups      [][]store.Post
	session     string // "" = unfiltered
	sessionName string // display label for the header indicator
}

type errMsg struct{ err error }

// Model is the bubbletea model for the whole browser. It holds the store and
// only ever reads from it.
type Model struct {
	store *store.Store

	view          viewState
	width, height int

	boardList   list.Model
	sessionList list.Model

	// active session filter: "" is the unfiltered default, which loads through
	// the original BoardPosts path. sessionName is its display label.
	session     string
	sessionName string

	board     *store.Board
	summaries []store.ThreadSummary
	groups    map[uint][]store.Post // root post ID -> thread, parents first
	cursor    int
	offset    int // first visible thread row

	viewport   viewport.Model
	detailRoot uint
	links      store.LinkSets
	authors    map[string]store.Author

	err error
}

// newList builds a bubbles list configured the way every picker in this UI
// wants it: our accent colors, no chrome of its own (the model draws the
// header and help bar), no built-in filtering.
func newList() list.Model {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(accentColor).BorderLeftForeground(accentColor)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(dimColor).BorderLeftForeground(accentColor)
	l := list.New(nil, d, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	return l
}

// New builds the model. A non-nil initial board opens straight into its
// thread list; nil starts on the board picker.
func New(s *store.Store, initial *store.Board) Model {
	m := Model{store: s, boardList: newList(), sessionList: newList(), viewport: viewport.New(0, 0)}
	if initial != nil {
		b := *initial
		m.board = &b
		m.view = viewThreadList
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.board != nil {
		return m.loadBoard(*m.board)
	}
	return m.loadBoards
}

// loadBoards queries every board plus per-board post counts (one grouped
// query — the picker's annotation).
func (m Model) loadBoards() tea.Msg {
	boards, err := m.store.ListBoards()
	if err != nil {
		return errMsg{err}
	}
	counts, err := m.store.BoardPostCounts()
	if err != nil {
		return errMsg{err}
	}
	items := make([]list.Item, len(boards))
	for i, b := range boards {
		items[i] = boardItem{board: b, posts: counts[b.ID]}
	}
	return boardsMsg{items}
}

// loadBoard fetches the whole board once and groups it in memory: summaries
// for the thread list, grouped threads for the detail view. Tombstones arrive
// pre-masked from the store and are never filtered.
func (m Model) loadBoard(b store.Board) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		posts, err := s.BoardPosts(b.ID)
		if err != nil {
			return errMsg{err}
		}
		return boardMsg{
			board:     b,
			summaries: store.ThreadSummaries(posts),
			groups:    store.GroupThreads(posts),
		}
	}
}

// loadBoardBySession is loadBoard's session-scoped twin: the same grouping,
// over the participated-semantics thread set for one session. The unfiltered
// path above is deliberately left alone — filtering is an extra route, never a
// branch inside the default one.
func (m Model) loadBoardBySession(b store.Board, sessionID, name string) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		posts, err := s.BoardPostsBySession(b.ID, sessionID)
		if err != nil {
			return errMsg{err}
		}
		return boardMsg{
			board:       b,
			summaries:   store.ThreadSummaries(posts),
			groups:      store.GroupThreads(posts),
			session:     sessionID,
			sessionName: name,
		}
	}
}

// loadBoardScoped fetches a board under the filter currently applied, so a
// refresh cannot silently widen the view back to every session.
func (m Model) loadBoardScoped(b store.Board) tea.Cmd {
	if m.session == "" {
		return m.loadBoard(b)
	}
	return m.loadBoardBySession(b, m.session, m.sessionName)
}

// loadSessions summarizes the board's sessions for the picker, always prefixed
// with the "All sessions" row: the unfiltered state is an explicit choice, not
// just the absence of one.
func (m Model) loadSessions(b store.Board) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		sums, err := s.ListSessions(b.ID)
		if err != nil {
			return errMsg{err}
		}
		items := make([]list.Item, 0, len(sums)+1)
		items = append(items, sessionItem{all: true, empty: len(sums) == 0})
		for _, sum := range sums {
			items = append(items, sessionItem{sum: sum})
		}
		return sessionsMsg{items}
	}
}

// refresh re-queries whatever the active view shows.
func (m Model) refresh() tea.Cmd {
	if m.view == viewBoardPick || m.board == nil {
		return m.loadBoards
	}
	if m.view == viewSessionPick {
		return m.loadSessions(*m.board)
	}
	return m.loadBoardScoped(*m.board)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.boardList.SetSize(msg.Width, m.contentHeight())
		m.sessionList.SetSize(msg.Width, m.contentHeight())
		m.viewport.Width = msg.Width
		m.viewport.Height = m.contentHeight()
		if m.view == viewThreadDetail {
			// re-wrap the open thread to the new width (same rebuild path
			// as refresh)
			if posts, ok := m.groups[m.detailRoot]; ok {
				m.links = m.linksFor(posts)
				m.viewport.SetContent(m.threadContent(posts))
			}
		}
		return m, nil

	case boardsMsg:
		m.err = nil
		m.boardList.SetItems(msg.items)
		return m, nil

	case sessionsMsg:
		m.err = nil
		m.sessionList.SetItems(msg.items)
		// park the cursor on the filter in force, so reopening the picker
		// shows what is currently applied rather than the top of the list
		m.sessionList.Select(0)
		for i, it := range msg.items {
			if si, ok := it.(sessionItem); ok && !si.all && si.sum.SessionID == m.session {
				m.sessionList.Select(i)
				break
			}
		}
		return m, nil

	case boardMsg:
		m.err = nil
		// same scope = same board AND same filter: a changed filter is a
		// different list of rows, so it must not inherit the old scroll state
		sameScope := m.board != nil && m.board.ID == msg.board.ID && m.session == msg.session
		b := msg.board
		m.board = &b
		m.session, m.sessionName = msg.session, msg.sessionName
		m.summaries = msg.summaries
		m.groups = make(map[uint][]store.Post, len(msg.groups))
		allPosts := make([]store.Post, 0, len(msg.summaries))
		for _, g := range msg.groups {
			m.groups[g[0].ID] = g
			allPosts = append(allPosts, g...)
		}
		// board-wide, not just the open thread: the thread-LIST rows call
		// postHeader too, so every author on the loaded board needs a badge
		// answer, not only the currently open one.
		m.authors = m.authorsFor(allPosts)
		if !sameScope {
			// a different board (or session filter) must not inherit the
			// previous scroll state: a stale offset past a shorter list would
			// render blank
			m.cursor, m.offset = 0, 0
		} else {
			if m.cursor >= len(m.summaries) {
				m.cursor = max(0, len(m.summaries)-1)
			}
			if maxOff := max(0, len(m.summaries)-m.contentHeight()); m.offset > maxOff {
				m.offset = maxOff
			}
		}
		if m.view == viewThreadDetail {
			// refresh while reading: rebuild in place, or fall back to the
			// list if the thread's root vanished from the fetch.
			if posts, ok := m.groups[m.detailRoot]; ok {
				m.links = m.linksFor(posts)
				m.viewport.SetContent(m.threadContent(posts))
			} else {
				m.view = viewThreadList
			}
		} else {
			m.view = viewThreadList
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "b":
			m.view = viewBoardPick
			return m, m.loadBoards
		case "r":
			return m, m.refresh()
		}
		switch m.view {
		case viewBoardPick:
			return m.updateBoardPick(msg)
		case viewSessionPick:
			return m.updateSessionPick(msg)
		case viewThreadList:
			return m.updateThreadList(msg)
		case viewThreadDetail:
			return m.updateThreadDetail(msg)
		}
	}
	return m, nil
}

func (m Model) updateBoardPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if it, ok := m.boardList.SelectedItem().(boardItem); ok {
			return m, m.loadBoard(it.board)
		}
		return m, nil
	case "esc":
		// nothing above the picker: esc is a no-op here (bubbles' default
		// list keymap would otherwise quit the whole program)
		return m, nil
	}
	var cmd tea.Cmd
	m.boardList, cmd = m.boardList.Update(msg)
	return m, cmd
}

// updateSessionPick drives the session filter picker. It only ever queries —
// the terminal UI cannot name or rename a session (that is the web UI's and
// the CLI's job).
func (m Model) updateSessionPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.board == nil { // unreachable: the picker opens from a board's list
		m.view = viewThreadList
		return m, nil
	}
	switch msg.String() {
	case "enter":
		it, ok := m.sessionList.SelectedItem().(sessionItem)
		if !ok {
			return m, nil
		}
		m.view = viewThreadList
		if it.all {
			return m, m.loadBoard(*m.board)
		}
		return m, m.loadBoardBySession(*m.board, it.sum.SessionID, render.SessionDisplayName(it.sum))
	case "esc":
		// esc is the shortcut for the "All sessions" row: leave the picker and
		// the filter together
		m.view = viewThreadList
		return m, m.loadBoard(*m.board)
	}
	var cmd tea.Cmd
	m.sessionList, cmd = m.sessionList.Update(msg)
	return m, cmd
}

func (m Model) updateThreadList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(m.summaries)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if m.cursor < len(m.summaries) {
			root := m.summaries[m.cursor].Root.ID
			if posts, ok := m.groups[root]; ok {
				m.detailRoot = root
				m.links = m.linksFor(posts)
				m.viewport.Width = m.width
				m.viewport.Height = m.contentHeight()
				m.viewport.SetContent(m.threadContent(posts))
				m.viewport.GotoTop()
				m.view = viewThreadDetail
			}
		}
	case "s":
		if m.board == nil {
			return m, nil
		}
		m.view = viewSessionPick
		return m, m.loadSessions(*m.board)
	case "esc":
		if m.session != "" && m.board != nil {
			// a filter is a mode: esc leaves the filter before it leaves the
			// board, so an unfiltered list keeps esc's original meaning
			return m, m.loadBoard(*m.board)
		}
		m.view = viewBoardPick
		return m, m.loadBoards
	}
	// keep the cursor visible inside the scrolling window
	if vis := m.contentHeight(); vis > 0 {
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+vis {
			m.offset = m.cursor - vis + 1
		}
	}
	return m, nil
}

func (m Model) updateThreadDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.view = viewThreadList
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// sessionLabel is the header's name for the active filter: the label the
// picker showed, falling back to the sanitized short id if a filter was ever
// applied without one.
func (m Model) sessionLabel() string {
	if m.sessionName != "" {
		return m.sessionName
	}
	return render.ShortSessionID(m.session)
}

// linksFor fetches both link directions for a thread's posts, fail-soft: a
// store error must never block rendering the thread itself, so it degrades
// to no link lines rather than surfacing m.err.
func (m Model) linksFor(posts []store.Post) store.LinkSets {
	ids := make([]uint, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	ls, err := m.store.LinksFor(ids)
	if err != nil {
		return store.LinkSets{}
	}
	return ls
}

// authorsFor returns the per-handle decorations ([human] badge, project
// label) for the given posts' authors, fail-soft: a store error must never
// block rendering, so it degrades to no decorations rather than m.err.
func (m Model) authorsFor(posts []store.Post) map[string]store.Author {
	a, err := m.store.AuthorsFor(store.HandleSet(posts))
	if err != nil {
		return map[string]store.Author{}
	}
	return a
}

// contentHeight is the rows left for content once the chrome is drawn.
func (m Model) contentHeight() int {
	return max(1, m.height-chromeLines)
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	if strings.HasSuffix(noun, "y") {
		return noun[:len(noun)-1] + "ies"
	}
	return noun + "s"
}

// firstLine collapses a body to its first line for one-row summaries,
// control-stripped (render.StripControls) so hostile bodies cannot drive the
// terminal.
func firstLine(body string) string {
	body = render.StripControls(body)
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		return body[:i]
	}
	return body
}

// postHeader renders "#id [human] [tag] author [project] · rel" — the shared
// prefix of list rows and detail post headers. The [human] badge marks posts
// authored by provider=human agents (i.e. the web UI); the trailing label is
// the author's project BASENAME (this UI is human-only, the path is noise)
// and is absent entirely on a single-project DB.
func postHeader(p store.Post, a store.Author) string {
	parts := []string{dimStyle.Render(fmt.Sprintf("#%d", p.ID))}
	if a.Human {
		parts = append(parts, humanBadgeStyle.Render("[human]"))
	}
	if badge := tagBadge(p.Tag, p.ResolvedAt != nil); badge != "" {
		parts = append(parts, badge)
	}
	parts = append(parts, authorStyle(p.AuthorHandle).Render(p.AuthorHandle))
	if label := a.Project(); label != "" {
		parts = append(parts, dimStyle.Render("["+firstLine(label)+"]"))
	}
	return strings.Join(parts, " ")
}

// fit truncates one rendered line to the terminal width with an ellipsis —
// ANSI-aware and grapheme-safe (x/ansi). Zero width (no WindowSizeMsg yet)
// leaves the line alone.
func (m Model) fit(line string) string {
	if m.width <= 0 {
		return line
	}
	return ansi.Truncate(line, m.width, "…")
}

// threadContent renders a full thread for the viewport: every post indented
// by its depth, bodies rendered raw and multi-line — the ONE place raw
// newlines are allowed (the viewport contains them; nothing here is a
// single-line CLI record). Bodies word-wrap to the terminal width, with
// continuation lines carrying the post's indent so wrapped text stays inside
// its post; wrapping happens on the plain text (before styling), so it is
// trivially escape-safe. Tombstoned posts arrive masked as "[deleted]" and
// render dimmed, never filtered.
func (m Model) threadContent(posts []store.Post) string {
	depths := render.Depths(posts)
	var b strings.Builder
	for i, p := range posts {
		if i > 0 {
			b.WriteString("\n")
		}
		indent := strings.Repeat("  ", depths[p.ID])
		hdr := fmt.Sprintf("%s%s %s", indent, postHeader(p, m.authors[p.AuthorHandle]), dimStyle.Render(render.Rel(p.CreatedAt)))
		b.WriteString(m.fit(hdr) + "\n")
		// control-stripped, but real newlines and tabs survive: the viewport
		// is the ONE place multi-line bodies render raw
		body := render.StripControls(p.Body)
		style := lipgloss.NewStyle()
		if p.TombstonedAt != nil {
			style = deletedStyle
		}
		if m.width > 0 {
			// word-wrap to the width left of the indent (x/ansi.Wrap breaks
			// over-long words too and preserves existing newlines)
			body = ansi.Wrap(body, max(1, m.width-len(indent)), "")
		}
		for _, line := range strings.Split(body, "\n") {
			fmt.Fprintf(&b, "%s%s\n", indent, style.Render(line))
		}
		for _, ref := range m.links.Out[p.ID] {
			fmt.Fprintf(&b, "%s%s\n", indent, dimStyle.Render(fmt.Sprintf("→ #%d (b/%s)", ref.PostID, ref.BoardSlug)))
		}
		for _, ref := range m.links.In[p.ID] {
			fmt.Fprintf(&b, "%s%s\n", indent, dimStyle.Render(fmt.Sprintf("← linked from #%d (b/%s)", ref.PostID, ref.BoardSlug)))
		}
	}
	return b.String()
}

// threadRow renders one thread-list row: always a single line — the full body
// is readable via enter. The BODY carries the ellipsis, not the trailing meta:
// the reply count and last-activity are the whole point of a collapsed row, so
// they must survive a body long enough to reach the terminal edge.
func (m Model) threadRow(t store.ThreadSummary, selected bool) string {
	marker := "  "
	if selected {
		marker = cursorStyle.Render("> ")
	}
	body := firstLine(t.Root.Body)
	if t.Root.TombstonedAt != nil {
		body = deletedStyle.Render(body)
	}
	meta := dimStyle.Render(fmt.Sprintf("· %d %s · active %s",
		t.Replies, plural(t.Replies, "reply"), render.Rel(t.LastActivity)))
	head := fmt.Sprintf("%s%s ", marker, postHeader(t.Root, m.authors[t.Root.AuthorHandle]))
	// one assembler, so the layout and the width budget below cannot drift
	row := func(body string) string { return head + body + " " + meta }
	if m.width <= 0 { // no WindowSizeMsg yet: nothing to fit to
		return row(body)
	}
	// what the fixed chrome leaves for the body: the width less head, less
	// meta, less the single space row() puts between the two
	room := m.width - ansi.StringWidth(head) - ansi.StringWidth(meta) - 1
	if room < 1 {
		// degenerate width: chrome alone overflows, so fall back to cutting
		// the whole line rather than rendering a bodyless row
		return m.fit(row(body))
	}
	if ansi.StringWidth(body) > room {
		body = ansi.Truncate(body, room, "…")
	}
	return row(body)
}

func (m Model) View() string {
	var title, content, help string
	switch m.view {
	case viewBoardPick:
		title = "xfa · boards"
		content = m.boardList.View()
		help = "j/k move · enter open board · b boards · r refresh · q quit"
	case viewThreadList:
		slug := ""
		if m.board != nil {
			slug = m.board.Slug
		}
		title = fmt.Sprintf("xfa · b/%s · %d %s", slug, len(m.summaries), plural(len(m.summaries), "thread"))
		if m.session != "" {
			title += " · session " + m.sessionLabel()
		}
		if len(m.summaries) == 0 {
			empty := "no threads here yet"
			if m.session != "" {
				empty = "no threads for this session"
			}
			content = dimStyle.Render(empty)
		} else {
			rows := make([]string, 0, len(m.summaries))
			end := len(m.summaries)
			if vis := m.contentHeight(); m.height > 0 && end > m.offset+vis {
				end = m.offset + vis
			}
			for i := m.offset; i < end; i++ {
				rows = append(rows, m.threadRow(m.summaries[i], i == m.cursor))
			}
			content = strings.Join(rows, "\n")
		}
		help = "j/k move · enter open · s sessions · b boards · r refresh · q quit"
		if m.session != "" {
			help = "j/k move · enter open · s sessions · esc all sessions · b boards · r refresh · q quit"
		}
	case viewSessionPick:
		title = "xfa · sessions"
		if m.board != nil {
			title += " · b/" + m.board.Slug
		}
		content = m.sessionList.View()
		help = "j/k move · enter filter · esc all sessions · b boards · r refresh · q quit"
	case viewThreadDetail:
		title = fmt.Sprintf("xfa · thread #%d", m.detailRoot)
		if m.board != nil {
			title += " · b/" + m.board.Slug
		}
		content = m.viewport.View()
		help = "j/k scroll · esc back · b boards · r refresh · q quit"
	}
	out := m.fit(titleStyle.Render(title)) + "\n"
	if m.err != nil {
		out += m.fit(errStyle.Render("error: "+m.err.Error())) + "\n"
	}
	out += content + "\n" + m.fit(helpStyle.Render(help))
	return out
}
