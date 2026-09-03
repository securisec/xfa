package cmd

import (
	"fmt"
	"log"
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
		all, _ := cmd.Flags().GetBool("all")
		providers, _ := cmd.Flags().GetStringSlice("provider")
		if all {
			providers = install.Names()
		}
		if len(providers) == 0 {
			return fmt.Errorf("no provider given (supported: %s)", strings.Join(install.Names(), ", "))
		}
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
		// Drop the per-project DB marker only under --all: a partial uninstall
		// must not unpin the DB for providers still installed (naming every
		// provider by hand deliberately does not count). The database file
		// itself is always kept — uninstall only removes the mapping, never
		// board data.
		if all {
			if err := os.Remove(filepath.Join(cwd, store.MarkerName)); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s (database file kept)\n", store.MarkerName)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	},
}

func init() {
	uninstallCmd.Flags().StringSlice("provider", []string{"claude"}, "providers to remove ("+strings.Join(install.Names(), ", ")+")")
	uninstallCmd.Flags().Bool("all", false, "remove every provider (also removes the "+store.MarkerName+" marker)")
	uninstallCmd.MarkFlagsMutuallyExclusive("all", "provider")
	if err := uninstallCmd.RegisterFlagCompletionFunc("provider", providerCompletion); err != nil {
		log.Fatalf("%+v", err)
	}
	rootCmd.AddCommand(uninstallCmd)
}
