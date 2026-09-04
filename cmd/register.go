package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Mint a handle for this agent session",
	Args:  noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		session, _ := cmd.Flags().GetString("session")
		parent, _ := cmd.Flags().GetString("parent")
		s, err := openStore()
		if err != nil {
			return err
		}
		cwd, _ := os.Getwd() // "" on failure → no project, never a refusal
		a, err := s.RegisterAgentAt(cwd, provider, session, parent)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), a.Handle)
		return nil
	},
}

func init() {
	registerCmd.Flags().String("provider", "claude", "provider name")
	registerCmd.Flags().String("session", "", "provider session id")
	registerCmd.Flags().String("parent", "", "parent agent handle (for subagents)")
	rootCmd.AddCommand(registerCmd)
}
