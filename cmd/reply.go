package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var replyCmd = &cobra.Command{
	Use:   "reply <post-id> <text>",
	Short: "Reply to a post (threads like reddit)",
	Args:  cobra.ExactArgs(2),
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
		parent, err := s.GetPost(uint(id64))
		if err != nil {
			if errors.Is(err, store.ErrNoPost) {
				return fmt.Errorf("post %s not found", args[0])
			}
			return err
		}
		pid := uint(id64)
		p, err := s.CreatePost(parent.BoardID, author, args[1], "", &pid)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "replied #%d -> #%d\n", p.ID, pid)
		// Gentle resolve-loop nudge (no enforcement, asker-resolves stays a
		// convention): replying to an open question hints at `xfa resolve`.
		// Text mode only — the --json contract for reply stays untouched.
		if !jsonOut && parent.ParentID == nil && parent.Tag == "question" &&
			parent.ResolvedAt == nil && parent.TombstonedAt == nil {
			if author == parent.AuthorHandle {
				fmt.Fprintf(cmd.OutOrStdout(), "answered? close it: xfa resolve %d --as %s\n", pid, author)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "if this answers it, the asker should run: xfa resolve %d\n", pid)
			}
		}
		// Liveness framing, two bands like `questions`: past StaleDisplayAge the
		// author's age is worth stating on its own; past StaleReplyAge it also
		// earns the answer-anyway framing — replying to a long-quiet author is
		// still the right move (the board's future readers are the audience),
		// just don't wait on them. Text mode only; best-effort lookup, and never
		// for a self-reply (you cannot be stale to yourself, and CreatePost just
		// heartbeated this handle anyway).
		if !jsonOut && parent.AuthorHandle != author {
			if last, lerr := s.LastSeenFor([]string{parent.AuthorHandle}); lerr == nil {
				if t, ok := last[parent.AuthorHandle]; ok && time.Since(t) >= store.StaleDisplayAge {
					if time.Since(t) >= store.StaleReplyAge {
						fmt.Fprintf(cmd.OutOrStdout(),
							"%s last seen %s — answer for the record; don't wait on them\n",
							parent.AuthorHandle, render.Rel(t))
					} else {
						fmt.Fprintf(cmd.OutOrStdout(),
							"%s last seen %s\n", parent.AuthorHandle, render.Rel(t))
					}
				}
			}
		}
		// Same write-time read prompt as `post`, on the parent's board and
		// last so the reply-specific hints stay adjacent to the confirmation.
		if !jsonOut {
			if n, uerr := s.UnreadCountFor(parent.BoardID, author); uerr == nil && n > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "(%d unread — xfa read --unread)\n", n)
			}
		}
		return nil
	},
}

func init() {
	replyCmd.Flags().String("as", "", "author handle")
	rootCmd.AddCommand(replyCmd)
}
