package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
)

// sessionNameJSON answers a rename: the id the client addressed, the name as
// the store actually kept it (trimmed), and the same display label the
// listing endpoint would compute for it, so the caller can update a row
// without a refetch.
type sessionNameJSON struct {
	SessionID   string `json:"session_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// writeErrMapped maps store errors from the write paths onto status codes.
// Unlike readErr there is no 500 default: every remaining store error on a
// write is a validation refusal (rune cap, tag rules, tagged reply,
// already-resolved, not-a-question) whose message is the UX copy the CLI
// shows, so it is passed through to the client as a 400.
func writeErrMapped(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNoBoard), errors.Is(err, store.ErrNoPost), errors.Is(err, store.ErrNoSession):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrNotOwner):
		// No current web write path actually produces ErrNotOwner (web delete
		// is a moderator hard delete with no ownership check); kept for
		// defensive mapper completeness.
		writeErr(w, http.StatusForbidden, err)
	default:
		writeErr(w, http.StatusBadRequest, err)
	}
}

// pathID parses the {id} path value.
func pathID(r *http.Request) (uint, error) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	return uint(id), err
}

// decodeBody reads the JSON request body into v, answering 400 itself and
// reporting whether the handler may continue. The body is capped at 1MiB
// before decoding so an oversized request can't be read into memory whole.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("malformed JSON body"))
		return false
	}
	return true
}

// idFromPath parses the {id} path value, answering 400 itself and reporting
// whether the handler may continue.
func idFromPath(w http.ResponseWriter, r *http.Request) (uint, bool) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("post id must be a number"))
		return 0, false
	}
	return id, true
}

// registerWriteRoutes wires every mutating endpoint of the JSON API. The
// author of every write is human, the server-side handle: the request body
// never gets a say in who a post belongs to.
func registerWriteRoutes(mux *http.ServeMux, s *store.Store, human string) {
	mux.HandleFunc("POST /api/posts", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Board, Body, Tag string }
		if !decodeBody(w, r, &in) {
			return
		}
		b, err := s.GetBoardBySlug(in.Board)
		if err != nil {
			writeErrMapped(w, err)
			return
		}
		p, err := s.CreatePost(b.ID, human, in.Body, in.Tag, nil)
		if err != nil {
			writeErrMapped(w, err)
			return
		}
		// nil session index: the author is always the web human, which is
		// registered with no session id and so is never labelled. The write
		// endpoints always author as the web human, so the human set is a
		// hardcoded one-entry map rather than a store round trip.
		writeJSON(w, http.StatusCreated, toPostJSON(*p, human, nil, store.LinkSets{}, map[string]bool{human: true}))
	})

	mux.HandleFunc("POST /api/posts/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		id, ok := idFromPath(w, r)
		if !ok {
			return
		}
		var in struct{ Body string }
		if !decodeBody(w, r, &in) {
			return
		}
		// The board is inherited from the parent, never taken from the
		// client: a reply belongs wherever its thread lives.
		parent, err := s.GetPost(id)
		if err != nil {
			writeErrMapped(w, err)
			return
		}
		p, err := s.CreatePost(parent.BoardID, human, in.Body, "", &parent.ID)
		if err != nil {
			writeErrMapped(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toPostJSON(*p, human, nil, store.LinkSets{}, map[string]bool{human: true}))
	})

	mux.HandleFunc("POST /api/posts/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		id, ok := idFromPath(w, r)
		if !ok {
			return
		}
		if err := s.Resolve(id, human); err != nil {
			writeErrMapped(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// Naming a session is not authored by anyone: a name is a shared label,
	// so — like resolve on the CLI — anyone may set it, and the human handle
	// is not recorded against it.
	mux.HandleFunc("POST /api/sessions/{id}/name", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !decodeBody(w, r, &in) {
			return
		}
		sessionID := r.PathValue("id")
		if err := s.SetSessionName(sessionID, in.Name); err != nil {
			// Every refusal here is validation (empty id, empty or oversize
			// name), which writeErrMapped already answers as a 400 carrying
			// the store's own copy.
			writeErrMapped(w, err)
			return
		}
		// Echo what was stored, not what was sent: the store trims, and the
		// UI writes the response straight into its dropdown.
		name, err := s.GetSessionName(sessionID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, sessionNameJSON{
			SessionID:   sessionID,
			Name:        name,
			DisplayName: render.SessionDisplayName(store.SessionSummary{SessionID: sessionID, Name: name}),
		})
	})

	// Hard delete, not tombstone: the web server is human-only and this is a
	// moderator power, reachable by no other surface. It removes the post and
	// its entire reply subtree, any author included — there is no ownership
	// check here the way the CLI's own-post-only delete has.
	mux.HandleFunc("DELETE /api/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := idFromPath(w, r)
		if !ok {
			return
		}
		deleted, err := s.HardDeletePost(id)
		if err != nil {
			writeErrMapped(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
	})

	// Another moderator power: wipes every post authored by sessionID's
	// agents (including foreign replies nested under them) and forgets the
	// session's name. A known-but-postless session id is a valid 200 with
	// deleted:0, not an error.
	mux.HandleFunc("DELETE /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		deleted, err := s.HardDeleteSession(sessionID)
		if err != nil {
			writeErrMapped(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
	})
}
