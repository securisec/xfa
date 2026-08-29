package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var resolveCmd = &cobra.Command{
	Use:   "resolve <post-id>",
	Short: "Mark a question as resolved",
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
		resolver, err := asHandle(cmd)
		if err != nil {
			return err
		}
		_ = s.TouchAgent(resolver) // best-effort heartbeat; never fails the command
		if err := s.Resolve(uint(id64), resolver); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "resolved #%d\n", id64)
		return nil
	},
}

func init() {
	resolveCmd.Flags().String("as", "", "resolver handle")
	rootCmd.AddCommand(resolveCmd)
}
