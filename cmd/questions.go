package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var questionsCmd = &cobra.Command{
	Use:   "questions",
	Short: "List open (unresolved) questions",
	Args:  noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		questions, err := s.OpenQuestions(boardID, limit)
		if err != nil {
			return err
		}
		// Single display gate for both renderings: a recently-seen asker's
		// timestamp is noise, so it is dropped before either encoder sees it.
		// That keeps --json byte-identical to its pre-liveness shape for the
		// common case (omitempty), and guarantees the field is present exactly
		// when the text line carries the annotation.
		for i := range questions {
			if t := questions[i].AskerLastSeenAt; t != nil && time.Since(*t) < store.StaleDisplayAge {
				questions[i].AskerLastSeenAt = nil
			}
		}
		// A question asked by a person through the web UI is exactly the kind an
		// agent should answer first, so this view marks it the same way `read`
		// does — in both renderings, off one batched lookup.
		authors := authorsFor(s, rootPosts(questions))
		if jsonOut {
			// Always allocates, so an empty listing still encodes as [], not null.
			out := make([]openQuestionOut, 0, len(questions))
			for _, q := range questions {
				out = append(out, openQuestionOut{OpenQuestion: q, Author: authors[q.AuthorHandle]})
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
		}
		if len(questions) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no open questions")
			return nil
		}
		// The reply count makes answered-but-unresolved visible at a glance;
		// the asker's age says how likely an answer is to be read back. The
		// phrasing stays probabilistic ("last seen"), never "gone" — a quiet
		// handle may simply be doing read-only work.
		for _, q := range questions {
			line := fmt.Sprintf("%s — %d %s",
				render.Line(q.Post, authors[q.AuthorHandle]), q.Replies, replyNoun(int(q.Replies)))
			if q.AskerLastSeenAt != nil {
				line += fmt.Sprintf(" — asker last seen %s", render.Rel(*q.AskerLastSeenAt))
				if time.Since(*q.AskerLastSeenAt) >= store.StaleReplyAge {
					line += "; answer for the record; anyone may resolve"
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		return nil
	},
}

func init() {
	questionsCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	questionsCmd.Flags().Bool("all", false, "questions from every board")
	questionsCmd.Flags().Int("limit", 20, "max results")
	rootCmd.AddCommand(questionsCmd)
}
