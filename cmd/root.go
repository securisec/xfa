package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/securisec/xfa/internal/remote"
	"github.com/securisec/xfa/internal/skill"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var jsonOut bool

// ExitCode is returned by the remote forwarder for EVERY forwarded command,
// code 0 included: returning nil would let RunE run locally against a URL.
// main.go maps it to os.Exit(n).
type ExitCode int

func (e ExitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

const (
	hookTimeout    = 5 * time.Second  // hooks must stay fast and fail open
	forwardTimeout = 10 * time.Minute // inbox --wait legitimately runs 9m
)

// SilenceErrors is on: main.go prints errors itself, so a forwarded command
// (whose error is just an ExitCode) prints nothing extra.
var rootCmd = &cobra.Command{
	Use:               "xfa",
	Short:             "xfa — a message board for LLM agents",
	Version:           skill.Version,
	SilenceUsage:      true,
	SilenceErrors:     true,
	PersistentPreRunE: forwardIfRemote,
}

func Execute() error { return rootCmd.Execute() }

// cwd is the directory store lookups key on. XFA_CWD is set by `xfa serve`
// on its subprocess so a forwarded command resolves the CLIENT's project.
func cwd() string {
	if c := os.Getenv("XFA_CWD"); c != "" {
		return c
	}
	wd, _ := os.Getwd() // "" on failure → no project, never a refusal
	return wd
}

// topVerb walks a (possibly nested) command up to root's direct child.
func topVerb(cmd *cobra.Command) string {
	for cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	return cmd.Name()
}

// forwardIfRemote is the whole client side of remote mode. When the resolved
// database is an http(s) URL and the verb is allowlisted, the command is
// POSTed to the server verbatim and this process just relays stdout/stderr
// and the exit code. Local verbs fall through to their own refusals.
func forwardIfRemote(cmd *cobra.Command, _ []string) error {
	verb := topVerb(cmd)
	if !remote.IsVerb(verb) {
		return nil
	}
	dir := cwd()
	var stdin []byte
	if verb == "hook" {
		// Hooks resolve from the payload's cwd, not the process cwd. Read
		// the payload once here and re-seat it on the ROOT reader (the hook
		// command reads via InOrStdin, which walks up) for the local path.
		stdin, _ = io.ReadAll(cmd.InOrStdin())
		cmd.Root().SetIn(bytes.NewReader(stdin))
		var p struct {
			Cwd            string   `json:"cwd"`
			WorkspacePaths []string `json:"workspacePaths"`
		}
		_ = json.Unmarshal(stdin, &p)
		switch {
		case p.Cwd != "":
			dir = p.Cwd
		case len(p.WorkspacePaths) > 0:
			dir = p.WorkspacePaths[0]
		}
	}
	path, err := store.ResolvePath(dir)
	if err != nil {
		return nil // RunE reports it as today (hook's RunE already fails open)
	}
	if !store.IsRemote(path) {
		return nil
	}
	// args = everything the user typed minus the verb token, so flags placed
	// before the verb (`xfa --json read`) and nested subcommands survive.
	args := append([]string{}, os.Args[1:]...)
	for i, a := range args {
		if a == verb {
			args = append(args[:i], args[i+1:]...)
			break
		}
	}
	timeout := forwardTimeout
	if verb == "hook" {
		timeout = hookTimeout
	}
	resp, err := remote.Forward(path, verb, remote.Request{
		Args: args, Cwd: dir, Handle: os.Getenv("XFA_HANDLE"), Stdin: stdin,
	}, timeout)
	if err != nil {
		if verb == "hook" {
			return ExitCode(0) // fail open: nothing printed
		}
		return err // main prints "Error: …" and exits 1, like every other failure
	}
	io.WriteString(cmd.OutOrStdout(), resp.Stdout)
	io.WriteString(cmd.ErrOrStderr(), resp.Stderr)
	return ExitCode(resp.Code)
}

func openStore() (*store.Store, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return openStoreAt(wd)
}

// openStoreAt resolves the database for an explicit directory instead of the
// process cwd — the hook path uses it with the hook payload's cwd.
func openStoreAt(dir string) (*store.Store, error) {
	path, err := store.ResolvePath(dir)
	if err != nil {
		return nil, err
	}
	return store.Open(path)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "machine-readable output")
}
