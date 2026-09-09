package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/securisec/xfa/internal/store"
)

// activityInterval is the poll period of the `xfa tui --web` activity log; a
// var so tests can shrink it.
var activityInterval = 2 * time.Second

// termWidth is the per-event stdout width probe (one ioctl, so a resize is
// honored); <= 0 means "don't truncate", mirroring Model.fit. A var for tests.
var termWidth = stdoutWidth

func stdoutWidth() int {
	cols, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		return 0
	}
	return cols
}

// Activity tails the board to the terminal while `xfa tui --web` runs — see
// CLAUDE.md ("--web activity log") for the design rationale.
type Activity struct {
	s       *store.Store
	w       io.Writer
	version int64
	lastID  int64                   // MaxPostID at snapshot; advances only from returned rows
	marks   map[uint]store.PostMark // last seen resolved/tombstoned flags per post
	boards  map[uint]string         // id -> slug; a miss is a new board
	lastErr string                  // last error printed; each distinct error prints once per outage
}

// NewActivity snapshots the current state synchronously — nothing existing
// is ever replayed. s must be a store private to the poller (see cmd/tui.go);
// it is never written to.
func NewActivity(s *store.Store, w io.Writer) (*Activity, error) {
	a := &Activity{s: s, w: w, marks: map[uint]store.PostMark{}, boards: map[uint]string{}}
	var err error
	if a.version, err = s.DataVersion(); err != nil {
		return nil, err
	}
	if a.lastID, err = s.MaxPostID(); err != nil {
		return nil, err
	}
	bs, err := s.ListBoards()
	if err != nil {
		return nil, err
	}
	for _, b := range bs {
		a.boards[b.ID] = b.Slug
	}
	marks, err := s.MarkedPostIDs()
	if err != nil {
		return nil, err
	}
	for _, m := range marks {
		a.marks[m.ID] = m
	}
	return a, nil
}

// Run polls until ctx is done. Goroutine body.
func (a *Activity) Run(ctx context.Context) {
	t := time.NewTicker(activityInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.tick(ctx)
		}
	}
}

// tick is one poll: the data_version gate, then a scan only on a foreign commit.
func (a *Activity) tick(ctx context.Context) {
	v, err := a.s.DataVersion()
	if err == nil && v == a.version {
		return
	}
	// The gate advances only on a successful scan, so a transient scan error
	// re-scans next tick instead of parking events until the next commit.
	if err == nil {
		if err = a.scan(ctx); err == nil {
			a.version, a.lastErr = v, ""
		}
	}
	if err != nil && err.Error() != a.lastErr {
		a.lastErr = err.Error()
		a.print(ctx, errStyle.Render("activity: "+a.lastErr))
	}
}

// scan reports new boards, posts past the id watermark, then mark changes.
func (a *Activity) scan(ctx context.Context) error {
	bs, err := a.s.ListBoards()
	if err != nil {
		return err
	}
	for _, b := range bs {
		if _, ok := a.boards[b.ID]; !ok {
			a.boards[b.ID] = b.Slug
			a.print(ctx, a.line(titleStyle, "board", a.slug(b.ID)+" created"))
		}
	}
	// AUTOINCREMENT never reuses ids, so a hard delete lowering MAX(id) is
	// harmless: lastID only ever advances from rows actually returned.
	posts, err := a.s.PostsAfter(a.lastID)
	if err != nil {
		return err
	}
	authors := a.s.AuthorsForPosts(posts)
	for _, p := range posts {
		a.lastID = int64(p.ID)
		// A create+resolve or create+tombstone inside one tick prints once:
		// postHeader shows ✓ and PostsAfter already masked [deleted].
		a.marks[p.ID] = store.PostMark{ID: p.ID, Resolved: p.ResolvedAt != nil, Tombstoned: p.TombstonedAt != nil}
		verb, st, tail := "post", titleStyle, ""
		if p.ParentID != nil {
			verb, st, tail = "reply", replyStyle, dimStyle.Render(fmt.Sprintf("↳ #%d", *p.ParentID))+"  "
		}
		a.print(ctx, a.line(st, verb, postHeader(p, authors[p.AuthorHandle])+"  "+tail+a.slug(p.BoardID)+"  "+firstLine(p.Body)))
	}
	marks, err := a.s.MarkedPostIDs()
	if err != nil {
		return err
	}
	return a.mark(ctx, marks)
}

// mark prints a resolve/delete line for every flag newly set since the last
// scan and records the new flags.
func (a *Activity) mark(ctx context.Context, marks []store.PostMark) error {
	for _, m := range marks {
		// Created AND marked between PostsAfter and MarkedPostIDs: the next
		// PostsAfter row carries the mark and prints once. Don't record it.
		if int64(m.ID) > a.lastID {
			continue
		}
		old := a.marks[m.ID]
		a.marks[m.ID] = m
		newR, newT := m.Resolved && !old.Resolved, m.Tombstoned && !old.Tombstoned
		if !newR && !newT {
			continue
		}
		p, err := a.s.GetPost(m.ID)
		if errors.Is(err, store.ErrNoPost) {
			continue // web hard delete raced the scan; flags are recorded, nothing to show
		}
		if err != nil {
			return err
		}
		head := postHeader(*p, a.s.AuthorsForPosts([]store.Post{*p})[p.AuthorHandle]) + "  " + a.slug(p.BoardID)
		if newR {
			a.print(ctx, a.line(resolvedStyle, "resolve", head+"  "+dimStyle.Render("by "+p.ResolvedBy)))
		}
		if newT {
			a.print(ctx, a.line(errStyle, "delete", head))
		}
	}
	return nil
}

func (a *Activity) slug(boardID uint) string {
	return dimStyle.Render("b/" + a.boards[boardID])
}

// line is "HH:MM:SS  verb     rest", truncated to the current terminal width.
func (a *Activity) line(st lipgloss.Style, verb, rest string) string {
	l := strings.Join([]string{dimStyle.Render(time.Now().Format("15:04:05")), st.Render(fmt.Sprintf("%-7s", verb)), rest}, "  ")
	if w := termWidth(); w > 0 {
		return ansi.Truncate(l, w, "…")
	}
	return l
}

func (a *Activity) print(ctx context.Context, line string) {
	if ctx.Err() != nil {
		return
	}
	fmt.Fprintln(a.w, line)
}
