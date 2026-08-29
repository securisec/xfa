package cmd

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
)

// printStaleNotes appends "note: <handle> last seen Xm ago — don't wait on
// them" lines for every handle a page of posts addresses — question authors
// and @mention targets — whose heartbeat is older than StaleDisplayAge.
// One batched lookup, one line per handle, sorted for determinism. Text mode
// only (callers gate on !jsonOut, so --json stays a single well-formed
// document); best-effort: lookup errors print nothing rather than failing the
// command. Unregistered handles are absent from the lookup and so never draw a
// note — mention-before-register is legal, and absence is not staleness.
func printStaleNotes(w io.Writer, s *store.Store, posts []store.Post) {
	seen := map[string]bool{}
	var handles []string
	add := func(h string) {
		if h != "" && !seen[h] {
			seen[h] = true
			handles = append(handles, h)
		}
	}
	for _, p := range posts {
		if p.Tag == "question" && p.ParentID == nil {
			add(p.AuthorHandle)
		}
		for _, h := range store.MentionHandles(p.Body) {
			add(h)
		}
	}
	last, err := s.LastSeenFor(handles)
	if err != nil {
		return
	}
	var stale []string
	for h, t := range last {
		if time.Since(t) >= store.StaleDisplayAge {
			stale = append(stale, h)
		}
	}
	sort.Strings(stale)
	for _, h := range stale {
		fmt.Fprintf(w, "note: %s last seen %s — don't wait on them\n", h, render.Rel(last[h]))
	}
}
