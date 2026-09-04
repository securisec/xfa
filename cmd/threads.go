package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// defaultThreadsLimit mirrors the --limit flag default so a clamped
// non-positive limit behaves exactly like an omitted flag.
const defaultThreadsLimit = 50

// threadSummary is the CLI's presentation of a store.ThreadSummary: the root
// post embedded so --json keeps its flat shape (post fields alongside
// Replies/LastActivity), exactly as it was before the grouping logic moved to
// internal/store.
type threadSummary struct {
	store.Post
	Replies      int
	LastActivity time.Time
	// Embedded flat, exactly as postOut carries it: human (the text view's
	// "[human]" marker — the root was written by a person through the web UI)
	// plus project_path, both omitempty, so agent-rooted threads on a
	// single-project DB keep the wire shape they always had.
	store.Author
}

// summarizeThreads adapts store.ThreadSummaries (the shared grouping logic —
// see internal/store/boardview.go) to the CLI's flat JSON shape. authors maps
// author handle -> decoration (authorsFor); a nil map decorates nothing.
func summarizeThreads(posts []store.Post, authors map[string]store.Author) []threadSummary {
	summaries := store.ThreadSummaries(posts)
	threads := make([]threadSummary, len(summaries))
	for i, t := range summaries {
		threads[i] = threadSummary{
			Post:         t.Root,
			Replies:      t.Replies,
			LastActivity: t.LastActivity,
			Author:       authors[t.Root.AuthorHandle],
		}
	}
	return threads
}

// replyNoun pluralizes for the threads text view: "1 reply", "2 replies".
func replyNoun(n int) string {
	if n == 1 {
		return "reply"
	}
	return "replies"
}

var threadsCmd = &cobra.Command{
	Use:   "threads",
	Short: "List a board's threads, most recently active first",
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
		limit, _ := cmd.Flags().GetInt("limit")
		limit = clampLimit(limit, defaultThreadsLimit)
		// Participated semantics: a session's filter matches every thread it
		// took part in, and matched threads come back whole — so reply counts
		// and activity times stay the thread's real ones.
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
		// One lookup for the whole listing: a thread rooted by a person shows
		// up as such in both renderings.
		threads := summarizeThreads(posts, authorsFor(s, posts))
		if len(threads) > limit {
			threads = threads[:limit]
		}
		if jsonOut {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(threads)
		}
		if len(threads) == 0 {
			// Say which filter came up empty: under --session, "no threads on
			// b/x" would read as an empty board.
			if session != "" {
				// Echoed back sanitized: the argument is attacker-controllable
				// text (a crafted id copied out of a shared board).
				fmt.Fprintf(cmd.OutOrStdout(), "no threads on b/%s for session %s\n",
					b.Slug, render.SessionID(session))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "no threads on b/%s\n", b.Slug)
			return nil
		}
		for _, t := range threads {
			fmt.Fprintf(cmd.OutOrStdout(), "%s — %d %s, active %s\n",
				render.Line(t.Post, t.Author), t.Replies, replyNoun(t.Replies), render.Rel(t.LastActivity))
		}
		return nil
	},
}

func init() {
	threadsCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	threadsCmd.Flags().Int("limit", defaultThreadsLimit, "max threads")
	registerSessionFlag(threadsCmd)
	rootCmd.AddCommand(threadsCmd)
}
