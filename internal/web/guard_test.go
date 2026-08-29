package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuardAllowsLocalhostRejectsForeign(t *testing.T) {
	s := openTemp(t)
	h := NewHandler(s, "human-tester-1", "")
	cases := []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:8080", "", 200},
		{"localhost:8080", "http://localhost:8080", 200},
		{"[::1]:8080", "http://[::1]:8080", 200},
		{"evil.example:8080", "", 403},
		{"localhost.evil.example:8080", "", 403},
		{"127.0.0.1:8080", "https://evil.example", 403},
		{"127.0.0.1:8080", "http://localhost.evil.example", 403},
		{"127.0.0.1:8080", "null", 403},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("host=%s origin=%s: got %d want %d", c.host, c.origin, rec.Code, c.want)
		}
		// Anti-framing on every response, allowed or refused: a foreign page
		// that knows the port must not be able to iframe the moderator UI.
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("host=%s origin=%s: X-Frame-Options=%q want DENY", c.host, c.origin, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("host=%s origin=%s: CSP=%q want frame-ancestors 'none'", c.host, c.origin, got)
		}
	}
}

func TestRootServesEmbeddedPage(t *testing.T) {
	s := openTemp(t)
	h := NewHandler(s, "human-tester-1", "")
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("embedded page not served: %d %q", rec.Code, body[:min(120, len(body))])
	}
	// The page is the built Vue SPA, not the placeholder: it must carry the
	// mount point the bundle hydrates into. The rest of the artifact's
	// contract is pinned by TestEmbeddedUIArtifact.
	if !strings.Contains(body, `id="app"`) {
		t.Error(`embedded page missing the id="app" mount point`)
	}
}
