package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, rec.Body.Bytes()
}

func TestWriteEndpoints(t *testing.T) {
	h, s, b, root, _ := seedWeb(t)
	// seedWeb's handler is built around a placeholder handle with no agent
	// row, which CreatePost would reject. EnsureHumanHandle mints and
	// registers the real web identity; rebuild the handler around it.
	human, err := EnsureHumanHandle(s)
	if err != nil {
		t.Fatal(err)
	}
	h = NewHandler(s, human, b.Slug)

	rec, body := do(t, h, "POST", "/api/posts", `{"board":"`+b.Slug+`","body":"hello from the web","tag":"til"}`)
	if rec.Code != 201 {
		t.Fatalf("post: %d %s", rec.Code, body)
	}
	var created postJSON
	json.Unmarshal(body, &created)
	if created.Author != human || !created.Mine || created.Tag != "til" {
		t.Fatalf("created: %+v", created)
	}

	rec, body = do(t, h, "POST", fmt.Sprintf("/api/posts/%d/reply", root.ID), `{"body":"web reply"}`)
	if rec.Code != 201 {
		t.Fatalf("reply: %d %s", rec.Code, body)
	}
	var rep postJSON
	json.Unmarshal(body, &rep)
	if rep.ParentID == nil || *rep.ParentID != root.ID || rep.BoardID != b.ID {
		t.Fatalf("reply threading: %+v", rep)
	}

	if rec, body = do(t, h, "POST", fmt.Sprintf("/api/posts/%d/resolve", root.ID), ""); rec.Code != 200 {
		t.Fatalf("resolve: %d %s", rec.Code, body)
	}

	// Human-authored posts resolve at any tag and any depth: an untagged
	// root, then the human's own reply to it.
	rec, body = do(t, h, "POST", "/api/posts", `{"board":"`+b.Slug+`","body":"please look at X"}`)
	if rec.Code != 201 {
		t.Fatalf("human untagged post: %d %s", rec.Code, body)
	}
	var hroot postJSON
	json.Unmarshal(body, &hroot)
	if rec, body = do(t, h, "POST", fmt.Sprintf("/api/posts/%d/resolve", hroot.ID), ""); rec.Code != 200 {
		t.Fatalf("resolve untagged human root: %d %s", rec.Code, body)
	}
	rec, body = do(t, h, "POST", fmt.Sprintf("/api/posts/%d/reply", hroot.ID), `{"body":"thanks, done"}`)
	if rec.Code != 201 {
		t.Fatalf("human reply: %d %s", rec.Code, body)
	}
	var hrep postJSON
	json.Unmarshal(body, &hrep)
	if rec, body = do(t, h, "POST", fmt.Sprintf("/api/posts/%d/resolve", hrep.ID), ""); rec.Code != 200 {
		t.Fatalf("resolve human reply: %d %s", rec.Code, body)
	}

	// Delete is a hard delete with no ownership check: it is a moderator
	// power reachable only through the human-gated web server, so deleting
	// the web human's own post and deleting an agent's foreign post both
	// succeed.
	if rec, _ = do(t, h, "DELETE", fmt.Sprintf("/api/posts/%d", created.ID), ""); rec.Code != 200 {
		t.Fatalf("delete own: %d", rec.Code)
	}
	if rec, body := do(t, h, "DELETE", fmt.Sprintf("/api/posts/%d", root.ID), ""); rec.Code != 200 {
		t.Fatalf("delete foreign: %d %s", rec.Code, body)
	}

	// validation surfaces: oversize body → 400, bad tag → 400, unknown board → 404
	if rec, _ = do(t, h, "POST", "/api/posts", `{"board":"`+b.Slug+`","body":"`+strings.Repeat("a", 2001)+`"}`); rec.Code != 400 {
		t.Fatalf("oversize: %d", rec.Code)
	}
	if rec, _ = do(t, h, "POST", "/api/posts", `{"board":"`+b.Slug+`","body":"x","tag":"Bad Tag"}`); rec.Code != 400 {
		t.Fatalf("bad tag: %d", rec.Code)
	}
	if rec, _ = do(t, h, "POST", "/api/posts", `{"board":"nope","body":"x"}`); rec.Code != 404 {
		t.Fatalf("unknown board: %d", rec.Code)
	}
	if rec, _ = do(t, h, "POST", "/api/posts", `not json`); rec.Code != 400 {
		t.Fatalf("bad json: %d", rec.Code)
	}
	// decodeBody caps the request body at 1MiB before decoding; a body past
	// that cap must 400 rather than being read into memory whole.
	if rec, _ = do(t, h, "POST", "/api/posts", `{"board":"`+b.Slug+`","body":"`+strings.Repeat("a", 1<<20+1)+`"}`); rec.Code != 400 {
		t.Fatalf("oversize request body: %d", rec.Code)
	}
}

// A reply or resolve aimed at a post that does not exist is the caller
// asking for something absent (404), while an unparsable id in the path is
// a malformed request (400).
func TestWriteMissingAndMalformedIDs(t *testing.T) {
	_, s, b, _, _ := seedWeb(t)
	human, err := EnsureHumanHandle(s)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, human, b.Slug)

	if rec, body := do(t, h, "POST", "/api/posts/9999/reply", `{"body":"hi"}`); rec.Code != 404 {
		t.Fatalf("reply to missing parent: %d %s", rec.Code, body)
	}
	if rec, body := do(t, h, "POST", "/api/posts/9999/resolve", ""); rec.Code != 404 {
		t.Fatalf("resolve missing: %d %s", rec.Code, body)
	}
	if rec, body := do(t, h, "DELETE", "/api/posts/9999", ""); rec.Code != 404 {
		t.Fatalf("delete missing: %d %s", rec.Code, body)
	}
	if rec, _ := do(t, h, "POST", "/api/posts/abc/reply", `{"body":"hi"}`); rec.Code != 400 {
		t.Fatalf("malformed reply id: %d", rec.Code)
	}
	if rec, _ := do(t, h, "POST", "/api/posts/abc/resolve", ""); rec.Code != 400 {
		t.Fatalf("malformed resolve id: %d", rec.Code)
	}
	if rec, _ := do(t, h, "DELETE", "/api/posts/abc", ""); rec.Code != 400 {
		t.Fatalf("malformed delete id: %d", rec.Code)
	}
}

func TestSessionRename(t *testing.T) {
	f := seedSessions(t)

	// The name is trimmed by the store, and the response echoes what was
	// actually stored — not what the client sent.
	rec, body := do(t, f.h, "POST", "/api/sessions/sess-alpha/name", `{"name":"  refactor pass  "}`)
	if rec.Code != 200 {
		t.Fatalf("rename: %d %s", rec.Code, body)
	}
	var out struct {
		SessionID   string `json:"session_id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.SessionID != "sess-alpha" || out.Name != "refactor pass" || out.DisplayName != "refactor pass" {
		t.Fatalf("rename payload: %+v (%s)", out, body)
	}
	stored, err := f.s.GetSessionName("sess-alpha")
	if err != nil || stored != "refactor pass" {
		t.Fatalf("stored name %q (%v)", stored, err)
	}

	// Rename is an upsert: naming again replaces.
	if rec, body = do(t, f.h, "POST", "/api/sessions/sess-alpha/name", `{"name":"second pass"}`); rec.Code != 200 {
		t.Fatalf("re-rename: %d %s", rec.Code, body)
	}
	if stored, _ = f.s.GetSessionName("sess-alpha"); stored != "second pass" {
		t.Fatalf("rename did not replace: %q", stored)
	}

	// The new name shows up on the listing endpoint.
	_, body = get(t, f.h, "/api/sessions")
	if got := findSession(t, decodeSessions(t, body), "sess-alpha"); got.Name != "second pass" {
		t.Fatalf("listing after rename: %+v", got)
	}
}

// Validation refusals from the store surface as 400s, with the store's own
// copy passed through — the same contract the other write endpoints keep.
func TestSessionRenameValidation(t *testing.T) {
	f := seedSessions(t)

	for name, payload := range map[string]string{
		"empty name":      `{"name":""}`,
		"whitespace name": `{"name":"   "}`,
		"missing field":   `{}`,
		"too long":        `{"name":"` + strings.Repeat("x", 61) + `"}`,
		"malformed json":  `not json`,
	} {
		if rec, body := do(t, f.h, "POST", "/api/sessions/sess-alpha/name", payload); rec.Code != 400 {
			t.Fatalf("%s should 400, got %d %s", name, rec.Code, body)
		}
	}
	// A refused rename must not have written anything.
	if stored, err := f.s.GetSessionName("sess-alpha"); err != nil || stored != "" {
		t.Fatalf("refused rename stored %q (%v)", stored, err)
	}

	// Exactly at the cap is fine.
	max := strings.Repeat("x", 60)
	if rec, body := do(t, f.h, "POST", "/api/sessions/sess-beta/name", `{"name":"`+max+`"}`); rec.Code != 200 {
		t.Fatalf("60-rune name should be accepted: %d %s", rec.Code, body)
	}

	// Naming is lazy: an id with no rows yet is nameable, same as the CLI.
	if rec, body := do(t, f.h, "POST", "/api/sessions/never-seen/name", `{"name":"future"}`); rec.Code != 200 {
		t.Fatalf("unknown session id: %d %s", rec.Code, body)
	}
}

// Rename sits behind the same guard stack as every other write.
func TestSessionRenameGuarded(t *testing.T) {
	f := seedSessions(t)

	req := httptest.NewRequest("POST", "/api/sessions/sess-alpha/name", strings.NewReader(`{"name":"nope"}`))
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("non-local host should 403, got %d", rec.Code)
	}

	req = httptest.NewRequest("POST", "/api/sessions/sess-alpha/name", strings.NewReader(`{"name":"nope"}`))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "https://evil.example.com")
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("non-local origin should 403, got %d", rec.Code)
	}

	if stored, _ := f.s.GetSessionName("sess-alpha"); stored != "" {
		t.Fatalf("guarded-away rename still wrote %q", stored)
	}
}

// The author of a write is always the server-side human handle: a
// client-supplied one is ignored, never trusted.
func TestWriteIgnoresClientSuppliedHandle(t *testing.T) {
	_, s, b, root, _ := seedWeb(t)
	human, err := EnsureHumanHandle(s)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, human, b.Slug)

	rec, body := do(t, h, "POST", "/api/posts",
		`{"board":"`+b.Slug+`","body":"whose post is this","as":"`+root.AuthorHandle+`","author":"`+root.AuthorHandle+`"}`)
	if rec.Code != 201 {
		t.Fatalf("post: %d %s", rec.Code, body)
	}
	var created postJSON
	json.Unmarshal(body, &created)
	if created.Author != human {
		t.Fatalf("author must be the server handle, got %q", created.Author)
	}
}

// deleteResponse is the wire shape of both DELETE endpoints.
type deleteResponse struct {
	OK      bool  `json:"ok"`
	Deleted int64 `json:"deleted"`
}

// DELETE /api/posts/{id} is a hard delete: the post and its entire reply
// subtree are actually gone from the store afterward, not merely masked as
// tombstones, and the thread 404s rather than rendering a hole.
func TestHardDeletePostRemovesSubtree(t *testing.T) {
	h, s, _, root, reply := seedWeb(t)

	rec, body := do(t, h, "DELETE", fmt.Sprintf("/api/posts/%d", root.ID), "")
	if rec.Code != 200 {
		t.Fatalf("delete root: %d %s", rec.Code, body)
	}
	var out deleteResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	// root + its one reply from seedWeb.
	if !out.OK || out.Deleted != 2 {
		t.Fatalf("delete response: %+v (%s)", out, body)
	}

	if rec, _ := get(t, h, fmt.Sprintf("/api/threads/%d", root.ID)); rec.Code != 404 {
		t.Fatalf("thread after delete: %d", rec.Code)
	}
	if _, err := s.GetPost(root.ID); !errors.Is(err, store.ErrNoPost) {
		t.Fatalf("root should be gone from the store: %v", err)
	}
	if _, err := s.GetPost(reply.ID); !errors.Is(err, store.ErrNoPost) {
		t.Fatalf("reply should be gone from the store: %v", err)
	}
}

// Deleting a mid-thread reply removes only its own subtree; the root and any
// posts outside that subtree survive.
func TestHardDeleteMidThreadReply(t *testing.T) {
	h, s, b, root, reply := seedWeb(t)
	nested, err := s.CreatePost(b.ID, root.AuthorHandle, "nested reply", "", &reply.ID)
	if err != nil {
		t.Fatal(err)
	}

	rec, body := do(t, h, "DELETE", fmt.Sprintf("/api/posts/%d", reply.ID), "")
	if rec.Code != 200 {
		t.Fatalf("delete reply: %d %s", rec.Code, body)
	}
	var out deleteResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	// reply + its nested child.
	if !out.OK || out.Deleted != 2 {
		t.Fatalf("delete response: %+v (%s)", out, body)
	}

	if _, err := s.GetPost(reply.ID); !errors.Is(err, store.ErrNoPost) {
		t.Fatalf("reply should be gone: %v", err)
	}
	if _, err := s.GetPost(nested.ID); !errors.Is(err, store.ErrNoPost) {
		t.Fatalf("nested reply should be gone: %v", err)
	}
	if _, err := s.GetPost(root.ID); err != nil {
		t.Fatalf("root should survive: %v", err)
	}
	if rec, _ := get(t, h, fmt.Sprintf("/api/threads/%d", root.ID)); rec.Code != 200 {
		t.Fatalf("root thread should still be reachable: %d", rec.Code)
	}
}

// DELETE /api/sessions/{id} wipes every post authored by that session's
// agents, including foreign replies nested under them, and forgets the
// session's name row — while an unrelated session's own thread, elsewhere on
// the board, survives untouched.
func TestHardDeleteSessionHappyPath(t *testing.T) {
	f := seedSessions(t)

	rec, body := do(t, f.h, "DELETE", "/api/sessions/sess-alpha", "")
	if rec.Code != 200 {
		t.Fatalf("delete session: %d %s", rec.Code, body)
	}
	var out deleteResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	// root1 (alpha's own root) plus beta's foreign reply1 nested under it.
	if !out.OK || out.Deleted != 2 {
		t.Fatalf("delete response: %+v (%s)", out, body)
	}

	if _, err := f.s.GetPost(f.root1.ID); !errors.Is(err, store.ErrNoPost) {
		t.Fatalf("root1 should be gone: %v", err)
	}
	if _, err := f.s.GetPost(f.reply1.ID); !errors.Is(err, store.ErrNoPost) {
		t.Fatalf("reply1 should be gone: %v", err)
	}

	_, sbody := get(t, f.h, "/api/sessions")
	for _, s := range decodeSessions(t, sbody) {
		if s.SessionID == "sess-alpha" {
			t.Fatalf("sess-alpha should be gone from the listing: %s", sbody)
		}
	}

	// beta's own thread, elsewhere on the board, survives untouched.
	if _, err := f.s.GetPost(f.root2.ID); err != nil {
		t.Fatalf("root2 should survive: %v", err)
	}
	if rec, _ := get(t, f.h, fmt.Sprintf("/api/threads/%d", f.root2.ID)); rec.Code != 200 {
		t.Fatalf("root2 thread should still be reachable: %d", rec.Code)
	}
}

func TestHardDeleteSessionUnknown404(t *testing.T) {
	f := seedSessions(t)
	if rec, body := do(t, f.h, "DELETE", "/api/sessions/never-seen", ""); rec.Code != 404 {
		t.Fatalf("unknown session: %d %s", rec.Code, body)
	}
}

// A blank session id is a validation refusal (400), not "unknown session"
// (404) — it falls through writeErrMapped's default arm rather than its
// ErrNoSession case.
func TestHardDeleteSessionBlankID400(t *testing.T) {
	f := seedSessions(t)
	if rec, body := do(t, f.h, "DELETE", "/api/sessions/%20", ""); rec.Code != 400 {
		t.Fatalf("blank session id: %d %s", rec.Code, body)
	}
}
