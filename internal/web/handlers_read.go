package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
)

// defaultLimit caps every list endpoint that takes one. The web UI is a
// browse-and-skim surface, not an export tool.
const defaultLimit = 50

// readErr maps store errors onto status codes. A missing board or post is
// the caller asking for something that isn't there (404); everything else
// is ours (500).
func readErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNoBoard), errors.Is(err, store.ErrNoPost):
		writeErr(w, http.StatusNotFound, err)
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
}

// boardByPath resolves the {slug} path value.
func boardByPath(s *store.Store, r *http.Request) (*store.Board, error) {
	return s.GetBoardBySlug(r.PathValue("slug"))
}

// boardIDByQuery resolves the optional ?board= param. An absent or empty
// slug means "all boards", which the store spells as board id 0.
func boardIDByQuery(s *store.Store, r *http.Request) (uint, error) {
	slug := r.URL.Query().Get("board")
	if slug == "" {
		return 0, nil
	}
	b, err := s.GetBoardBySlug(slug)
	if err != nil {
		return 0, err
	}
	return b.ID, nil
}

// limitFromQuery reads ?limit=, falling back to defaultLimit for anything
// absent, unparseable, or non-positive — a bad limit narrows the view, it
// never fails the request.
func limitFromQuery(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return defaultLimit
	}
	return n
}

// boardPostsForRequest fetches a board's posts for the thread and board
// views, honouring the optional ?session= filter. An absent or empty param
// is the "All sessions" default and runs the existing unfiltered query
// untouched — an empty filter must never mean "match everything" implicitly,
// which is why the store refuses an empty session id outright.
func boardPostsForRequest(s *store.Store, r *http.Request, boardID uint) ([]store.Post, error) {
	if sid := r.URL.Query().Get("session"); sid != "" {
		return s.BoardPostsBySession(boardID, sid)
	}
	return s.BoardPosts(boardID)
}

// postsJSON maps a store slice to the wire shape, always non-nil so the
// client sees [] rather than null.
func postsJSON(posts []store.Post, human string, sess sessionIndex, links store.LinkSets, humans map[string]bool) []postJSON {
	out := make([]postJSON, 0, len(posts))
	for _, p := range posts {
		out = append(out, toPostJSON(p, human, sess, links, humans))
	}
	return out
}

// humanHandlesForPosts fetches the provider=human subset of posts' authors
// for this page, fail-soft to an empty map: a lookup failure should blank
// the human badge, not the whole read.
func humanHandlesForPosts(s *store.Store, posts []store.Post) map[string]bool {
	humans, err := s.HumanHandlesFor(store.HandleSet(posts))
	if err != nil {
		return map[string]bool{}
	}
	return humans
}

type boardJSON struct {
	ID          uint   `json:"id"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Posts       int64  `json:"posts"`
}

type threadJSON struct {
	Root         postJSON  `json:"root"`
	Replies      int       `json:"replies"`
	LastActivity time.Time `json:"last_activity"`
}

// questionJSON embeds postJSON unnamed so encoding/json flattens its fields
// alongside replies/asker_last_seen_at — one flat object, per the API shape.
type questionJSON struct {
	postJSON
	Replies         int64      `json:"replies"`
	AskerLastSeenAt *time.Time `json:"asker_last_seen_at,omitempty"`
}

// sessionJSON is one session picker row. display_name is computed here
// rather than in the browser so the CLI, the TUI and the web UI can never
// label the same session differently — render.SessionDisplayName is the one
// place that decision lives.
type sessionJSON struct {
	SessionID    string    `json:"session_id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name"`
	LeadHandle   string    `json:"lead_handle"`
	Posts        int64     `json:"posts"`
	StartedAt    time.Time `json:"started_at"`
	LastActivity time.Time `json:"last_activity"`
}

func toSessionJSON(sum store.SessionSummary) sessionJSON {
	return sessionJSON{
		SessionID:    sum.SessionID,
		Name:         sum.Name,
		DisplayName:  render.SessionDisplayName(sum),
		LeadHandle:   sum.LeadHandle,
		Posts:        sum.Posts,
		StartedAt:    sum.StartedAt,
		LastActivity: sum.LastActivity,
	}
}

type posterJSON struct {
	Handle string `json:"handle"`
	Count  int64  `json:"count"`
}

type statsJSON struct {
	Posts         int64        `json:"posts"`
	Posts24h      int64        `json:"posts_24h"`
	Agents        int64        `json:"agents"`
	OpenQuestions int64        `json:"open_questions"`
	TopPosters    []posterJSON `json:"top_posters"`
}

// registerReadRoutes wires every GET endpoint of the JSON API. These are
// strictly read-only: no read cursor is advanced and no agent liveness is
// touched, so opening the web UI never consumes an agent's unread queue.
func registerReadRoutes(mux *http.ServeMux, s *store.Store, human, initialBoard string) {
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"handle": human,
			"board":  initialBoard,
		})
	})

	mux.HandleFunc("GET /api/boards", func(w http.ResponseWriter, r *http.Request) {
		boards, err := s.ListBoards()
		if err != nil {
			readErr(w, err)
			return
		}
		counts, err := s.BoardPostCounts()
		if err != nil {
			readErr(w, err)
			return
		}
		out := make([]boardJSON, 0, len(boards))
		for _, b := range boards {
			out = append(out, boardJSON{
				ID: b.ID, Slug: b.Slug, Description: b.Description,
				Posts: counts[b.ID],
			})
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		boardID, err := boardIDByQuery(s, r)
		if err != nil {
			readErr(w, err)
			return
		}
		sessions, err := s.ListSessions(boardID)
		if err != nil {
			readErr(w, err)
			return
		}
		out := make([]sessionJSON, 0, len(sessions))
		for _, sum := range sessions {
			out = append(out, toSessionJSON(sum))
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/boards/{slug}/threads", func(w http.ResponseWriter, r *http.Request) {
		b, err := boardByPath(s, r)
		if err != nil {
			readErr(w, err)
			return
		}
		posts, err := boardPostsForRequest(s, r, b.ID)
		if err != nil {
			readErr(w, err)
			return
		}
		// Rebuilt per request rather than cached, so an agent that registered
		// a second ago is already labelled. Two queries, no post scan.
		sess, err := s.SessionsByHandle()
		if err != nil {
			readErr(w, err)
			return
		}
		humans := humanHandlesForPosts(s, posts)
		// Summarizing needs the whole board (a reply anywhere decides its
		// root's activity), so the limit is applied to the summaries, not
		// to the fetch.
		summaries := store.ThreadSummaries(posts)
		if limit := limitFromQuery(r); len(summaries) > limit {
			summaries = summaries[:limit]
		}
		out := make([]threadJSON, 0, len(summaries))
		for _, t := range summaries {
			out = append(out, threadJSON{
				Root:         toPostJSON(t.Root, human, sess, store.LinkSets{}, humans),
				Replies:      t.Replies,
				LastActivity: t.LastActivity,
			})
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/boards/{slug}/board", func(w http.ResponseWriter, r *http.Request) {
		b, err := boardByPath(s, r)
		if err != nil {
			readErr(w, err)
			return
		}
		posts, err := boardPostsForRequest(s, r, b.ID)
		if err != nil {
			readErr(w, err)
			return
		}
		sess, err := s.SessionsByHandle()
		if err != nil {
			readErr(w, err)
			return
		}
		humans := humanHandlesForPosts(s, posts)
		groups := store.GroupThreads(posts)
		out := make([][]postJSON, 0, len(groups))
		for _, g := range groups {
			out = append(out, postsJSON(g, human, sess, store.LinkSets{}, humans))
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/threads/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, strconv.IntSize)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("post id must be a number"))
			return
		}
		// Thread() root-resolves any id it is given, so a reply id would
		// render fine. Reject it anyway: the UI only ever links to roots, so
		// a reply id here is a client bug worth surfacing, not a subtree
		// request to quietly honor.
		p, err := s.GetPost(uint(id))
		if err != nil {
			readErr(w, err)
			return
		}
		if p.ParentID != nil {
			writeErr(w, http.StatusNotFound, errors.New("not a thread root"))
			return
		}
		posts, err := s.Thread(p.ID)
		if err != nil {
			readErr(w, err)
			return
		}
		sess, err := s.SessionsByHandle()
		if err != nil {
			readErr(w, err)
			return
		}
		ids := make([]uint, 0, len(posts))
		for _, p := range posts {
			ids = append(ids, p.ID)
		}
		links, err := s.LinksFor(ids)
		if err != nil {
			// Read stays fail-soft: a links lookup failure shouldn't blank out
			// an otherwise-good thread render.
			links = store.LinkSets{}
		}
		humans := humanHandlesForPosts(s, posts)
		writeJSON(w, http.StatusOK, postsJSON(posts, human, sess, links, humans))
	})

	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeErr(w, http.StatusBadRequest, errors.New("q is required"))
			return
		}
		boardID, err := boardIDByQuery(s, r)
		if err != nil {
			readErr(w, err)
			return
		}
		posts, err := s.Search(q, boardID, defaultLimit)
		if err != nil {
			readErr(w, err)
			return
		}
		sess, err := s.SessionsByHandle()
		if err != nil {
			readErr(w, err)
			return
		}
		humans := humanHandlesForPosts(s, posts)
		writeJSON(w, http.StatusOK, postsJSON(posts, human, sess, store.LinkSets{}, humans))
	})

	mux.HandleFunc("GET /api/questions", func(w http.ResponseWriter, r *http.Request) {
		boardID, err := boardIDByQuery(s, r)
		if err != nil {
			readErr(w, err)
			return
		}
		questions, err := s.OpenQuestions(boardID, defaultLimit)
		if err != nil {
			readErr(w, err)
			return
		}
		sess, err := s.SessionsByHandle()
		if err != nil {
			readErr(w, err)
			return
		}
		qPosts := make([]store.Post, 0, len(questions))
		for _, q := range questions {
			qPosts = append(qPosts, q.Post)
		}
		humans := humanHandlesForPosts(s, qPosts)
		out := make([]questionJSON, 0, len(questions))
		for _, q := range questions {
			out = append(out, questionJSON{
				postJSON:        toPostJSON(q.Post, human, sess, store.LinkSets{}, humans),
				Replies:         q.Replies,
				AskerLastSeenAt: q.AskerLastSeenAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/inbox", func(w http.ResponseWriter, r *http.Request) {
		posts, err := s.Inbox(human, defaultLimit)
		// Inbox refuses an unregistered handle so a typo'd `--as` on the CLI
		// can't read as "no news". The web handle is never typed — it is
		// minted and healed by EnsureHumanHandle — so here an unknown agent
		// genuinely means an empty inbox, not a mistake worth a 500.
		if errors.Is(err, store.ErrNoAgent) {
			writeJSON(w, http.StatusOK, []postJSON{})
			return
		}
		if err != nil {
			readErr(w, err)
			return
		}
		sess, err := s.SessionsByHandle()
		if err != nil {
			readErr(w, err)
			return
		}
		humans := humanHandlesForPosts(s, posts)
		writeJSON(w, http.StatusOK, postsJSON(posts, human, sess, store.LinkSets{}, humans))
	})

	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		boardID, err := boardIDByQuery(s, r)
		if err != nil {
			readErr(w, err)
			return
		}
		st, err := s.Stats(boardID)
		if err != nil {
			readErr(w, err)
			return
		}
		top := make([]posterJSON, 0, len(st.TopPosters))
		for _, p := range st.TopPosters {
			top = append(top, posterJSON{Handle: p.Handle, Count: p.Count})
		}
		writeJSON(w, http.StatusOK, statsJSON{
			Posts:         st.Posts,
			Posts24h:      st.Posts24h,
			Agents:        st.Agents,
			OpenQuestions: st.OpenQuestions,
			TopPosters:    top,
		})
	})
}
