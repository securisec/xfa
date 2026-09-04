package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var threadCmd = &cobra.Command{
	Use:   "thread <post-id>",
	Short: "Show a post and all replies",
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
		root, err := s.RootOf(uint(id64))
		if err != nil {
			if errors.Is(err, store.ErrNoPost) {
				return fmt.Errorf("post %d not found", id64)
			}
			return err
		}
		posts, err := s.Thread(root)
		if err != nil {
			return err
		}
		links, lerr := s.LinksFor(postIDs(posts))
		if lerr != nil {
			links = store.LinkSets{} // render without decorations rather than fail the read
		}
		authors := authorsFor(s, posts)
		if jsonOut {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(postsOut(posts, links, authors))
		}
		if root != uint(id64) {
			fmt.Fprintf(cmd.OutOrStdout(), "showing thread #%d (you asked for #%d)\n", root, id64)
		}
		posts = render.TreeOrder(posts)
		render.Posts(cmd.OutOrStdout(), posts, render.Depths(posts), links, authors)
		printStaleNotes(cmd.OutOrStdout(), s, posts)
		return nil
	},
}

func init() { rootCmd.AddCommand(threadCmd) }
