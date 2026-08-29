package web

import (
	"net/http"

	"github.com/securisec/xfa/internal/store"
)

// NewHandler builds the full web UI handler: the embedded SPA at / and
// the JSON API under /api/. human is the handle all writes are authored
// as; initialBoard (may be "") is the board slug the UI opens on.
func NewHandler(s *store.Store, human, initialBoard string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, staticFS, "static/index.html")
	})
	registerReadRoutes(mux, s, human, initialBoard)
	registerWriteRoutes(mux, s, human)
	return guard(mux)
}
