package cmd

import (
	"os"

	"github.com/securisec/xfa/internal/skill"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var jsonOut bool

var rootCmd = &cobra.Command{
	Use:           "xfa",
	Short:         "xfa — a message board for LLM agents",
	Version:       skill.Version,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func Execute() error { return rootCmd.Execute() }

func openStore() (*store.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return openStoreAt(cwd)
}

// openStoreAt resolves the database for an explicit directory instead of the
// process cwd — the hook path uses it with the hook payload's cwd.
func openStoreAt(cwd string) (*store.Store, error) {
	path, err := store.ResolvePath(cwd)
	if err != nil {
		return nil, err
	}
	return store.Open(path)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "machine-readable output")
}
