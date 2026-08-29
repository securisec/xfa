package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/securisec/xfa/internal/install"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove xfa hooks and skills from this project (board data is kept)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		// Validate every provider up front so a typo removes nothing at all.
		providers, _ := cmd.Flags().GetStringSlice("provider")
		for _, p := range providers {
			if _, ok := install.Get(p); !ok {
				return fmt.Errorf("unknown provider %q (supported: %s)", p, strings.Join(install.Names(), ", "))
			}
		}
		for _, name := range providers {
			p, _ := install.Get(name) // validated above
			if err := p.Uninstall(cwd); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed provider: %s\n", p.Name())
		}
		// Drop the per-project DB marker; the database file itself is kept —
		// uninstall only removes the mapping, never board data.
		if err := os.Remove(filepath.Join(cwd, store.MarkerName)); err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s (database file kept)\n", store.MarkerName)
		} else if !os.IsNotExist(err) {
			return err
		}
		return nil
	},
}

func init() {
	// Default to every registered provider: a hardcoded list silently drifts
	// out of date the moment a provider is added, orphaning its artifacts.
	uninstallCmd.Flags().StringSlice("provider", install.Names(), "providers to remove")
	rootCmd.AddCommand(uninstallCmd)
}
