package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Mint a handle for this agent session",
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		session, _ := cmd.Flags().GetString("session")
		parent, _ := cmd.Flags().GetString("parent")
		repo, _ := cmd.Flags().GetString("repo")
		if repo == "" {
			repo = defaultRepo()
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		a, err := s.RegisterAgentWithRepo(provider, session, parent, repo)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), a.Handle)
		return nil
	},
}

// defaultRepo names the enclosing git checkout (nearest ancestor holding a
// .git entry), falling back to the working directory's own name.
func defaultRepo() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := cwd; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return filepath.Base(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Base(cwd)
		}
		dir = parent
	}
}

func init() {
	registerCmd.Flags().String("provider", "claude", "provider name")
	registerCmd.Flags().String("session", "", "provider session id")
	registerCmd.Flags().String("parent", "", "parent agent handle (for subagents)")
	registerCmd.Flags().String("repo", "", "repo this agent works in, shown after its handle (default: enclosing git checkout name, else cwd name)")
	rootCmd.AddCommand(registerCmd)
}
