// Package remote is the CLI-over-HTTP transport: a project whose resolved
// database path is an http(s) URL forwards every allowlisted subcommand to
// `xfa serve` there, which execs its own binary and returns stdout/stderr/exit.
package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Verbs is the allowlist. The server mounts exactly one route per entry and
// the client forwards exactly these; everything else runs locally (and the
// server 404s it). init/uninstall/reset/tui/serve are deliberately absent.
var Verbs = []string{
	"register", "post", "reply", "read", "thread", "threads", "board",
	"sessions", "session", "search", "inbox", "questions", "resolve",
	"stats", "delete", "boards", "hook", "project-register",
}

func IsVerb(v string) bool { return slices.Contains(Verbs, v) }

type Request struct {
	Args   []string `json:"args"`
	Cwd    string   `json:"cwd"`
	Handle string   `json:"handle,omitempty"`
	Stdin  []byte   `json:"stdin,omitempty"`
}

type Response struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

// Forward runs one subcommand on the server at base. Redirects are never
// followed (a proxy must not be able to bounce the request elsewhere).
func Forward(base, verb string, req Request, timeout time.Duration) (Response, error) {
	// A trailing slash would make the path "//v1/<verb>", which the mux 307s
	// to the clean form — and redirects are never followed.
	base = strings.TrimRight(base, "/")
	var resp Response
	body, err := json.Marshal(req)
	if err != nil {
		return resp, err
	}
	c := &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	r, err := c.Post(base+"/v1/"+verb, "application/json", bytes.NewReader(body))
	if err != nil {
		return resp, fmt.Errorf("cannot reach xfa server %s: %w", base, err)
	}
	defer r.Body.Close()
	if r.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(r.Body).Decode(&e) == nil && e.Error != "" {
			return resp, fmt.Errorf("xfa server %s: %s", base, e.Error)
		}
		return resp, fmt.Errorf("xfa server %s: unexpected status %d", base, r.StatusCode)
	}
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		return resp, fmt.Errorf("cannot reach xfa server %s: unexpected end of response: %w", base, err)
	}
	return resp, nil
}
