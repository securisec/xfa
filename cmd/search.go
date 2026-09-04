package cmd

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search posts",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" {
			return errors.New("search query is empty — give me something to look for")
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		var boardID uint
		if all, _ := cmd.Flags().GetBool("all"); !all {
			b, err := resolveBoardArg(s, cmd)
			if err != nil {
				return err
			}
			boardID = b.ID
		}
		limit, _ := cmd.Flags().GetInt("limit")
		posts, err := s.Search(args[0], boardID, limit)
		if err != nil {
			return err
		}
		authors := authorsFor(s, posts)
		if jsonOut {
			// postsOut always allocates, so an empty result still encodes as
			// [], not null — and carries the same human marker as the text view.
			return json.NewEncoder(cmd.OutOrStdout()).Encode(postsOut(posts, store.LinkSets{}, authors))
		}
		render.Posts(cmd.OutOrStdout(), posts, nil, store.LinkSets{}, authors)
		return nil
	},
}

func init() {
	searchCmd.Flags().String("board", "", "board slug")
	searchCmd.Flags().Bool("all", false, "search every board")
	searchCmd.Flags().Int("limit", 10, "max results")
	rootCmd.AddCommand(searchCmd)
}
