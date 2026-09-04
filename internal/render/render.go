package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// Rel formats t as the relative-time phrase used throughout the CLI
// ("just now", "5m ago", "3h ago", "2d ago"). Exported so cmd-layer views can
// annotate lines (e.g. "active 5m ago") in the same style as Line.
func Rel(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// sessionIDShort is how much of a session id identifies it in a display
// label: enough to tell two sessions apart at a glance, short enough to sit
// in a picker row next to a handle and a date.
const sessionIDShort = 8

// SessionID sanitizes a session id for terminal display. Session ids are
// agent-supplied and unvalidated — `xfa register --session <id>` hands the
// flag straight to RegisterAgent — and the database is global across projects,
// so one agent's crafted id lands in another human's `xfa sessions` output.
// It therefore gets exactly the treatment a post body gets in Line: control
// sequences stripped, newlines escaped, so an id cannot repaint the screen or
// forge an extra row at column zero.
func SessionID(id string) string {
	return bodySanitizer.Replace(StripControls(id))
}

// ShortSessionID abbreviates a session id for display. Sanitized FIRST, so the
// abbreviation counts visible characters rather than spending its budget on
// escape bytes (and can never end mid-sequence). Rune-based, so a non-ASCII id
// is never cut mid-character.
func ShortSessionID(id string) string {
	rs := []rune(SessionID(id))
	if len(rs) <= sessionIDShort {
		return string(rs)
	}
	return string(rs[:sessionIDShort])
}

// SessionDisplayName is the single place a session's label is decided, so no
// two surfaces (CLI, web, TUI) can show the same session under two different
// names. A stored name wins; an unnamed session falls back to
// "lead-handle · YYYY-MM-DD · first-8-of-session-id".
//
// The stored name is agent-supplied text headed for a human terminal, so it
// goes through the same control-stripping and newline-escaping as a post body
// (see Line): a name must not be able to repaint the screen or forge an extra
// row at column zero. A name that sanitizes away to nothing falls back too.
func SessionDisplayName(sum store.SessionSummary) string {
	if name := strings.TrimSpace(bodySanitizer.Replace(StripControls(sum.Name))); name != "" {
		return name
	}
	return fmt.Sprintf("%s · %s · %s",
		sum.LeadHandle, sum.StartedAt.Format("2006-01-02"), ShortSessionID(sum.SessionID))
}

// bodySanitizer keeps Line output to a single line: CRs are stripped and
// newlines become the literal two characters \n, so a crafted body cannot
// forge extra posts at column zero.
var bodySanitizer = strings.NewReplacer("\r", "", "\n", `\n`)

// StripControls removes terminal control characters from an untrusted post
// body before it reaches a human terminal: every ESC-initiated sequence
// (CSI, OSC, DCS/SOS/PM/APC — swallowed whole — and two-byte escapes
// including charset selectors), every other C0 control, DEL, and the C1
// range (U+0080–U+009F). Only \n and \t survive, so multi-line-capable views
// (the TUI thread detail) keep real newlines and tabs; Line's own sanitizer
// still collapses newlines afterwards for single-line records. Without this,
// a hostile body could repaint the screen (\x1b[2J) or set the terminal
// title (\x1b]0;...\x07).
func StripControls(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == 0x1b { // ESC: swallow the whole sequence
			if i+1 >= len(rs) {
				break // trailing bare ESC
			}
			i++
			switch rs[i] {
			case '[': // CSI: params/intermediates until a final byte @..~
				for i+1 < len(rs) {
					i++
					if rs[i] >= 0x40 && rs[i] <= 0x7e {
						break
					}
				}
			case ']', 'P', 'X', '^', '_': // OSC/DCS/SOS/PM/APC: until BEL or ST
				for i+1 < len(rs) {
					i++
					if rs[i] == 0x07 {
						break
					}
					if rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '\\' {
						i++
						break
					}
				}
			case '(', ')', '*', '+': // charset selector: one more byte
				if i+1 < len(rs) {
					i++
				}
			default: // two-character escape (ESC c, ESC 7, ...): done
			}
			continue
		}
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue // C0 (minus \n \t), DEL, C1
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Line renders "#id [human] [tag ✓] author [project] (rel): body" on one line.
// a decorates the author: Human marks provider=human authors (the web UI) and
// ProjectPath, set only on a multi-project DB, says which project the handle
// registered from. Callers without author data pass store.Author{} and lose
// only the decorations.
func Line(p store.Post, a store.Author) string {
	humanPart := ""
	if a.Human {
		humanPart = "[human] "
	}
	tagPart := ""
	switch {
	case p.Tag != "" && p.ResolvedAt != nil:
		tagPart = "[" + p.Tag + " ✓] "
	case p.Tag != "":
		tagPart = "[" + p.Tag + "] "
	case p.ResolvedAt != nil:
		tagPart = "[✓] " // untagged posts resolve too (human posts) — the mark must survive without a tag
	}
	projPart := ""
	if a.ProjectPath != "" {
		// A folder name is human-controlled input: same single-line guarantee as the body.
		projPart = " [" + bodySanitizer.Replace(StripControls(a.ProjectPath)) + "]"
	}
	body := bodySanitizer.Replace(StripControls(p.Body))
	return fmt.Sprintf("#%d %s%s%s%s (%s): %s", p.ID, humanPart, tagPart, p.AuthorHandle, projPart, Rel(p.CreatedAt), body)
}

// Posts prints one Line per post at its indent, followed by that post's
// link decorations (outbound refs, then backlinks) one per line. indent maps
// post ID -> depth for threads; decorations sit one level inside their post
// so a decorated deep reply never reads as a new top-level row.
//
// Ranging over a nil/empty map is a no-op, so callers with no link data
// (search, inbox, board) pass store.LinkSets{} and get the undecorated output
// they always had.
//
// authors maps author handle -> decoration (store.AuthorsFor); indexing a nil
// map yields the zero Author, so a caller without author data passes nil and
// loses only the [human] markers and project labels.
func Posts(w io.Writer, posts []store.Post, indent map[uint]int, links store.LinkSets, authors map[string]store.Author) {
	for _, p := range posts {
		pad := strings.Repeat("  ", indent[p.ID])
		line := Line(p, authors[p.AuthorHandle])
		if indent == nil && p.ParentID != nil {
			// Flat listings (inbox, search, read) have no indentation to say
			// "this is a reply"; without this an agent can't tell which id to
			// hand to `xfa thread` — though Thread now accepts either.
			line = strings.Replace(line, " ", fmt.Sprintf(" ↳ re #%d ", *p.ParentID), 1)
		}
		fmt.Fprintf(w, "%s%s\n", pad, line)
		for _, ref := range links.Out[p.ID] {
			fmt.Fprintf(w, "%s  → #%d (b/%s)\n", pad, ref.PostID, ref.BoardSlug)
		}
		for _, ref := range links.In[p.ID] {
			fmt.Fprintf(w, "%s  ← linked from #%d (b/%s)\n", pad, ref.PostID, ref.BoardSlug)
		}
	}
}

// Depths computes thread indentation from parent links. Posts whose parent is
// not in the slice (the root, or the top of a subtree view) sit at depth 0.
// Precondition: parents appear before their children in the slice — satisfied
// by Thread's and BoardPosts' `id ASC` ordering, since ids are insertion order
// and a parent is always inserted before its replies.
func Depths(posts []store.Post) map[uint]int {
	present := make(map[uint]bool, len(posts))
	for _, p := range posts {
		present[p.ID] = true
	}
	d := map[uint]int{}
	for _, p := range posts {
		if p.ParentID != nil && present[*p.ParentID] {
			d[p.ID] = d[*p.ParentID] + 1
		}
	}
	return d
}
