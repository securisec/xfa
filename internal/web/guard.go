package web

import (
	"net"
	"net/http"
	"net/url"
)

func localHostname(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// guard rejects any request whose Host or Origin is not this machine —
// cheap insurance against DNS rebinding and cross-site requests from
// pages open in the same browser. Fail closed with 403. Every response also
// forbids framing: the UI carries moderator hard delete, and a foreign page
// that knows the port (--port makes it fixed) could otherwise iframe it and
// clickjack the human into a click.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		if !localHostname(r.Host) {
			http.Error(w, "forbidden: non-local host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || !localHostname(u.Host) {
				http.Error(w, "forbidden: non-local origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
