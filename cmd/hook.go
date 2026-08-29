package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/securisec/xfa/internal/hookrun"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "hook <event>",
	Short:  "Internal: invoked by provider hooks; reads hook JSON on stdin",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Antigravity's payload is camelCase conversationId/workspacePaths, not
		// the Claude shape — decode and dispatch it separately. Fail open, like
		// everything below: hooks must never break an agent session.
		if args[0] == "antigravity-invoke" || args[0] == "antigravity-stop" {
			var ain hookrun.AntigravityInput
			_ = json.NewDecoder(os.Stdin).Decode(&ain)
			if len(ain.WorkspacePaths) == 0 {
				return nil
			}
			s, err := openStoreAt(ain.WorkspacePaths[0])
			if err != nil {
				return nil
			}
			var out string
			if args[0] == "antigravity-invoke" {
				out, _ = hookrun.AntigravityInvoke(s, ain)
			} else {
				out, _ = hookrun.AntigravityStop(s, ain)
			}
			if out != "" {
				fmt.Fprintln(cmd.OutOrStdout(), out)
			}
			return nil
		}
		// Fail open: hooks must never break an agent session.
		var in hookrun.Input
		_ = json.NewDecoder(os.Stdin).Decode(&in)
		if in.Cwd == "" {
			in.Cwd, _ = os.Getwd()
		}
		// Resolve the DB from the payload's cwd — the same cwd the hookrun
		// entrypoints resolve the board from — not the process cwd: providers
		// may invoke the hook from outside the marked project.
		s, err := openStoreAt(in.Cwd)
		if err != nil {
			return nil
		}
		var out string
		switch args[0] {
		case "session-start":
			out, _ = hookrun.SessionStart(s, in)
		case "stop":
			out, _ = hookrun.Stop(s, in)
		case "subagent-stop":
			out, _ = hookrun.SubagentStop(s, in)
		case "user-prompt":
			out, _ = hookrun.UserPrompt(s, in)
		}
		if out != "" {
			fmt.Fprintln(cmd.OutOrStdout(), out)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(hookCmd) }
