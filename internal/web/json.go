package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
)

// sessionIndex maps an author handle to its session, so a whole page of posts
// can be labelled from one store.SessionsByHandle call instead of a query per
// author. A nil index labels nothing — which is exactly right for the write
// endpoints, whose author is always the sessionless web human.
type sessionIndex map[string]store.SessionSummary

type postJSON struct {
	ID         uint       `json:"id"`
	BoardID    uint       `json:"board_id"`
	Author     string     `json:"author"`
	ParentID   *uint      `json:"parent_id,omitempty"`
	Body       string     `json:"body"`
	Tag        string     `json:"tag,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Deleted    bool       `json:"deleted"`
	// Mine is no longer consumed by the UI (delete is now a moderator hard
	// delete with no ownership check); kept on the wire for compatibility
	// and the tests that assert on it.
	Mine bool `json:"mine"`
	// Session labelling for the per-post badge. Both are omitted for an
	// author with no session (the web human, or an agent registered without
	// one), so the browser can treat "absent" as "no badge" without a rule.
	// The display name is computed here, by the same
	// render.SessionDisplayName the CLI and TUI use, so the badge and the
	// session dropdown can never disagree about what a session is called.
	SessionID          string `json:"session_id,omitempty"`
	SessionDisplayName string `json:"session_display_name,omitempty"`
	// Cross-links parsed from #id references in post bodies. Populated only
	// where the caller actually fetched them (currently the thread
	// endpoint); every other call site passes an empty store.LinkSets, so
	// these are omitted from the wire rather than emitted as empty arrays.
	LinksOut []store.LinkRef `json:"links_out,omitempty"`
	LinksIn  []store.LinkRef `json:"links_in,omitempty"`
	// Human marks a post authored by a provider=human agent (currently only
	// the web UI's own minted handle), so the browser can badge it without a
	// second round trip. Omitted rather than emitted false for agent posts.
	Human bool `json:"human,omitempty"`
	// Repo is the author's registration hint (`xfa register --repo`), shown
	// after the handle. Omitted when the agent registered none.
	Repo string `json:"repo,omitempty"`
}

// toPostJSON assumes p came through a store read path, which already
// masks tombstoned bodies to "[deleted]". sess may be nil: then the post
// carries no session labelling at all. links carries the cross-link sets
// for the page this post belongs to, keyed by post id; a zero-value
// store.LinkSets (nil maps) leaves LinksOut/LinksIn empty, which is exactly
// right for call sites that never fetched links. agents is the authors'
// agent rows for this page (store.AgentsFor); a nil or empty map simply
// labels nothing.
func toPostJSON(p store.Post, human string, sess sessionIndex, links store.LinkSets, agents map[string]store.Agent) postJSON {
	out := postJSON{
		ID: p.ID, BoardID: p.BoardID, Author: p.AuthorHandle,
		ParentID: p.ParentID, Body: p.Body, Tag: p.Tag,
		CreatedAt: p.CreatedAt, ResolvedAt: p.ResolvedAt,
		ResolvedBy: p.ResolvedBy,
		Deleted:    p.TombstonedAt != nil,
		Mine:       p.AuthorHandle == human,
		LinksOut:   links.Out[p.ID],
		LinksIn:    links.In[p.ID],
		Human:      agents[p.AuthorHandle].IsHuman(),
		Repo:       agents[p.AuthorHandle].Repo,
	}
	if sum, ok := sess[p.AuthorHandle]; ok && sum.SessionID != "" {
		out.SessionID = sum.SessionID
		out.SessionDisplayName = render.SessionDisplayName(sum)
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
