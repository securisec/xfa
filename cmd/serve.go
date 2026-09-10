package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/securisec/xfa/internal/remote"
	"github.com/securisec/xfa/internal/store"
	"github.com/securisec/xfa/internal/web"
	"github.com/spf13/cobra"
)

// serve is HOST-ONLY: it exposes this database to the network with no
// authentication. Loopback by default; the skill never mentions it.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve this project's database to remote xfa clients over HTTP (no auth)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		path, err := store.ResolvePath(wd)
		if err != nil {
			return err
		}
		if store.IsRemote(path) {
			return fmt.Errorf("cannot serve a remote database (%s)", path)
		}
		if path, err = filepath.Abs(path); err != nil {
			return err
		}
		// Open once so the schema exists and a bad path fails here, not in
		// the first subprocess.
		if _, err := store.Open(path); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		addr, _ := cmd.Flags().GetString("addr")
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		printServeBanner(cmd.OutOrStdout(), path, exe, ln.Addr().String())
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runServe(ctx, ln, path, exe, cmd.ErrOrStderr())
	},
}

func runServe(ctx context.Context, ln net.Listener, db, exe string, log io.Writer) error {
	return web.ServeUntil(ctx, ln, remote.NewHandler(db, exe, log))
}

func printServeBanner(w io.Writer, db, exe, addr string) {
	fmt.Fprintf(w, "xfa serve: %s on http://%s (no authentication; exec %s)\n", db, addr, exe)
	host, _, _ := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		fmt.Fprintf(w, "warning: anyone who can reach %s can post, read, delete, resolve and mark-read as any handle, register projects and create boards — expose only on a trusted network\n", addr)
	}
}

func init() {
	serveCmd.Flags().String("addr", "127.0.0.1:7777", "listen address (0.0.0.0:PORT exposes it on the network)")
	rootCmd.AddCommand(serveCmd)
}
