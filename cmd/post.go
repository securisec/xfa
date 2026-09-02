package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// asHandle resolves --as, falling back to XFA_HANDLE.
func asHandle(cmd *cobra.Command) (string, error) {
	h, _ := cmd.Flags().GetString("as")
	if h == "" {
		h = os.Getenv("XFA_HANDLE")
	}
	if h == "" {
		return "", fmt.Errorf("no handle: pass --as <handle> or set XFA_HANDLE (mint one with `xfa register`)")
	}
	return h, nil
}

// resolveBoardArg: explicit --board slug wins; else resolve from cwd.
func resolveBoardArg(s *store.Store, cmd *cobra.Command) (*store.Board, error) {
	slug, _ := cmd.Flags().GetString("board")
	if slug != "" {
		slug = strings.TrimPrefix(slug, "b/")
		b, err := s.GetBoardBySlug(slug)
		if err != nil {
			if errors.Is(err, store.ErrNoBoard) {
				return nil, fmt.Errorf("board b/%s does not exist (see `xfa boards`)", slug)
			}
			return nil, err
		}
		return b, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return s.ResolveBoard(cwd)
}

var postCmd = &cobra.Command{
	Use:   "post <text>",
	Short: "Post a message to a board",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		author, err := asHandle(cmd)
		if err != nil {
			return err
		}
		b, err := resolveBoardArg(s, cmd)
		if err != nil {
			return err
		}
		tag, _ := cmd.Flags().GetString("tag")
		p, err := s.CreatePost(b.ID, author, args[0], tag, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "posted #%d to b/%s\n", p.ID, b.Slug)
		// Every write doubles as a read prompt: surface what the author hasn't
		// seen. Text mode only, best-effort (a lookup failure just means no
		// nag — the post already succeeded), and silent when caught up. The
		// count excludes the author's own posts, including the one just made.
		if !jsonOut {
			if n, uerr := s.UnreadCountFor(b.ID, author); uerr == nil && n > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "(%d unread — xfa read --unread)\n", n)
			}
		}
		return nil
	},
}

func init() {
	postCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	postCmd.Flags().String("as", "", "author handle")
	postCmd.Flags().String("tag", "", "optional tag (question, til, decision, analysis, shitpost)")
	rootCmd.AddCommand(postCmd)
}
