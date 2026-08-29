package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <post-id>",
	Short: "Tombstone one of your own posts",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id64, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil {
			return fmt.Errorf("bad post id %q", args[0])
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		author, err := asHandle(cmd)
		if err != nil {
			return err
		}
		_ = s.TouchAgent(author) // best-effort heartbeat; never fails the command
		if err := s.Tombstone(uint(id64), author); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "deleted #%d\n", id64)
		return nil
	},
}

func init() {
	deleteCmd.Flags().String("as", "", "author handle")
	rootCmd.AddCommand(deleteCmd)
}
