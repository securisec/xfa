package web

import (
	"strings"
	"testing"
)

// TestEmbeddedUIArtifact pins the properties of the built single-file UI
// that the Go side depends on. The bundle is minified Vue — behavioral
// assertions live in ui/ (vitest) and the source-level v-html containment
// test; here we pin what survives minification: string literals and size.
func TestEmbeddedUIArtifact(t *testing.T) {
	h, _, _, _, _ := seedWeb(t)
	rec, body := get(t, h, "/")
	if rec.Code != 200 {
		t.Fatalf("page: %d", rec.Code)
	}
	page := string(body)
	if len(page) < 200_000 {
		t.Errorf("artifact suspiciously small (%d bytes) — placeholder build?", len(page))
	}
	for _, want := range []string{
		`id="app"`,        // the Vue mount point
		"/api/posts/",     // hard-delete + reply targets (string literals survive minify)
		"/api/sessions",   // session picker + rename + delete targets
		`"DELETE"`,        // the hard-delete verb (esbuild normalizes JS strings to double quotes)
		"confirm(",        // blast-radius confirmation still wired
		"data:font/woff2", // fonts inlined, not fetched
	} {
		if !strings.Contains(page, want) {
			t.Errorf("built UI no longer contains %q", want)
		}
	}
	// The offline guarantee: no runtime CDN or font hosts.
	for _, banned := range []string{"cdn.jsdelivr.net", "fonts.googleapis.com", "fonts.gstatic.com", "unpkg.com"} {
		if strings.Contains(page, banned) {
			t.Errorf("built UI references external host %q", banned)
		}
	}
}
