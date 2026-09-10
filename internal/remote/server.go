package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Limits. Package vars so tests can shrink them. Concurrency numbers are RSS
// bounds (each subprocess is a full xfa + SQLite open); a single-writer file
// gains nothing from more short verbs in flight.
var (
	VerbTimeout      = 30 * time.Second
	InboxTimeout     = 10 * time.Minute // inbox --wait caps itself at 9m
	MaxBody          = int64(1 << 20)   // hook payloads carry the full user prompt
	MaxArgs          = 64
	MaxArgLen        = 16 << 10 // MaxPostLen is 2000 runes = up to 8 KiB
	MaxCwdLen        = 4 << 10
	MaxOutput        = 1 << 20
	Concurrency      = 16
	InboxConcurrency = 32
)

// handleRe is the minted shape from internal/handle (adjective-noun-N). It
// gates only XFA_HANDLE; --as rides in args and ends in GetAgent like locally.
var handleRe = regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9]{1,2}$`)

type server struct {
	db, exe       string
	log           io.Writer
	sem, inboxSem chan struct{}
}

// NewHandler serves POST /v1/<verb> for every Verbs entry by exec'ing exe
// with XFA_DB=db. The route list IS the allowlist. log gets one line per
// request: remote addr, verb, exit code, duration — never args or handles.
func NewHandler(db, exe string, log io.Writer) http.Handler {
	s := &server{db: db, exe: exe, log: log,
		sem: make(chan struct{}, Concurrency), inboxSem: make(chan struct{}, InboxConcurrency)}
	mux := http.NewServeMux()
	for _, v := range Verbs {
		mux.HandleFunc("POST /v1/"+v, func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, v) })
	}
	return mux
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *server) serve(w http.ResponseWriter, r *http.Request, verb string) {
	start := time.Now()
	logf := func(what string, code int) {
		fmt.Fprintf(s.log, "%s %s %s %d %s\n", r.RemoteAddr, verb, what, code, time.Since(start).Round(time.Millisecond))
	}
	req, code, msg := validate(w, r, verb)
	if msg != "" {
		fail(w, code, msg)
		logf("reject", code)
		return
	}
	// Slot AFTER validation so a slow or bad sender never holds one.
	sem, timeout := s.sem, VerbTimeout
	if verb == "inbox" {
		sem, timeout = s.inboxSem, InboxTimeout
	}
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	default:
		fail(w, http.StatusServiceUnavailable, "server busy")
		logf("reject", http.StatusServiceUnavailable)
		return
	}
	resp := s.exec(r.Context(), verb, req, timeout)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	logf("exit", resp.Code)
}

// validate is the whole trust boundary before exec. Every rule in the spec's
// hardening table lives here. A non-empty msg means reject with code.
func validate(w http.ResponseWriter, r *http.Request, verb string) (req Request, code int, msg string) {
	// Browsers attach Origin to every non-GET request, same-origin ones
	// under DNS rebinding included, so this alone keeps pages out.
	if r.Header.Get("Origin") != "" {
		return req, http.StatusForbidden, "forbidden: cross-origin requests are not accepted"
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" && !strings.HasPrefix(ct, "application/json;") {
		return req, http.StatusUnsupportedMediaType, "Content-Type must be application/json"
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return req, http.StatusRequestEntityTooLarge, "request body too large"
		}
		return req, http.StatusBadRequest, "invalid JSON body"
	}
	if m := checkCwd(req.Cwd); m != "" {
		return req, http.StatusBadRequest, m
	}
	if len(req.Args) > MaxArgs {
		return req, http.StatusBadRequest, "too many args"
	}
	for _, a := range req.Args {
		if len(a) > MaxArgLen {
			return req, http.StatusBadRequest, "arg too long"
		}
		if strings.IndexByte(a, 0) >= 0 {
			return req, http.StatusBadRequest, "arg contains NUL"
		}
	}
	if req.Handle != "" && (len(req.Handle) > 64 || !handleRe.MatchString(req.Handle)) {
		return req, http.StatusBadRequest, "handle must look like word-word-N"
	}
	if verb != "hook" {
		req.Stdin = nil
		return req, 0, ""
	}
	// hookrun reads cwd from the payload, not from XFA_CWD, and cmd/hook.go
	// picks the field by EVENT — so every cwd-like field must pass. The
	// session id becomes a reminders row (unique index), so it is bounded
	// like register --session.
	var p struct {
		Cwd            string   `json:"cwd"`
		WorkspacePaths []string `json:"workspacePaths"`
		SessionID      string   `json:"session_id"`
		ConversationID string   `json:"conversationId"`
	}
	_ = json.Unmarshal(req.Stdin, &p)
	if len(p.SessionID) > 128 || len(p.ConversationID) > 128 {
		return req, http.StatusBadRequest, "hook payload session id too long"
	}
	for _, c := range append([]string{p.Cwd}, p.WorkspacePaths...) {
		if c == "" {
			continue
		}
		if m := checkCwd(c); m != "" {
			return req, http.StatusBadRequest, "hook payload " + m
		}
	}
	return req, 0, ""
}

// checkCwd returns "" or the rejection message. The string is only ever a
// lookup key in the projects table — but NormalizePath runs EvalSymlinks on
// it against the server filesystem (read-only), and a registered "/" or
// "/Users" would capture ResolveProject's walk-up for every unregistered
// path on a --global/XFA_DB host, so depth ≥ 2 is required.
func checkCwd(c string) string {
	if len(c) > MaxCwdLen {
		return "cwd too long"
	}
	// Any control byte (NUL included) is rejected: it would otherwise land in
	// projects.path and from there into every listing.
	if strings.IndexFunc(c, func(r rune) bool { return r < 0x20 }) >= 0 || !filepath.IsAbs(c) {
		return "cwd must be an absolute path"
	}
	if strings.Count(filepath.Clean(c), string(filepath.Separator)) < 2 {
		return "cwd must be at least two levels deep"
	}
	return ""
}

func (s *server) exec(ctx context.Context, verb string, req Request, timeout time.Duration) Response {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(ctx, s.exe, append([]string{verb}, req.Args...)...)
	c.Env = []string{"XFA_DB=" + s.db, "XFA_CWD=" + req.Cwd} // never the parent env
	if req.Handle != "" {
		c.Env = append(c.Env, "XFA_HANDLE="+req.Handle)
	}
	c.Stdin = bytes.NewReader(req.Stdin) // empty for non-hook verbs: EOF, never a block
	c.WaitDelay = time.Second
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	resp := Response{Stdout: capped(out.Bytes()), Stderr: capped(errb.Bytes())}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		resp.Code = 124
		resp.Stderr += fmt.Sprintf("xfa server: %s timed out after %s\n", verb, timeout)
	case err == nil:
		resp.Code = 0
	default:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			resp.Code = ee.ExitCode()
			if resp.Code < 0 { // signal death is -1; os.Exit(-1) would be 255
				resp.Code = 1
			}
		} else {
			resp.Code = 1
			resp.Stderr += "xfa server: " + err.Error() + "\n"
		}
	}
	return resp
}

// capped bounds one output stream. ponytail: post-hoc cap — our own binary's
// listings are bounded by --limit/MaxPostLen, so growth beyond this is a
// transient RSS cost, not a correctness one; stream-cap if a board ever
// prints >100 MiB.
func capped(b []byte) string {
	if len(b) <= MaxOutput {
		return string(b)
	}
	return string(b[:MaxOutput]) + "\n[output truncated]\n"
}
