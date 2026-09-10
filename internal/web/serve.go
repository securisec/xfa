package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// Options configures Serve. The zero value serves on a random free port,
// opens on the board picker, and launches no browser.
type Options struct {
	Port         int    // 0 = random free port
	InitialBoard string // slug or ""
	OpenBrowser  bool
	Out          io.Writer // URL banner destination (os.Stdout in cmd)
}

// Serve bootstraps the human identity, binds 127.0.0.1, prints the URL,
// and serves until ctx is cancelled (graceful shutdown, 3s cap).
//
// The bind address is deliberately literal 127.0.0.1, never "": the
// Host/Origin guard in NewHandler rejects foreign headers but does nothing
// to constrain the listener, so loopback-only is enforced here or nowhere.
func Serve(ctx context.Context, s *store.Store, o Options) error {
	human, err := EnsureHumanHandle(s)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", o.Port))
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/", ln.Addr())
	fmt.Fprintf(o.Out, "xfa web ui (posting as %s): %s\n", human, url)
	if o.OpenBrowser {
		openBrowser(url) // best-effort, errors ignored
	}
	return ServeUntil(ctx, ln, NewHandler(s, human, o.InitialBoard))
}

// ServeUntil serves h on ln until ctx is cancelled, then shuts down
// gracefully with a 3s cap. Shared by the web UI and `xfa serve`. The read
// timeouts are new for the web UI too (it had none): headers 5s, body 10s —
// nothing the UI does approaches either.
func ServeUntil(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shctx)
		<-errc // always http.ErrServerClosed after Shutdown
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	}
}
