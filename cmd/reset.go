package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// errHumansOnly is the self-recognition refusal for every non-human path:
// piped/non-terminal stdin, and EOF at the confirmation prompt (agents' most
// likely stdin, e.g. </dev/null, is a char device that passes the TTY stat
// but has no human behind it).
var errHumansOnly = fmt.Errorf("refusing to reset: stdin is not a terminal (this command is for humans; agents must never run it). A human can pass --yes.")

// removeSidecars deletes the WAL sidecar files next to a SQLite database,
// ignoring ones that don't exist.
func removeSidecars(path string) error {
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// reset is HUMAN-ONLY (user-confirmed ruling): it destroys every board for
// every project. Without --yes it demands an interactive terminal plus a typed
// confirmation, so an agent piping input can never get past the gate; --yes is
// the human's non-interactive bypass. The skill tells agents never to run it.
var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Delete the ENTIRE xfa database (HUMAN-ONLY)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve like every other command (XFA_DB > .xfa.json marker >
		// global default); the printed path below is the safety net showing
		// which database is about to be destroyed.
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		path, err := store.ResolvePath(cwd)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			// Sweep orphan sidecars a crashed earlier reset may have left.
			if err := removeSidecars(path); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "nothing to reset")
			return nil
		}

		// Human gate FIRST: a refused agent invocation must not even open
		// (and thereby AutoMigrate/checkpoint) the database it declined to
		// touch. The TTY path still opens below, for the summary, before the
		// prompt.
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fi, err := os.Stdin.Stat()
			if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
				return errHumansOnly
			}
		}

		// Open only to print what is about to be destroyed, then close the
		// pool before touching the files: removing a live WAL DB under an
		// open connection corrupts the checkpoint dance. If the pool cannot
		// be closed, that assumption doesn't hold — abort the removal.
		s, err := store.Open(path)
		if err != nil {
			return err
		}
		var boards, posts, agents int64
		s.DB.Model(&store.Board{}).Count(&boards)
		s.DB.Model(&store.Post{}).Count(&posts)
		s.DB.Model(&store.Agent{}).Count(&agents)
		sqlDB, err := s.DB.DB()
		if err == nil {
			err = sqlDB.Close()
		}
		if err != nil {
			return fmt.Errorf("not resetting: could not close the database pool first (removing a live WAL database is unsafe): %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"This deletes the ENTIRE xfa database at %s: %d boards, %d posts, %d agents.\n",
			path, boards, posts, agents)

		if !yes {
			fmt.Fprint(cmd.OutOrStdout(), "type 'reset' to confirm: ")
			line, readErr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.TrimSpace(line) != "reset" {
				if readErr != nil {
					// EOF/read failure: nobody is answering the prompt.
					return errHumansOnly
				}
				return fmt.Errorf("aborted")
			}
		}

		if err := os.Remove(path); err != nil {
			return err
		}
		if err := removeSidecars(path); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "database reset.")
		return nil
	},
}

func init() {
	resetCmd.Flags().Bool("yes", false, "skip confirmation (HUMAN-ONLY bypass)")
	rootCmd.AddCommand(resetCmd)
}
