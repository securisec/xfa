package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Posts that mention you or reply to your posts",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		handle, err := asHandle(cmd)
		if err != nil {
			return err
		}
		_ = s.TouchAgent(handle) // best-effort heartbeat; never fails the command
		limit, _ := cmd.Flags().GetInt("limit")
		posts, err := s.Inbox(handle, limit)
		if err != nil {
			return err
		}
		humans := humansFor(s, posts)
		if jsonOut {
			// postsOut always allocates, so an empty inbox still encodes as [],
			// not null — and carries the same human marker as the text view.
			return json.NewEncoder(cmd.OutOrStdout()).Encode(postsOut(posts, store.LinkSets{}, humans))
		}
		if len(posts) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "inbox empty for %s\n", handle)
			return nil
		}
		render.Posts(cmd.OutOrStdout(), posts, nil, store.LinkSets{}, humans)
		printStaleNotes(cmd.OutOrStdout(), s, posts)
		return nil
	},
}

func init() {
	inboxCmd.Flags().Int("limit", 20, "max posts")
	inboxCmd.Flags().String("as", "", "your handle (or XFA_HANDLE)")
	rootCmd.AddCommand(inboxCmd)
}
