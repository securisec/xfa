package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:   "board",
	Short: "Show a whole board: every thread, replies indented",
	Args:  noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		b, err := resolveBoardArg(s, cmd)
		if err != nil {
			return err
		}
		// Participated semantics: matched threads render whole, other
		// sessions' replies included — the filter selects threads, not posts.
		session := sessionFilter(cmd)
		var posts []store.Post
		if session != "" {
			posts, err = s.BoardPostsBySession(b.ID, session)
		} else {
			posts, err = s.BoardPosts(b.ID)
		}
		if err != nil {
			return err
		}
		if jsonOut {
			if posts == nil {
				posts = []store.Post{} // encode as [], not null
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(posts)
		}
		if len(posts) == 0 {
			// Under a filter the board itself may be full — say what came up
			// empty rather than declaring the board empty.
			if session != "" {
				// Echoed back sanitized: the argument is attacker-controllable
				// text (a crafted id copied out of a shared board).
				fmt.Fprintf(cmd.OutOrStdout(), "no posts on b/%s for session %s\n",
					b.Slug, render.SessionID(session))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "board b/%s is empty\n", b.Slug)
			return nil
		}
		w := cmd.OutOrStdout()
		// One lookup for the whole board, reused by every thread below.
		authors := authorsFor(s, posts)
		for i, thread := range store.GroupThreads(posts) {
			if i > 0 {
				fmt.Fprintln(w) // one blank line between threads
			}
			thread = render.TreeOrder(thread)
			render.Posts(w, thread, render.Depths(thread), store.LinkSets{}, authors)
		}
		return nil
	},
}

func init() {
	boardCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	registerSessionFlag(boardCmd)
	rootCmd.AddCommand(boardCmd)
}
