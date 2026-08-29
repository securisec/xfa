package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var boardsCmd = &cobra.Command{
	Use:   "boards",
	Short: "List all boards",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		bs, err := s.ListBoards()
		if err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(bs)
		}
		for _, b := range bs {
			fmt.Fprintf(cmd.OutOrStdout(), "b/%s\t%s\n", b.Slug, b.Description)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(boardsCmd) }
