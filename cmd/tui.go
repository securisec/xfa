package cmd

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/securisec/xfa/internal/store"
	"github.com/securisec/xfa/internal/tui"
	web "github.com/securisec/xfa/internal/web"
)

// stdinIsTTY is the human-only gate's real predicate, held in a var purely so
// tests can step past it and exercise the code below. This does NOT weaken the
// gate: the compiled binary always runs this exact implementation, and an agent
// invoking `xfa tui` cannot assign a Go variable in another process. The
// non-TTY refusal tests exercise this function itself, unpatched.
var stdinIsTTY = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && term.IsTerminal(os.Stdin.Fd())
}

// serveWebUI is the --web entry point, likewise a var so tests can capture the
// Options the flag/board wiring produces without binding a real socket.
var serveWebUI = web.Serve

// tui is HUMAN-ONLY (like reset, and gated the same way): an interactive
// browser is useless to agents and burns their context, so non-terminal stdin
// is refused outright — before the database is even opened or created — and
// the error redirects agents to the equivalent read-only commands. The skill
// deliberately never mentions this command. The char-device stat alone is not
// enough: `xfa tui </dev/null` passes it (/dev/null IS a char device), so
// term.IsTerminal must also hold.
var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Browse boards and threads interactively (HUMAN-ONLY)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !stdinIsTTY() {
			return errors.New("xfa tui is interactive and human-only; agents should use `xfa threads` or `xfa board`")
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		// Explicit --board wins; else try the cwd registry. An unresolved cwd
		// is not an error here — the TUI just starts on the board picker.
		var initial *store.Board
		if slug, _ := cmd.Flags().GetString("board"); slug != "" {
			if initial, err = resolveBoardArg(s, cmd); err != nil {
				return err
			}
		} else if cwd, err := os.Getwd(); err == nil {
			b, err := s.ResolveBoard(cwd)
			if err == nil {
				initial = b
			} else if !errors.Is(err, store.ErrNoBoard) {
				return err
			}
		}
		// --web swaps the bubbletea front end for a localhost HTTP one; it is
		// the same human-only command, so it lands below the gate above.
		serveWeb, port, err := webFlags(cmd)
		if err != nil {
			return err
		}
		if serveWeb {
			slug := ""
			if initial != nil {
				slug = initial.Slug
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return serveWebUI(ctx, s, web.Options{
				Port: port, InitialBoard: slug, OpenBrowser: true, Out: cmd.OutOrStdout(),
			})
		}
		_, err = tea.NewProgram(tui.New(s, initial), tea.WithAltScreen()).Run()
		return err
	},
}

// webFlags reads the --web/--port pair. --port alone is a user mistake, not a
// no-op, so it is rejected. Callers must run the human-only TTY gate first:
// an agent probing any --web/--port variant must hit that refusal, not this.
func webFlags(cmd *cobra.Command) (bool, int, error) {
	serveWeb, _ := cmd.Flags().GetBool("web")
	if !serveWeb && cmd.Flags().Changed("port") {
		return false, 0, errors.New("--port requires --web")
	}
	port, _ := cmd.Flags().GetInt("port")
	return serveWeb, port, nil
}

func registerTuiWebFlags(c *cobra.Command) {
	c.Flags().Bool("web", false, "serve the web UI on localhost instead of the terminal UI (HUMAN-ONLY)")
	c.Flags().Int("port", 0, "web UI port (default: random free port; requires --web)")
}

func init() {
	tuiCmd.Flags().String("board", "", "board slug (default: resolved from cwd, else the board picker)")
	registerTuiWebFlags(tuiCmd)
	rootCmd.AddCommand(tuiCmd)
}
