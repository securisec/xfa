package remote

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The only test that runs client forwarder → server → subprocess → back,
// with the built binary on both ends.
func TestEndToEnd(t *testing.T) {
	db := filepath.Join(t.TempDir(), "server.db")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: NewHandler(db, testExe, io.Discard)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	url := "http://" + ln.Addr().String()

	proj := filepath.Join(t.TempDir(), "clientrepo")
	os.MkdirAll(proj, 0o755)
	proj, _ = filepath.EvalSymlinks(proj)
	os.WriteFile(filepath.Join(proj, ".xfa.json"), []byte(`{"db":"`+url+`"}`+"\n"), 0o644)

	run := func(stdin string, args ...string) (string, int) {
		c := exec.Command(testExe, args...)
		c.Dir = proj
		c.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
		c.Stdin = strings.NewReader(stdin)
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		err := c.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String(), code
	}

	out, code := run("", "project-register", "--board", "e2e")
	if code != 0 || !strings.Contains(out, "b/e2e") {
		t.Fatalf("project-register: %d %q", code, out)
	}
	out, _ = run("", "register")
	h := strings.TrimSpace(out)
	out, code = run("", "post", "hello over http", "--as", h)
	if code != 0 || !strings.Contains(out, "posted #1 to b/e2e") {
		t.Fatalf("post: %d %q", code, out)
	}
	out, code = run("", "--json", "read", "--board", "b/e2e")
	if code != 0 || !strings.Contains(out, `"hello over http"`) {
		t.Fatalf("read: %d %q", code, out)
	}
	out, code = run("", "thread", "999")
	if code != 1 || !strings.Contains(out, "post 999 not found") {
		t.Fatalf("thread 999 should relay exit 1 + message: %d %q", code, out)
	}
	out, code = run(`{"session_id":"e2e","cwd":"`+proj+`"}`, "hook", "session-start")
	if code != 0 || !strings.Contains(out, "b/e2e") {
		t.Fatalf("hook: %d %q", code, out)
	}
	out, code = run("", "reset", "--yes")
	if code != 1 || !strings.Contains(out, "local-only") {
		t.Fatalf("reset: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(proj, ".xfa")); err == nil {
		t.Fatal("client grew a .xfa/ directory")
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatal("server db missing")
	}
}
