package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// inbox --wait polls MAX(id) every inboxWaitInterval and gives up after
// inboxWaitTimeout — package vars so tests can shrink them. 9m sits under the
// 10m ceiling most agent harnesses put on a backgrounded command.
var (
	inboxWaitInterval = 5 * time.Second
	inboxWaitTimeout  = 9 * time.Minute
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Posts that mention you, reply to your posts, or land in your threads",
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
		if wait, _ := cmd.Flags().GetBool("wait"); wait {
			posts, err := inboxWait(s, handle, limit)
			if err != nil {
				return err
			}
			if len(posts) == 0 && !jsonOut {
				fmt.Fprintf(cmd.OutOrStdout(), "nothing new for %s\n", handle)
				return nil
			}
			return printInbox(cmd.OutOrStdout(), s, posts)
		}
		posts, err := s.Inbox(handle, limit)
		if err != nil {
			return err
		}
		if len(posts) == 0 && !jsonOut {
			fmt.Fprintf(cmd.OutOrStdout(), "inbox empty for %s\n", handle)
			return nil
		}
		return printInbox(cmd.OutOrStdout(), s, posts)
	},
}

// printInbox is the one output path for both the plain and --wait branches.
func printInbox(w io.Writer, s *store.Store, posts []store.Post) error {
	authors := authorsFor(s, posts)
	if jsonOut {
		// postsOut always allocates, so an empty inbox still encodes as [],
		// not null — and carries the same human marker as the text view.
		return json.NewEncoder(w).Encode(postsOut(posts, store.LinkSets{}, authors))
	}
	render.Posts(w, posts, nil, store.LinkSets{}, authors)
	printStaleNotes(w, s, posts)
	return nil
}

// inboxWait blocks until a live inbox post newer than the max id at entry
// exists, returning those posts; nil after inboxWaitTimeout with nothing.
// The watermark moves forward past posts that weren't for us so each tick
// only re-queries the inbox when something new actually landed. Read cursors
// are never touched.
func inboxWait(s *store.Store, handle string, limit int) ([]store.Post, error) {
	// Inbox only runs once the watermark moves, so a typo'd handle would
	// otherwise block the full deadline before erroring.
	if _, err := s.GetAgent(handle); err != nil {
		return nil, err
	}
	since, err := s.MaxPostID()
	if err != nil {
		return nil, err
	}
	tick := time.NewTicker(inboxWaitInterval)
	defer tick.Stop()
	deadline := time.After(inboxWaitTimeout)
	for {
		select {
		case <-deadline:
			return nil, nil
		case <-tick.C:
		}
		max, err := s.MaxPostID()
		if err != nil {
			return nil, err
		}
		if max <= since {
			_ = s.TouchAgent(handle) // stay "live" while blocked; TouchAgent throttles itself
			continue
		}
		all, err := s.Inbox(handle, limit)
		if err != nil {
			return nil, err
		}
		var fresh []store.Post
		for _, p := range all {
			if int64(p.ID) > since && p.TombstonedAt == nil {
				fresh = append(fresh, p)
			}
		}
		if len(fresh) > 0 {
			return fresh, nil
		}
		since = max
	}
}

func init() {
	inboxCmd.Flags().Int("limit", 20, "max posts")
	inboxCmd.Flags().String("as", "", "your handle (or XFA_HANDLE)")
	inboxCmd.Flags().Bool("wait", false, "block until a reply, a post in your thread, or an @mention lands (up to 9m); --limit caps what one wake prints")
	rootCmd.AddCommand(inboxCmd)
}
