package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// seedWeb: one board, one agent, a question thread with one reply, and a
// tombstoned post. Returns handler, board, root post, reply.
func seedWeb(t *testing.T) (http.Handler, *store.Store, *store.Board, *store.Post, *store.Post) {
	t.Helper()
	s := openTemp(t)
	b, err := s.EnsureBoard("general", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.RegisterAgent("claude", "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.CreatePost(b.ID, a.Handle, "how do I frobnicate?", "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.CreatePost(b.ID, a.Handle, "you frob the nicate", "", &root.ID)
	if err != nil {
		t.Fatal(err)
	}
	dead, err := s.CreatePost(b.ID, a.Handle, "delete me", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(dead.ID, a.Handle); err != nil {
		t.Fatal(err)
	}
	return NewHandler(s, "human-tester-1", b.Slug), s, b, root, reply
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, rec.Body.Bytes()
}

func TestReadEndpoints(t *testing.T) {
	h, _, b, root, _ := seedWeb(t)

	rec, body := get(t, h, "/api/me")
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, body)
	}
	var me map[string]string
	json.Unmarshal(body, &me)
	if me["handle"] != "human-tester-1" || me["board"] != b.Slug {
		t.Fatalf("me payload: %v", me)
	}

	rec, body = get(t, h, "/api/boards")
	var boards []map[string]any
	json.Unmarshal(body, &boards)
	if rec.Code != 200 || len(boards) != 1 || boards[0]["posts"].(float64) != 3 {
		t.Fatalf("boards: %d %s", rec.Code, body)
	}

	rec, body = get(t, h, "/api/boards/"+b.Slug+"/threads")
	var threads []struct {
		Root    postJSON `json:"root"`
		Replies int      `json:"replies"`
	}
	json.Unmarshal(body, &threads)
	if rec.Code != 200 || len(threads) != 2 {
		t.Fatalf("threads: %d %s", rec.Code, body)
	}

	rec, body = get(t, h, fmt.Sprintf("/api/threads/%d", root.ID))
	var thread []postJSON
	json.Unmarshal(body, &thread)
	if rec.Code != 200 || len(thread) != 2 || thread[0].ID != root.ID || thread[0].Mine {
		t.Fatalf("thread: %d %s", rec.Code, body)
	}

	if rec, _ := get(t, h, "/api/threads/99999"); rec.Code != 404 {
		t.Fatalf("missing thread: %d", rec.Code)
	}
	if rec, _ := get(t, h, "/api/boards/nope/threads"); rec.Code != 404 {
		t.Fatalf("missing board: %d", rec.Code)
	}

	rec, body = get(t, h, "/api/search?q=frobnicate")
	var hits []postJSON
	json.Unmarshal(body, &hits)
	if rec.Code != 200 || len(hits) == 0 {
		t.Fatalf("search: %d %s", rec.Code, body)
	}
	if rec, _ = get(t, h, "/api/search?q="); rec.Code != 400 {
		t.Fatalf("empty search: %d", rec.Code)
	}

	rec, body = get(t, h, "/api/questions?board="+b.Slug)
	var qs []map[string]any
	json.Unmarshal(body, &qs)
	if rec.Code != 200 || len(qs) != 1 || qs[0]["replies"].(float64) != 1 {
		t.Fatalf("questions: %d %s", rec.Code, body)
	}

	rec, body = get(t, h, "/api/stats")
	var st map[string]any
	json.Unmarshal(body, &st)
	if rec.Code != 200 || st["posts"].(float64) != 3 {
		t.Fatalf("stats: %d %s", rec.Code, body)
	}

	if rec, _ = get(t, h, "/api/inbox"); rec.Code != 200 {
		t.Fatalf("inbox: %d", rec.Code)
	}
}

// TestThreadEndpointCarriesLinks pins the wire shape for #id cross-links: the
// thread endpoint is the one call site that actually fetches store.LinkSets,
// so a post's links_out/links_in should show up there, while every other
// endpoint (which passes an empty store.LinkSets{}) keeps omitting the
// fields entirely rather than emitting empty arrays.
func TestThreadEndpointCarriesLinks(t *testing.T) {
	h, s, b, rootA, _ := seedWeb(t)

	a, err := s.RegisterAgent("claude", "sess-2", "")
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := s.CreatePost(b.ID, a.Handle, fmt.Sprintf("see #%d", rootA.ID), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	rec, body := get(t, h, fmt.Sprintf("/api/threads/%d", rootA.ID))
	if rec.Code != 200 {
		t.Fatalf("thread A: %d %s", rec.Code, body)
	}
	var threadA []postJSON
	if err := json.Unmarshal(body, &threadA); err != nil {
		t.Fatalf("decode thread A: %v (%s)", err, body)
	}
	if len(threadA) == 0 || threadA[0].ID != rootA.ID {
		t.Fatalf("thread A root not found: %s", body)
	}
	wantIn := []store.LinkRef{{PostID: rootB.ID, ThreadID: rootB.ID, BoardSlug: b.Slug}}
	if len(threadA[0].LinksIn) != 1 || threadA[0].LinksIn[0] != wantIn[0] {
		t.Fatalf("A.links_in = %+v, want %+v", threadA[0].LinksIn, wantIn)
	}
	if len(threadA[0].LinksOut) != 0 {
		t.Fatalf("A.links_out = %+v, want empty", threadA[0].LinksOut)
	}

	rec, body = get(t, h, fmt.Sprintf("/api/threads/%d", rootB.ID))
	if rec.Code != 200 {
		t.Fatalf("thread B: %d %s", rec.Code, body)
	}
	var threadB []postJSON
	if err := json.Unmarshal(body, &threadB); err != nil {
		t.Fatalf("decode thread B: %v (%s)", err, body)
	}
	if len(threadB) == 0 || threadB[0].ID != rootB.ID {
		t.Fatalf("thread B root not found: %s", body)
	}
	if len(threadB[0].LinksOut) != 1 ||
		threadB[0].LinksOut[0].PostID != rootA.ID ||
		threadB[0].LinksOut[0].ThreadID != rootA.ID {
		t.Fatalf("B.links_out = %+v, want post_id/thread_id == %d", threadB[0].LinksOut, rootA.ID)
	}

	// Every other endpoint passes an empty store.LinkSets{}: links fields are
	// absent (omitempty), not present-but-empty — assert it still decodes.
	rec, body = get(t, h, "/api/boards/"+b.Slug+"/threads")
	if rec.Code != 200 {
		t.Fatalf("boards/threads: %d %s", rec.Code, body)
	}
	var threads []struct {
		Root postJSON `json:"root"`
	}
	if err := json.Unmarshal(body, &threads); err != nil {
		t.Fatalf("decode boards/threads: %v (%s)", err, body)
	}
	if strings.Contains(string(body), "links_out") || strings.Contains(string(body), "links_in") {
		t.Fatalf("boards/threads should omit links fields entirely, got %s", body)
	}
}

// A reply id is not a thread root: Thread() would happily render the reply's
// subtree, which reads as a truncated thread. The API must refuse it.
func TestThreadRejectsReplyIDAndGarbage(t *testing.T) {
	h, _, _, _, reply := seedWeb(t)

	if rec, body := get(t, h, fmt.Sprintf("/api/threads/%d", reply.ID)); rec.Code != 404 {
		t.Fatalf("reply id should 404, got %d %s", rec.Code, body)
	}
	if rec, body := get(t, h, "/api/threads/not-a-number"); rec.Code != 400 {
		t.Fatalf("garbage id should 400, got %d %s", rec.Code, body)
	}
}

// Every list endpoint must serialize an empty result as [], never null:
// the UI iterates these without null checks.
func TestEmptyListsSerializeAsArrays(t *testing.T) {
	s := openTemp(t)
	b, err := s.EnsureBoard("empty", "")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, "human-tester-1", b.Slug)

	for _, path := range []string{
		"/api/boards/" + b.Slug + "/threads",
		"/api/boards/" + b.Slug + "/board",
		"/api/search?q=nothingmatchesthis",
		"/api/questions",
		"/api/inbox",
		"/api/myposts",
	} {
		rec, body := get(t, h, path)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, body)
		}
		if got := strings.TrimSpace(string(body)); got != "[]" {
			t.Fatalf("%s: want [], got %s", path, got)
		}
	}

	rec, body := get(t, h, "/api/stats")
	var st struct {
		TopPosters []posterJSON `json:"top_posters"`
	}
	if err := json.Unmarshal(body, &st); err != nil || rec.Code != 200 {
		t.Fatalf("stats: %d %s (%v)", rec.Code, body, err)
	}
	if st.TopPosters == nil {
		t.Fatalf("top_posters serialized as null: %s", body)
	}
}

func TestThreadsLimitTruncates(t *testing.T) {
	h, _, b, _, _ := seedWeb(t)
	_, body := get(t, h, "/api/boards/"+b.Slug+"/threads?limit=1")
	var threads []threadJSON
	if err := json.Unmarshal(body, &threads); err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("limit=1 should yield 1 thread, got %d: %s", len(threads), body)
	}
}

// Binding constraint: read endpoints never advance a read cursor. Browsing
// the web UI must not consume an agent's — or the human's — unread queue.
func TestReadEndpointsDoNotMoveCursors(t *testing.T) {
	h, s, b, root, _ := seedWeb(t)
	for _, path := range []string{
		"/api/boards",
		"/api/boards/" + b.Slug + "/threads",
		"/api/boards/" + b.Slug + "/board",
		fmt.Sprintf("/api/threads/%d", root.ID),
		"/api/search?q=frobnicate",
		"/api/questions",
		"/api/inbox",
		"/api/myposts",
		"/api/stats",
	} {
		if rec, body := get(t, h, path); rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, body)
		}
	}
	var cursors int64
	if err := s.DB.Model(&store.ReadCursor{}).Count(&cursors).Error; err != nil {
		t.Fatal(err)
	}
	if cursors != 0 {
		t.Fatalf("read endpoints wrote %d read cursor(s)", cursors)
	}
}

// wireSession pins the /api/sessions wire shape independently of the
// handler's own struct: the field names below are the contract the web UI
// codes against, so a rename in the handler must fail here.
type wireSession struct {
	SessionID    string    `json:"session_id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name"`
	LeadHandle   string    `json:"lead_handle"`
	Posts        int64     `json:"posts"`
	StartedAt    time.Time `json:"started_at"`
	LastActivity time.Time `json:"last_activity"`
}

// sessionFixture: one board with two sessions plus a sessionless agent.
// alpha owns root1; beta replies to it and owns root2; the sessionless agent
// owns root3. So alpha matches one thread (returned whole, beta's reply
// included), beta matches two, and root3's thread matches neither — while the
// unfiltered view still shows all three.
type sessionFixture struct {
	h      http.Handler
	s      *store.Store
	board  *store.Board
	alpha  *store.Agent
	beta   *store.Agent
	loner  *store.Agent
	root1  *store.Post
	reply1 *store.Post
	root2  *store.Post
	root3  *store.Post
}

func seedSessions(t *testing.T) sessionFixture {
	t.Helper()
	s := openTemp(t)
	b, err := s.EnsureBoard("general", "")
	if err != nil {
		t.Fatal(err)
	}
	mkAgent := func(session string) *store.Agent {
		a, err := s.RegisterAgent("claude", session, "")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	alpha, beta, loner := mkAgent("sess-alpha"), mkAgent("sess-beta"), mkAgent("")
	mkPost := func(a *store.Agent, body string, parent *uint) *store.Post {
		p, err := s.CreatePost(b.ID, a.Handle, body, "", parent)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	root1 := mkPost(alpha, "alpha opens a thread", nil)
	reply1 := mkPost(beta, "beta replies to alpha", &root1.ID)
	root2 := mkPost(beta, "beta opens its own thread", nil)
	root3 := mkPost(loner, "no session here", nil)

	return sessionFixture{
		h: NewHandler(s, "human-tester-1", b.Slug), s: s, board: b,
		alpha: alpha, beta: beta, loner: loner,
		root1: root1, reply1: reply1, root2: root2, root3: root3,
	}
}

func decodeSessions(t *testing.T, body []byte) []wireSession {
	t.Helper()
	var out []wireSession
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode sessions: %v (%s)", err, body)
	}
	return out
}

func findSession(t *testing.T, list []wireSession, id string) wireSession {
	t.Helper()
	for _, s := range list {
		if s.SessionID == id {
			return s
		}
	}
	t.Fatalf("session %q not in %+v", id, list)
	return wireSession{}
}

func TestSessionsEndpoint(t *testing.T) {
	f := seedSessions(t)

	rec, body := get(t, f.h, "/api/sessions")
	if rec.Code != 200 {
		t.Fatalf("sessions: %d %s", rec.Code, body)
	}
	list := decodeSessions(t, body)
	if len(list) != 2 {
		t.Fatalf("want the two real sessions, got %d: %s", len(list), body)
	}
	// The sessionless agent is not a session and must never be listed.
	for _, s := range list {
		if s.SessionID == "" {
			t.Fatalf("empty session id surfaced: %s", body)
		}
	}
	// Ordering contract: most recently active first.
	if list[0].LastActivity.Before(list[1].LastActivity) {
		t.Fatalf("sessions not ordered by last_activity desc: %s", body)
	}

	alpha := findSession(t, list, "sess-alpha")
	if alpha.Name != "" {
		t.Fatalf("unnamed session should carry an empty name: %+v", alpha)
	}
	if alpha.LeadHandle != f.alpha.Handle || alpha.Posts != 1 {
		t.Fatalf("alpha summary: %+v", alpha)
	}
	if alpha.StartedAt.IsZero() || alpha.LastActivity.IsZero() {
		t.Fatalf("alpha timestamps: %+v", alpha)
	}
	// Pinned fallback format: lead-handle · YYYY-MM-DD · first-8-of-id.
	wantFallback := fmt.Sprintf("%s · %s · %s",
		f.alpha.Handle, alpha.StartedAt.Format("2006-01-02"), "sess-alp")
	if alpha.DisplayName != wantFallback {
		t.Fatalf("display_name fallback: got %q want %q", alpha.DisplayName, wantFallback)
	}
	if beta := findSession(t, list, "sess-beta"); beta.Posts != 2 {
		t.Fatalf("beta should count its reply and its root: %+v", beta)
	}

	// A stored name wins over the fallback.
	if err := f.s.SetSessionName("sess-alpha", "refactor pass"); err != nil {
		t.Fatal(err)
	}
	_, body = get(t, f.h, "/api/sessions")
	alpha = findSession(t, decodeSessions(t, body), "sess-alpha")
	if alpha.Name != "refactor pass" || alpha.DisplayName != "refactor pass" {
		t.Fatalf("named session: %+v", alpha)
	}
}

// ?board= follows the boardIDByQuery convention: absent or empty means all
// boards, a known slug scopes, an unknown slug 404s.
func TestSessionsBoardScoping(t *testing.T) {
	f := seedSessions(t)
	other, err := f.s.EnsureBoard("other", "")
	if err != nil {
		t.Fatal(err)
	}
	gamma, err := f.s.RegisterAgent("claude", "sess-gamma", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreatePost(other.ID, gamma.Handle, "over here", "", nil); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, f.h, "/api/sessions?board="+other.Slug)
	list := decodeSessions(t, body)
	if len(list) != 1 || list[0].SessionID != "sess-gamma" {
		t.Fatalf("board scoping: %s", body)
	}

	_, body = get(t, f.h, "/api/sessions?board=")
	if len(decodeSessions(t, body)) != 3 {
		t.Fatalf("empty board param should mean all boards: %s", body)
	}

	if rec, _ := get(t, f.h, "/api/sessions?board=nope"); rec.Code != 404 {
		t.Fatalf("unknown board: %d", rec.Code)
	}
}

func TestSessionsEmptyListSerializesAsArray(t *testing.T) {
	s := openTemp(t)
	b, err := s.EnsureBoard("empty", "")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, "human-tester-1", b.Slug)
	rec, body := get(t, h, "/api/sessions")
	if rec.Code != 200 {
		t.Fatalf("sessions: %d %s", rec.Code, body)
	}
	if got := strings.TrimSpace(string(body)); got != "[]" {
		t.Fatalf("want [], got %s", got)
	}
}

// ?session= filters threads and board views with participated semantics: a
// matched thread comes back whole, other sessions' replies included.
func TestThreadsAndBoardSessionFilter(t *testing.T) {
	f := seedSessions(t)

	rec, body := get(t, f.h, "/api/boards/"+f.board.Slug+"/threads?session=sess-alpha")
	if rec.Code != 200 {
		t.Fatalf("threads: %d %s", rec.Code, body)
	}
	var threads []threadJSON
	if err := json.Unmarshal(body, &threads); err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].Root.ID != f.root1.ID || threads[0].Replies != 1 {
		t.Fatalf("alpha threads: %s", body)
	}

	// Beta participated in both threads (a reply in one, a root in the other).
	_, body = get(t, f.h, "/api/boards/"+f.board.Slug+"/threads?session=sess-beta")
	json.Unmarshal(body, &threads)
	if len(threads) != 2 {
		t.Fatalf("beta threads: %s", body)
	}

	rec, body = get(t, f.h, "/api/boards/"+f.board.Slug+"/board?session=sess-alpha")
	if rec.Code != 200 {
		t.Fatalf("board: %d %s", rec.Code, body)
	}
	var groups [][]postJSON
	if err := json.Unmarshal(body, &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Fatalf("alpha board view should be one whole thread: %s", body)
	}
	if groups[0][0].ID != f.root1.ID || groups[0][1].ID != f.reply1.ID {
		t.Fatalf("matched thread must come back whole: %s", body)
	}

	// An unknown session matches nothing — 200 with an empty array, not a 500.
	for _, path := range []string{
		"/api/boards/" + f.board.Slug + "/threads?session=nope",
		"/api/boards/" + f.board.Slug + "/board?session=nope",
	} {
		rec, body := get(t, f.h, path)
		if rec.Code != 200 || strings.TrimSpace(string(body)) != "[]" {
			t.Fatalf("%s: %d %s", path, rec.Code, body)
		}
	}

	// Session filtering composes with ?limit=.
	_, body = get(t, f.h, "/api/boards/"+f.board.Slug+"/threads?session=sess-beta&limit=1")
	json.Unmarshal(body, &threads)
	if len(threads) != 1 {
		t.Fatalf("limit with session filter: %s", body)
	}
}

// The "All sessions" default: an absent or empty ?session= must run the
// existing unfiltered path byte for byte, including the sessionless agent's
// thread.
func TestEmptySessionParamIsUnfiltered(t *testing.T) {
	f := seedSessions(t)

	for _, suffix := range []string{"threads", "board"} {
		base := "/api/boards/" + f.board.Slug + "/" + suffix
		recA, absent := get(t, f.h, base)
		recB, empty := get(t, f.h, base+"?session=")
		if recA.Code != 200 || recB.Code != 200 {
			t.Fatalf("%s: %d/%d", suffix, recA.Code, recB.Code)
		}
		if string(absent) != string(empty) {
			t.Fatalf("%s: empty session param diverged\nabsent: %s\nempty:  %s", suffix, absent, empty)
		}
	}

	_, body := get(t, f.h, "/api/boards/"+f.board.Slug+"/threads")
	var threads []threadJSON
	json.Unmarshal(body, &threads)
	if len(threads) != 3 {
		t.Fatalf("unfiltered view must include the sessionless thread: %s", body)
	}
}

// Session-scoped reads are still reads: no cursor may move.
func TestSessionReadsDoNotMoveCursors(t *testing.T) {
	f := seedSessions(t)
	for _, path := range []string{
		"/api/sessions",
		"/api/sessions?board=" + f.board.Slug,
		"/api/boards/" + f.board.Slug + "/threads?session=sess-alpha",
		"/api/boards/" + f.board.Slug + "/board?session=sess-alpha",
	} {
		if rec, body := get(t, f.h, path); rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, body)
		}
	}
	var cursors int64
	if err := f.s.DB.Model(&store.ReadCursor{}).Count(&cursors).Error; err != nil {
		t.Fatal(err)
	}
	if cursors != 0 {
		t.Fatalf("session reads wrote %d read cursor(s)", cursors)
	}
}

func TestTombstoneMaskedInThreadList(t *testing.T) {
	h, _, b, _, _ := seedWeb(t)
	_, body := get(t, h, "/api/boards/"+b.Slug+"/board")
	var groups [][]postJSON
	json.Unmarshal(body, &groups)
	found := false
	for _, g := range groups {
		for _, p := range g {
			if p.Deleted {
				found = true
				if p.Body != "[deleted]" {
					t.Fatalf("tombstone body leaked: %q", p.Body)
				}
			}
		}
	}
	if !found {
		t.Fatal("no tombstoned post surfaced")
	}
}

// wirePost pins the additive per-post session labelling the UI badges read.
type wirePost struct {
	ID                 uint   `json:"id"`
	Author             string `json:"author"`
	SessionID          string `json:"session_id"`
	SessionDisplayName string `json:"session_display_name"`
	Human              bool   `json:"human"`
}

func findPost(t *testing.T, groups [][]wirePost, id uint) wirePost {
	t.Helper()
	for _, g := range groups {
		for _, p := range g {
			if p.ID == id {
				return p
			}
		}
	}
	t.Fatalf("post %d not in %+v", id, groups)
	return wirePost{}
}

// TestPostsCarrySessionLabels: every post the API hands back names its
// author's session, so a card can badge it without a second round trip. A
// named session shows its name; an unnamed one shows the shared fallback
// label; a sessionless author carries neither field.
func TestPostsCarrySessionLabels(t *testing.T) {
	f := seedSessions(t)
	if err := f.s.SetSessionName("sess-alpha", "parser work"); err != nil {
		t.Fatal(err)
	}

	rec, body := get(t, f.h, "/api/boards/general/board")
	if rec.Code != 200 {
		t.Fatalf("board: %d %s", rec.Code, body)
	}
	var groups [][]wirePost
	if err := json.Unmarshal(body, &groups); err != nil {
		t.Fatalf("decode board: %v (%s)", err, body)
	}

	named := findPost(t, groups, f.root1.ID)
	if named.SessionID != "sess-alpha" || named.SessionDisplayName != "parser work" {
		t.Errorf("alpha post labels = %q/%q, want sess-alpha/parser work",
			named.SessionID, named.SessionDisplayName)
	}
	// A reply is labelled by its own author, not by the thread's root.
	reply := findPost(t, groups, f.reply1.ID)
	if reply.SessionID != "sess-beta" {
		t.Errorf("reply session = %q, want sess-beta (the reply's author)", reply.SessionID)
	}
	if reply.SessionDisplayName == "" || !strings.Contains(reply.SessionDisplayName, f.beta.Handle) {
		t.Errorf("unnamed session label = %q, want the lead-handle fallback", reply.SessionDisplayName)
	}
	loner := findPost(t, groups, f.root3.ID)
	if loner.SessionID != "" || loner.SessionDisplayName != "" {
		t.Errorf("sessionless post labels = %q/%q, want both empty",
			loner.SessionID, loner.SessionDisplayName)
	}
	// omitempty, not an empty string: the badge must not render at all.
	if strings.Contains(string(body), `"session_id":""`) {
		t.Errorf("sessionless post emitted an empty session_id: %s", body)
	}

	// The same labels reach the thread list and the thread detail.
	rec, body = get(t, f.h, "/api/boards/general/threads")
	if rec.Code != 200 {
		t.Fatalf("threads: %d %s", rec.Code, body)
	}
	var threads []struct {
		Root wirePost `json:"root"`
	}
	if err := json.Unmarshal(body, &threads); err != nil {
		t.Fatalf("decode threads: %v (%s)", err, body)
	}
	var seen bool
	for _, th := range threads {
		if th.Root.ID == f.root1.ID {
			seen = true
			if th.Root.SessionDisplayName != "parser work" {
				t.Errorf("thread root label = %q, want parser work", th.Root.SessionDisplayName)
			}
		}
	}
	if !seen {
		t.Fatalf("root1 missing from the thread list: %s", body)
	}

	rec, body = get(t, f.h, fmt.Sprintf("/api/threads/%d", f.root1.ID))
	if rec.Code != 200 {
		t.Fatalf("thread: %d %s", rec.Code, body)
	}
	var posts []wirePost
	if err := json.Unmarshal(body, &posts); err != nil {
		t.Fatalf("decode thread: %v (%s)", err, body)
	}
	if len(posts) == 0 || posts[0].SessionDisplayName != "parser work" {
		t.Errorf("thread detail labels: %s", body)
	}
}

// TestPostsMarkHumanAuthor pins the human badge data: only posts authored by
// the web human handle (the provider=human agent EnsureHumanHandle mints)
// carry "human":true. Agent-authored posts omit the field entirely, per the
// omitempty pin the rest of postJSON already honours.
func TestPostsMarkHumanAuthor(t *testing.T) {
	s := openTemp(t)
	b, err := s.EnsureBoard("general", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.RegisterAgent("claude", "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	human, err := EnsureHumanHandle(s)
	if err != nil {
		t.Fatal(err)
	}
	agentPost, err := s.CreatePost(b.ID, a.Handle, "an agent post", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	humanPost, err := s.CreatePost(b.ID, human, "a human post", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(s, human, b.Slug)

	rec, body := get(t, h, "/api/boards/"+b.Slug+"/threads")
	if rec.Code != 200 {
		t.Fatalf("threads: %d %s", rec.Code, body)
	}
	var threads []struct {
		Root wirePost `json:"root"`
	}
	if err := json.Unmarshal(body, &threads); err != nil {
		t.Fatalf("decode threads: %v (%s)", err, body)
	}
	var sawAgent, sawHuman bool
	for _, th := range threads {
		switch th.Root.ID {
		case agentPost.ID:
			sawAgent = true
			if th.Root.Human {
				t.Errorf("agent post marked human")
			}
		case humanPost.ID:
			sawHuman = true
			if !th.Root.Human {
				t.Errorf("human post not marked human")
			}
		}
	}
	if !sawAgent || !sawHuman {
		t.Fatalf("missing posts in threads: agent=%v human=%v body=%s", sawAgent, sawHuman, body)
	}
	// omitempty, not a literal false: the agent post's field must be absent.
	if strings.Contains(string(body), `"human":false`) {
		t.Errorf(`wire shape should omit "human" rather than emit false, got: %s`, body)
	}

	rec, body = get(t, h, fmt.Sprintf("/api/threads/%d", humanPost.ID))
	if rec.Code != 200 {
		t.Fatalf("thread (human): %d %s", rec.Code, body)
	}
	var posts []wirePost
	if err := json.Unmarshal(body, &posts); err != nil {
		t.Fatalf("decode thread: %v (%s)", err, body)
	}
	if len(posts) != 1 || !posts[0].Human {
		t.Errorf("human thread detail not marked human: %s", body)
	}

	rec, body = get(t, h, fmt.Sprintf("/api/threads/%d", agentPost.ID))
	if rec.Code != 200 {
		t.Fatalf("thread (agent): %d %s", rec.Code, body)
	}
	posts = nil
	if err := json.Unmarshal(body, &posts); err != nil {
		t.Fatalf("decode thread: %v (%s)", err, body)
	}
	if len(posts) != 1 || posts[0].Human {
		t.Errorf("agent thread detail marked human: %s", body)
	}
}

func TestMyPostsEndpoint(t *testing.T) {
	s := openTemp(t)
	b, err := s.EnsureBoard("general", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.EnsureBoard("other", "")
	if err != nil {
		t.Fatal(err)
	}
	human, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.RegisterAgent("claude", "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	mine, _ := s.CreatePost(b.ID, human.Handle, "mine here", "", nil)
	elsewhere, _ := s.CreatePost(other.ID, human.Handle, "mine there", "", nil)
	_, _ = s.CreatePost(b.ID, agent.Handle, "not mine", "", nil)
	h := NewHandler(s, human.Handle, b.Slug)

	ids := func(path string) []uint {
		t.Helper()
		rec, body := get(t, h, path)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, body)
		}
		var posts []postJSON
		if err := json.Unmarshal(body, &posts); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out := []uint{}
		for _, p := range posts {
			out = append(out, p.ID)
		}
		return out
	}
	if got := ids("/api/myposts"); !reflect.DeepEqual(got, []uint{elsewhere.ID, mine.ID}) {
		t.Fatalf("all boards: %v", got)
	}
	if got := ids("/api/myposts?board=" + b.Slug); !reflect.DeepEqual(got, []uint{mine.ID}) {
		t.Fatalf("board-scoped: %v", got)
	}
	// Unknown board behaves like /api/stats?board=nope.
	want, _ := get(t, h, "/api/stats?board=nope")
	if rec, _ := get(t, h, "/api/myposts?board=nope"); rec.Code != want.Code {
		t.Fatalf("unknown board: %d, want %d", rec.Code, want.Code)
	}
}

func TestBoardPostsCarryProjectWhenShared(t *testing.T) {
	h, s, b, root, _ := seedWeb(t)
	dir := t.TempDir()
	if err := s.RegisterProject(dir, b.ID); err != nil {
		t.Fatal(err)
	}
	a, err := s.RegisterAgentAt(dir, "claude", "sess-proj", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "from ctf", "", &root.ID); err != nil {
		t.Fatal(err)
	}
	if _, raw := get(t, h, "/api/threads/"+strconv.Itoa(int(root.ID))); strings.Contains(string(raw), `"project`) {
		t.Fatalf("single-project DB must omit project fields: %s", raw)
	}
	if err := s.RegisterProject(t.TempDir(), b.ID); err != nil { // opens the gate
		t.Fatal(err)
	}
	_, raw := get(t, h, "/api/threads/"+strconv.Itoa(int(root.ID)))
	body := string(raw)
	if !strings.Contains(body, `"project_path":"`) || !strings.Contains(body, `"project":"`+filepath.Base(dir)+`"`) {
		t.Fatalf("thread JSON must carry project fields: %s", body)
	}
}
