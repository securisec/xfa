package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var testExe string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "xfa-remote-test")
	if err != nil {
		panic(err)
	}
	testExe = filepath.Join(dir, "xfa")
	build := exec.Command("go", "build", "-o", testExe, "../..")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build test binary (on a fresh checkout run `mise run ui-build` first — go:embed needs internal/web/static/index.html): " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func newServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "board.db")
	srv := httptest.NewServer(NewHandler(db, testExe, io.Discard))
	t.Cleanup(srv.Close)
	return srv, db
}

func post(t *testing.T, srv *httptest.Server, verb string, req Request, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	body, _ := json.Marshal(req)
	r, _ := http.NewRequest("POST", srv.URL+"/v1/"+verb, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func decode(t *testing.T, b []byte) Response {
	t.Helper()
	var r Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	return r
}

// seed registers a client project + handle on srv's DB.
func seed(t *testing.T, srv *httptest.Server, cwd, board string) string {
	t.Helper()
	resp, b := post(t, srv, "project-register", Request{Args: []string{"--board", board}, Cwd: cwd}, nil)
	if resp.StatusCode != 200 || decode(t, b).Code != 0 {
		t.Fatalf("project-register: %d %s", resp.StatusCode, b)
	}
	_, b = post(t, srv, "register", Request{Cwd: cwd}, nil)
	h := strings.TrimSpace(decode(t, b).Stdout)
	if h == "" {
		t.Fatalf("register printed no handle: %s", b)
	}
	return h
}

func TestExecRegisterAndPost(t *testing.T) {
	srv, _ := newServer(t)
	h := seed(t, srv, "/client/proj", "remote-t")
	_, b := post(t, srv, "post", Request{Args: []string{"hello from afar"}, Cwd: "/client/proj", Handle: h}, nil)
	if r := decode(t, b); r.Code != 0 || !strings.Contains(r.Stdout, "posted #1 to b/remote-t") {
		t.Fatalf("post: %+v", r)
	}
	_, b = post(t, srv, "read", Request{Args: []string{"--json"}, Cwd: "/client/proj"}, nil)
	if r := decode(t, b); !strings.Contains(r.Stdout, `"hello from afar"`) {
		t.Fatalf("read --json: %+v", r)
	}
}

// Every route is mounted and execs: --help exits 0 on every verb.
func TestEveryVerbIsMountedAndExecs(t *testing.T) {
	srv, _ := newServer(t)
	for _, v := range Verbs {
		resp, b := post(t, srv, v, Request{Args: []string{"--help"}, Cwd: "/a/b"}, nil)
		if resp.StatusCode != 200 || decode(t, b).Code != 0 {
			t.Errorf("%s --help: %d %s", v, resp.StatusCode, b)
		}
	}
}

func TestExecPropagatesExitCode(t *testing.T) {
	srv, _ := newServer(t)
	_, b := post(t, srv, "thread", Request{Args: []string{"999"}, Cwd: "/client/proj"}, nil)
	r := decode(t, b)
	if r.Code != 1 || !strings.Contains(r.Stderr, "post 999 not found") {
		t.Fatalf("expected the CLI's own not-found error with exit 1, got %+v", r)
	}
}

func TestAllowlist(t *testing.T) {
	srv, _ := newServer(t)
	for _, v := range []string{"init", "uninstall", "reset", "tui", "serve", "help", "completion", "__complete", "READ", ""} {
		resp, _ := post(t, srv, v, Request{Cwd: "/client/proj"}, nil)
		if resp.StatusCode != 404 {
			t.Errorf("%q: status %d, want 404", v, resp.StatusCode)
		}
	}
	r, _ := http.Get(srv.URL + "/v1/read")
	if r.StatusCode != 405 {
		t.Errorf("GET: %d", r.StatusCode)
	}
}

func TestRejectsBeforeExec(t *testing.T) {
	srv, _ := newServer(t)
	ok := Request{Cwd: "/client/proj"}
	cases := []struct {
		name string
		req  Request
		hdr  map[string]string
		want int
		msg  string
	}{
		{"origin", ok, map[string]string{"Origin": "http://evil"}, 403, "forbidden: cross-origin"},
		{"origin null", ok, map[string]string{"Origin": "null"}, 403, "forbidden: cross-origin"},
		{"content-type", ok, map[string]string{"Content-Type": "text/plain"}, 415, "Content-Type must be application/json"},
		{"relative cwd", Request{Cwd: "rel"}, nil, 400, "cwd must be an absolute path"},
		{"root cwd", Request{Cwd: "/"}, nil, 400, "cwd must be at least two levels deep"},
		{"depth1 cwd", Request{Cwd: "/Users"}, nil, 400, "cwd must be at least two levels deep"},
		{"nul cwd", Request{Cwd: "/a/b\x00c"}, nil, 400, "cwd must be an absolute path"},
		{"control cwd", Request{Cwd: "/a/b\n"}, nil, 400, "cwd must be an absolute path"},
		{"long cwd", Request{Cwd: "/a/" + strings.Repeat("b", MaxCwdLen)}, nil, 400, "cwd too long"},
		{"too many args", Request{Cwd: "/a/b", Args: make([]string, MaxArgs+1)}, nil, 400, "too many args"},
		{"long arg", Request{Cwd: "/a/b", Args: []string{strings.Repeat("x", MaxArgLen+1)}}, nil, 400, "arg too long"},
		{"nul arg", Request{Cwd: "/a/b", Args: []string{"a\x00b"}}, nil, 400, "arg contains NUL"},
		{"bad handle", Request{Cwd: "/a/b", Handle: "../../etc"}, nil, 400, "handle must look like word-word-N"},
		{"long handle", Request{Cwd: "/a/b", Handle: strings.Repeat("a", 65)}, nil, 400, "handle must look like word-word-N"},
	}
	for _, c := range cases {
		resp, b := post(t, srv, "read", c.req, c.hdr)
		if resp.StatusCode != c.want || !strings.Contains(string(b), c.msg) {
			t.Errorf("%s: %d %s (want %d %q)", c.name, resp.StatusCode, b, c.want, c.msg)
		}
	}
	// oversized body: the server answers 413 and closes; the client may see
	// the 413 or a write error depending on how much it had flushed.
	big, _ := json.Marshal(Request{Cwd: "/a/b", Stdin: bytes.Repeat([]byte("x"), int(MaxBody)+1)})
	r, _ := http.NewRequest("POST", srv.URL+"/v1/hook", bytes.NewReader(big))
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err == nil && resp.StatusCode != 413 {
		t.Errorf("oversized body: %d", resp.StatusCode)
	}
}

func TestHookPayloadCwdValidated(t *testing.T) {
	srv, _ := newServer(t)
	for _, payload := range []string{
		`{"cwd":"/"}`, `{"cwd":"rel"}`, `{"workspacePaths":["/"]}`,
		`{"cwd":"/a/b","workspacePaths":["/"]}`, // every cwd-like field is checked
	} {
		resp, b := post(t, srv, "hook", Request{Args: []string{"session-start"}, Cwd: "/a/b", Stdin: []byte(payload)}, nil)
		if resp.StatusCode != 400 || !strings.Contains(string(b), "hook payload cwd") {
			t.Errorf("%s: %d %s", payload, resp.StatusCode, b)
		}
	}
	resp, b := post(t, srv, "hook", Request{Args: []string{"session-start"}, Cwd: "/a/b", Stdin: []byte(`{"session_id":"s1","cwd":"/a/b"}`)}, nil)
	if resp.StatusCode != 200 || decode(t, b).Code != 0 {
		t.Fatalf("good hook: %d %s", resp.StatusCode, b)
	}
	for _, payload := range []string{
		`{"session_id":"` + strings.Repeat("s", 129) + `","cwd":"/a/b"}`,
		`{"conversationId":"` + strings.Repeat("c", 129) + `","workspacePaths":["/a/b"]}`,
	} {
		resp, b = post(t, srv, "hook", Request{Args: []string{"stop"}, Cwd: "/a/b", Stdin: []byte(payload)}, nil)
		if resp.StatusCode != 400 || !strings.Contains(string(b), "hook payload session id too long") {
			t.Errorf("long session id: %d %s", resp.StatusCode, b)
		}
	}
}

// validate nulls stdin for every verb but hook; exec forwards whatever is
// left. ("x" is not JSON; validate ignores the unmarshal error by design.)
func TestStdinOnlyForHook(t *testing.T) {
	body, _ := json.Marshal(Request{Cwd: "/a/b", Stdin: []byte("x")})
	for verb, want := range map[string]int{"boards": 0, "hook": 1} {
		r := httptest.NewRequest("POST", "/v1/"+verb, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		req, _, msg := validate(httptest.NewRecorder(), r, verb)
		if msg != "" || len(req.Stdin) != want {
			t.Fatalf("%s: stdin=%q msg=%q", verb, req.Stdin, msg)
		}
	}
}

// The subprocess environment is exactly three variables. `env NAME=VAL`
// with no utility prints its environment, so exec it in place of xfa.
func TestSubprocessEnvIsExactlyThreeVars(t *testing.T) {
	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Skip("no env binary")
	}
	t.Setenv("XFA_DB", "/should/not/leak.db")
	t.Setenv("HOME", "/should/not/leak")
	s := &server{db: "/srv.db", exe: envPath}
	got := func(req Request) []string {
		r := s.exec(context.Background(), "XFA_NOOP=1", req, time.Second)
		if r.Code != 0 {
			t.Fatalf("env exited %d: %s", r.Code, r.Stderr)
		}
		lines := strings.Fields(r.Stdout)
		sort.Strings(lines)
		return lines
	}
	want := []string{"XFA_CWD=/a/b", "XFA_DB=/srv.db", "XFA_HANDLE=amber-otter-1", "XFA_NOOP=1"}
	if g := got(Request{Cwd: "/a/b", Handle: "amber-otter-1"}); strings.Join(g, " ") != strings.Join(want, " ") {
		t.Fatalf("env = %v", g)
	}
	want = []string{"XFA_CWD=/a/b", "XFA_DB=/srv.db", "XFA_NOOP=1"}
	if g := got(Request{Cwd: "/a/b"}); strings.Join(g, " ") != strings.Join(want, " ") {
		t.Fatalf("env without handle = %v", g)
	}
}

func TestTimeoutYields124(t *testing.T) {
	oldI := InboxTimeout
	InboxTimeout = 300 * time.Millisecond
	t.Cleanup(func() { InboxTimeout = oldI })
	srv, _ := newServer(t)
	h := seed(t, srv, "/c/p", "tmo")
	start := time.Now()
	_, b := post(t, srv, "inbox", Request{Args: []string{"--as", h, "--wait"}, Cwd: "/c/p", Handle: h}, nil)
	r := decode(t, b)
	if r.Code != 124 || !strings.Contains(r.Stderr, "xfa server: inbox timed out after") {
		t.Fatalf("got %+v", r)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout did not kill the subprocess promptly")
	}
}

func TestOutputCap(t *testing.T) {
	old := MaxOutput
	MaxOutput = 64
	t.Cleanup(func() { MaxOutput = old })
	srv, _ := newServer(t)
	_, b := post(t, srv, "read", Request{Args: []string{"--help"}, Cwd: "/a/b"}, nil)
	r := decode(t, b)
	if r.Code != 0 || !strings.HasSuffix(r.Stdout, "\n[output truncated]\n") || len(r.Stdout) > MaxOutput+len("\n[output truncated]\n") {
		t.Fatalf("cap: code=%d len=%d tail=%q", r.Code, len(r.Stdout), r.Stdout[max(0, len(r.Stdout)-30):])
	}
}

// A signal-killed subprocess reports ExitCode() == -1; the response must
// carry a usable code, since os.Exit(-1) on the client would become 255.
func TestSignalDeathIsExitOne(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	s := &server{exe: sh}
	r := s.exec(context.Background(), "-c", Request{Args: []string{"kill -9 $$"}, Cwd: "/a/b"}, 5*time.Second)
	if r.Code != 1 {
		t.Fatalf("code = %d, want 1", r.Code)
	}
}

func TestConcurrencyCaps(t *testing.T) {
	old, oldI := Concurrency, InboxConcurrency
	Concurrency, InboxConcurrency = 1, 1
	t.Cleanup(func() { Concurrency, InboxConcurrency = old, oldI })
	oldT := InboxTimeout
	InboxTimeout = 3 * time.Second
	t.Cleanup(func() { InboxTimeout = oldT })
	srv, _ := newServer(t)
	h := seed(t, srv, "/c/p", "cc")
	done := make(chan struct{})
	go func() {
		// Not post(): t.Fatal from a non-test goroutine is illegal, and the
		// close must happen even when the request errors.
		defer close(done)
		body, _ := json.Marshal(Request{Args: []string{"--as", h, "--wait"}, Cwd: "/c/p", Handle: h})
		if r, err := http.Post(srv.URL+"/v1/inbox", "application/json", bytes.NewReader(body)); err == nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
	}()
	// poll until the slot is taken (never a fixed sleep). The sleep leads the
	// poll: with one slot, a poll that beats the waiter to it takes the only
	// slot and the waiter — not the poller — is the one that gets the 503.
	var resp *http.Response
	var b []byte
	deadline := time.Now().Add(5 * time.Second)
	for {
		time.Sleep(50 * time.Millisecond)
		resp, b = post(t, srv, "inbox", Request{Args: []string{"--as", h}, Cwd: "/c/p", Handle: h}, nil)
		if resp.StatusCode == 503 || time.Now().After(deadline) {
			break
		}
	}
	if resp.StatusCode != 503 || !strings.Contains(string(b), "server busy") {
		t.Fatalf("second inbox: %d %s", resp.StatusCode, b)
	}
	resp, _ = post(t, srv, "boards", Request{Cwd: "/c/p"}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("boards while inbox slot is held: %d", resp.StatusCode)
	}
	<-done
}
