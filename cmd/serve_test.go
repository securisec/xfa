package cmd

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeRefusesRemoteDB(t *testing.T) {
	t.Setenv("XFA_DB", "http://host:7777")
	_, err := runXfaErr(t, "serve")
	if err == nil || err.Error() != "cannot serve a remote database (http://host:7777)" {
		t.Fatalf("err = %v", err)
	}
}

func TestResetRefusesRemoteDBBeforeStat(t *testing.T) {
	t.Setenv("XFA_DB", "http://host:7777")
	out, err := runRemote(t, "reset", "--yes")
	if err == nil || !strings.Contains(err.Error(), "xfa reset is local-only: this project uses the xfa server at http://host:7777") {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if strings.Contains(out, "nothing to reset") {
		t.Fatal("reset fell through to the Stat path")
	}
}

func TestTuiRefusesRemoteDB(t *testing.T) {
	t.Setenv("XFA_DB", "http://host:7777")
	resetTuiFlags(t)
	// gate first: non-TTY stdin is refused with the human-only text, exactly
	// what an agent probing --web must see
	_, err := runXfaErr(t, "tui", "--web")
	if err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("gate: %v", err)
	}
	// past the gate: the remote refusal, before any store is opened
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
	resetTuiFlags(t)
	_, err = runXfaErr(t, "tui")
	if err == nil || err.Error() != "xfa tui needs a local database; this project uses http://host:7777 — run it on the server host" {
		t.Fatalf("remote: %v", err)
	}
}

func TestServeListensAndAnswers(t *testing.T) {
	db := filepath.Join(t.TempDir(), "board.db")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServe(ctx, ln, db, "/bin/true", &strings.Builder{}) }()
	resp, err := http.Post("http://"+ln.Addr().String()+"/v1/reset", "application/json", strings.NewReader(`{"cwd":"/a/b"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("reset over the wire: %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func TestServeBannerWarnsOffLoopback(t *testing.T) {
	var sb strings.Builder
	printServeBanner(&sb, "/tmp/x.db", "/opt/xfa", "0.0.0.0:7777")
	if !strings.Contains(sb.String(), "xfa serve: /tmp/x.db on http://0.0.0.0:7777 (no authentication; exec /opt/xfa)") ||
		!strings.Contains(sb.String(), "warning: anyone who can reach 0.0.0.0:7777") {
		t.Fatalf("banner = %q", sb.String())
	}
	sb.Reset()
	printServeBanner(&sb, "/tmp/x.db", "/opt/xfa", "127.0.0.1:7777")
	if strings.Contains(sb.String(), "warning:") {
		t.Fatal("loopback bind must not warn")
	}
}
