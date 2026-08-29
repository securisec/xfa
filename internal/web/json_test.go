package web

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

func TestToPostJSON(t *testing.T) {
	created := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	tombstoned := time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC)
	resolved := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	parent := uint(7)

	cases := []struct {
		name  string
		post  store.Post
		human string
		want  postJSON
	}{
		{
			name: "top-level post by the human",
			post: store.Post{
				ID: 1, BoardID: 2, AuthorHandle: "human-tester-1",
				ParentID: nil, Body: "hello", Tag: "til", CreatedAt: created,
			},
			human: "human-tester-1",
			want: postJSON{
				ID: 1, BoardID: 2, Author: "human-tester-1",
				ParentID: nil, Body: "hello", Tag: "til", CreatedAt: created,
				Deleted: false, Mine: true,
			},
		},
		{
			name: "reply by another agent",
			post: store.Post{
				ID: 3, BoardID: 2, AuthorHandle: "crimson-otter-7",
				ParentID: &parent, Body: "re: hello", CreatedAt: created,
			},
			human: "human-tester-1",
			want: postJSON{
				ID: 3, BoardID: 2, Author: "crimson-otter-7",
				ParentID: &parent, Body: "re: hello", CreatedAt: created,
				Deleted: false, Mine: false,
			},
		},
		{
			name: "tombstoned post is Deleted",
			post: store.Post{
				ID: 4, BoardID: 2, AuthorHandle: "crimson-otter-7",
				Body: "[deleted]", CreatedAt: created, TombstonedAt: &tombstoned,
			},
			human: "human-tester-1",
			want: postJSON{
				ID: 4, BoardID: 2, Author: "crimson-otter-7",
				Body: "[deleted]", CreatedAt: created,
				Deleted: true, Mine: false,
			},
		},
		{
			name: "tombstoned post authored by the human is both Deleted and Mine",
			post: store.Post{
				ID: 5, BoardID: 2, AuthorHandle: "human-tester-1",
				Body: "[deleted]", CreatedAt: created, TombstonedAt: &tombstoned,
			},
			human: "human-tester-1",
			want: postJSON{
				ID: 5, BoardID: 2, Author: "human-tester-1",
				Body: "[deleted]", CreatedAt: created,
				Deleted: true, Mine: true,
			},
		},
		{
			name: "resolved question carries resolution fields",
			post: store.Post{
				ID: 6, BoardID: 2, AuthorHandle: "crimson-otter-7",
				Body: "why?", Tag: "question", CreatedAt: created,
				ResolvedAt: &resolved, ResolvedBy: "human-tester-1",
			},
			human: "human-tester-1",
			want: postJSON{
				ID: 6, BoardID: 2, Author: "crimson-otter-7",
				Body: "why?", Tag: "question", CreatedAt: created,
				ResolvedAt: &resolved, ResolvedBy: "human-tester-1",
				Deleted: false, Mine: false,
			},
		},
		{
			name: "empty human handle matches nobody",
			post: store.Post{
				ID: 7, BoardID: 2, AuthorHandle: "crimson-otter-7",
				Body: "hi", CreatedAt: created,
			},
			human: "",
			want: postJSON{
				ID: 7, BoardID: 2, Author: "crimson-otter-7",
				Body: "hi", CreatedAt: created,
				Deleted: false, Mine: false,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := toPostJSON(c.post, c.human, nil, store.LinkSets{}, nil)
			if got.ID != c.want.ID || got.BoardID != c.want.BoardID ||
				got.Author != c.want.Author || got.Body != c.want.Body ||
				got.Tag != c.want.Tag || !got.CreatedAt.Equal(c.want.CreatedAt) ||
				got.ResolvedBy != c.want.ResolvedBy ||
				got.Deleted != c.want.Deleted || got.Mine != c.want.Mine {
				t.Errorf("toPostJSON() = %+v, want %+v", got, c.want)
			}
			switch {
			case c.want.ParentID == nil && got.ParentID != nil:
				t.Errorf("ParentID = %v, want nil", *got.ParentID)
			case c.want.ParentID != nil && got.ParentID == nil:
				t.Errorf("ParentID = nil, want %v", *c.want.ParentID)
			case c.want.ParentID != nil && *got.ParentID != *c.want.ParentID:
				t.Errorf("ParentID = %v, want %v", *got.ParentID, *c.want.ParentID)
			}
			switch {
			case c.want.ResolvedAt == nil && got.ResolvedAt != nil:
				t.Errorf("ResolvedAt = %v, want nil", *got.ResolvedAt)
			case c.want.ResolvedAt != nil && got.ResolvedAt == nil:
				t.Errorf("ResolvedAt = nil, want %v", *c.want.ResolvedAt)
			case c.want.ResolvedAt != nil && !got.ResolvedAt.Equal(*c.want.ResolvedAt):
				t.Errorf("ResolvedAt = %v, want %v", *got.ResolvedAt, *c.want.ResolvedAt)
			}
		})
	}
}

// TestToPostJSONOmitsEmptyOptionalFields pins the wire shape later tasks and
// the browser UI consume: nil ParentID/ResolvedAt and empty Tag/ResolvedBy must
// not appear at all, while deleted/mine are always present booleans.
func TestToPostJSONOmitsEmptyOptionalFields(t *testing.T) {
	created := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	b, err := json.Marshal(toPostJSON(store.Post{
		ID: 1, BoardID: 2, AuthorHandle: "crimson-otter-7",
		Body: "hi", CreatedAt: created,
	}, "human-tester-1", nil, store.LinkSets{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, absent := range []string{
		"parent_id", "tag", "resolved_at", "resolved_by",
		"session_id", "session_display_name",
		"links_out", "links_in",
	} {
		if strings.Contains(got, absent) {
			t.Errorf("wire shape should omit %q, got %s", absent, got)
		}
	}
	for _, present := range []string{`"deleted":false`, `"mine":false`, `"id":1`, `"board_id":2`} {
		if !strings.Contains(got, present) {
			t.Errorf("wire shape should contain %q, got %s", present, got)
		}
	}
}

// TestToPostJSONSessionLabels pins the additive per-post session fields the
// web UI's badges read. The label is the server's — the same
// render.SessionDisplayName every other surface uses — so the browser never
// rebuilds the fallback format itself.
func TestToPostJSONSessionLabels(t *testing.T) {
	created := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	started := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	idx := sessionIndex{
		"crimson-otter-7": {
			SessionID: "sess-alpha1234", Name: "parser work",
			LeadHandle: "crimson-otter-7", StartedAt: started,
		},
		"azure-lynx-3": {
			SessionID:  "sess-beta5678",
			LeadHandle: "azure-lynx-3", StartedAt: started,
		},
	}
	post := func(author string) store.Post {
		return store.Post{ID: 1, BoardID: 2, AuthorHandle: author, Body: "hi", CreatedAt: created}
	}

	named := toPostJSON(post("crimson-otter-7"), "human-tester-1", idx, store.LinkSets{}, nil)
	if named.SessionID != "sess-alpha1234" || named.SessionDisplayName != "parser work" {
		t.Errorf("named session = %q/%q, want sess-alpha1234/parser work",
			named.SessionID, named.SessionDisplayName)
	}

	unnamed := toPostJSON(post("azure-lynx-3"), "human-tester-1", idx, store.LinkSets{}, nil)
	want := "azure-lynx-3 · 2026-08-20 · sess-bet"
	if unnamed.SessionID != "sess-beta5678" || unnamed.SessionDisplayName != want {
		t.Errorf("unnamed session = %q/%q, want sess-beta5678/%q",
			unnamed.SessionID, unnamed.SessionDisplayName, want)
	}

	// The web human handle has no session row, and neither does an agent that
	// registered without a session id: no badge, and no empty badge either.
	loner := toPostJSON(post("human-tester-1"), "human-tester-1", idx, store.LinkSets{}, nil)
	if loner.SessionID != "" || loner.SessionDisplayName != "" {
		t.Errorf("sessionless author = %q/%q, want both empty",
			loner.SessionID, loner.SessionDisplayName)
	}
	// Existing fields are untouched by the labelling.
	if named.ID != 1 || named.BoardID != 2 || named.Author != "crimson-otter-7" ||
		named.Body != "hi" || named.Mine || !named.CreatedAt.Equal(created) {
		t.Errorf("labelling disturbed the existing fields: %+v", named)
	}
	if loner.Mine != true {
		t.Errorf("Mine = false for the human's own post: %+v", loner)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 201, map[string]any{"ok": true})

	if rec.Code != 201 {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["ok"] != true {
		t.Errorf("body = %v, want ok=true", got)
	}
}

func TestWriteErr(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, 404, errors.New("no such post"))

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if len(got) != 1 || got["error"] != "no such post" {
		t.Errorf(`body = %v, want {"error":"no such post"}`, got)
	}
}
