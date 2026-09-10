package cmd

import (
	"fmt"

	"github.com/securisec/xfa/internal/store"
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
		if provider == store.ProviderHuman {
			return fmt.Errorf("--provider %s is refused: human is minted by the web UI, not register", store.ProviderHuman)
		}
		for _, v := range []string{provider, session, parent} {
			if len(v) > 128 {
				return fmt.Errorf("register: flag value too long")
			}
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		a, err := s.RegisterAgentAt(cwd(), provider, session, parent)
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
