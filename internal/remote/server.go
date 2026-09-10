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
	"strings"
	"time"
	"unicode/utf8"
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
	// Bound the whole in-flight life (queue wait + exec) by timeout, so a
	// client that never disconnects cannot park a goroutine and its decoded
	// body on this unauthenticated listener forever.
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		// Client gave up, server is shutting down, or the queue wait itself
		// hit timeout: bursts queue here instead of hard-failing on arrival.
		fail(w, http.StatusServiceUnavailable, "server busy")
		logf("reject", http.StatusServiceUnavailable)
		return
	}
	resp := s.exec(ctx, verb, req, timeout)
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
	// Only project-register persists cwd into projects.path, where a shallow
	// registered path ("/" or "/Users") would capture ResolveProject's
	// walk-up; every other verb at worst fails "no board here".
	if verb == "project-register" && strings.Count(filepath.Clean(req.Cwd), string(filepath.Separator)) < 2 {
		return req, http.StatusBadRequest, "cwd must be at least two levels deep"
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
	if req.Handle != "" && (len(req.Handle) > 64 || strings.IndexFunc(req.Handle, func(r rune) bool { return r < 0x20 }) >= 0) {
		// Shape is not checked (--as carries arbitrary handles too); an
		// unknown handle is a no-op in GetAgent downstream, exactly as local.
		return req, http.StatusBadRequest, "invalid handle"
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
// it against the server filesystem (read-only). The ≥2-levels floor that
// guards project-register's projects.path insert lives in validate, not here.
func checkCwd(c string) string {
	if len(c) > MaxCwdLen {
		return "cwd too long"
	}
	// Any control byte (NUL included) is rejected: it would otherwise land in
	// projects.path and from there into every listing.
	if strings.IndexFunc(c, func(r rune) bool { return r < 0x20 }) >= 0 || !filepath.IsAbs(c) {
		return "cwd must be an absolute path"
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
	stdout, otrunc := capped(out.Bytes())
	stderr, etrunc := capped(errb.Bytes())
	resp := Response{Stdout: stdout, Stderr: stderr}
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
	// A truncated payload must not read as a clean success: JSON on stdout
	// would be invalid, so signal via a non-zero code and a stderr note.
	if (otrunc || etrunc) && resp.Code == 0 {
		resp.Code = 1
		resp.Stderr += "xfa server: output truncated\n"
	}
	return resp
}

// capped bounds one output stream. ponytail: post-hoc cap — our own binary's
// listings are bounded by --limit/MaxPostLen, so growth beyond this is a
// transient RSS cost, not a correctness one; stream-cap if a board ever
// prints >100 MiB.
func capped(b []byte) (string, bool) {
	if len(b) <= MaxOutput {
		return string(b), false
	}
	n := MaxOutput
	for n > 0 && !utf8.RuneStart(b[n]) { // never split a multi-byte rune
		n--
	}
	return string(b[:n]), true
}
