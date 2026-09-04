package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// sessionFlagUsage is the one description of --session, shared by read,
// threads and board so the three can never document the filter differently.
// Absent or empty means "all sessions" — the default, and byte-for-byte the
// behavior each command had before the flag existed.
const sessionFlagUsage = "only this session's activity (see `xfa sessions`); default: all sessions"

// registerSessionFlag adds --session to a read-side command.
func registerSessionFlag(c *cobra.Command) {
	c.Flags().String("session", "", sessionFlagUsage)
}

// sessionFilter reads --session. An empty value is not a filter: it is the
// all-sessions default, which routes to the untouched unfiltered query.
// Trimmed, so a whitespace-only value is also treated as no filter — a bare
// "--session ' '" would otherwise pass the != "" check yet match nothing,
// and (on `read`) trip the --unread refusal for no reason.
func sessionFilter(cmd *cobra.Command) string {
	sess, _ := cmd.Flags().GetString("session")
	return strings.TrimSpace(sess)
}

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List sessions that have posted, most recently active first",
	Args:  noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		var boardID uint
		scope := "any board"
		if all, _ := cmd.Flags().GetBool("all"); !all {
			b, err := resolveBoardArg(s, cmd)
			if err != nil {
				return err
			}
			boardID = b.ID
			scope = "b/" + b.Slug
		}
		sums, err := s.ListSessions(boardID)
		if err != nil {
			return err
		}
		if jsonOut {
			if sums == nil {
				sums = []store.SessionSummary{} // encode as [], not null
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(sums)
		}
		if len(sums) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "no sessions on %s\n", scope)
			return nil
		}
		// The full id, not the abbreviated one — sanitized, because an id is
		// agent-supplied text on its way to a human terminal just as much as
		// a name or a post body is. A crafted id is therefore printed in its
		// escaped form for safety, not the raw bytes that were registered.
		for _, sum := range sums {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %d post(s)  last active %s\n",
				render.SessionDisplayName(sum), render.SessionID(sum.SessionID),
				sum.Posts, render.Rel(sum.LastActivity))
		}
		return nil
	},
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Session-scoped commands",
}

var sessionNameCmd = &cobra.Command{
	Use:   "name <session-id> <name>",
	Short: "Give a session a short descriptive name",
	// Anyone may name any session — the same trust model as `xfa resolve`,
	// so there is no --as and no ownership check. Naming is an upsert:
	// running it again renames.
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		if err := s.SetSessionName(args[0], args[1]); err != nil {
			return err
		}
		// The store trims before storing; echo the trimmed form so the
		// confirmation shows what a later `xfa sessions` will display.
		fmt.Fprintf(cmd.OutOrStdout(), "named session %s %q\n", render.SessionID(args[0]), strings.TrimSpace(args[1]))
		return nil
	},
}

func init() {
	sessionsCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	sessionsCmd.Flags().Bool("all", false, "sessions across every board")
	rootCmd.AddCommand(sessionsCmd)

	sessionCmd.AddCommand(sessionNameCmd)
	rootCmd.AddCommand(sessionCmd)
}
