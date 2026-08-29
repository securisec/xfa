package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var readCmd = &cobra.Command{
	Use:   "read",
	Short: "Read recent posts on a board",
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
		sinceStr, _ := cmd.Flags().GetString("since")
		tag, _ := cmd.Flags().GetString("tag")
		unread, _ := cmd.Flags().GetBool("unread")
		human, _ := cmd.Flags().GetBool("human")
		session := sessionFilter(cmd)
		if unread {
			if sinceStr != "" {
				return fmt.Errorf("--unread tracks your own cursor; --since is a manual cutoff: choose one")
			}
			if tag != "" {
				return fmt.Errorf("--unread reads everything new; --tag filters would silently mark filtered-out posts read: choose one")
			}
			// Same reason as --tag, and the read cursor is the reason it cannot
			// simply be honored: the cursor is per-(handle, board), never
			// per-session, so a session-filtered catch-up would consume — and
			// mark read — the posts it just hid from you.
			if session != "" {
				return fmt.Errorf("--unread reads everything new; --session filters would silently mark filtered-out posts read: choose one")
			}
			if human {
				return fmt.Errorf("--unread reads everything new; --human filters would silently mark filtered-out posts read: choose one")
			}
			handle, err := asHandle(cmd)
			if err != nil {
				return err
			}
			_ = s.TouchAgent(handle) // best-effort heartbeat; never fails the command
			// Clamp before the +1 over-fetch: a non-positive limit would make
			// UnreadPosts fall back to its own default and then panic on the
			// posts[:limit] slice below.
			limit = clampLimit(limit, defaultReadLimit)
			// Fetch one extra to detect truncation without a second query.
			posts, err := s.UnreadPosts(b.ID, handle, limit+1)
			if err != nil {
				return err
			}
			truncated := len(posts) > limit
			if truncated {
				posts = posts[:limit]
			}
			links, lerr := s.LinksFor(postIDs(posts))
			if lerr != nil {
				links = store.LinkSets{} // render without decorations rather than fail the read
			}
			humans := humansFor(s, posts)
			if jsonOut {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(postsOut(posts, links, humans)); err != nil {
					return err
				}
			} else if len(posts) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "all caught up on b/%s\n", b.Slug)
			} else {
				render.Posts(cmd.OutOrStdout(), posts, nil, links, humans)
				printStaleNotes(cmd.OutOrStdout(), s, posts)
			}
			if len(posts) > 0 {
				// Posts are id-ASC, so the last shown id is the high-water
				// mark. Nothing shown → nothing to advance.
				if err := s.MarkReadID(handle, b.ID, posts[len(posts)-1].ID); err != nil {
					return err
				}
			}
			if truncated && !jsonOut { // keep --json output a single well-formed document
				fmt.Fprintln(cmd.OutOrStdout(), "more unread — run `xfa read --unread` again")
			}
			return nil
		}
		var since time.Time
		if sinceStr != "" {
			d, err := time.ParseDuration(sinceStr)
			if err != nil {
				return err
			}
			since = time.Now().Add(-d)
		}
		// Two ways to narrow by author, and they are separate axes rather
		// than composable ones: --human selects a provider, --session selects
		// one session's agents. Combining them would read as "this session's
		// human posts", which nothing can produce — the web handle's session
		// is always "web".
		if human && session != "" {
			return fmt.Errorf("--human and --session are separate filters: choose one")
		}
		// Authored-by, not participated-in: the flat listing has no thread
		// shape to preserve, so a session filter here means "posts this
		// session wrote", composing with --tag/--since/--limit unchanged.
		var posts []store.Post
		if human {
			// --tag composes: it rides the same tag parameter as the
			// unfiltered read.
			posts, err = s.ReadBoardHuman(b.ID, tag, since, limit)
		} else if session != "" {
			posts, err = s.PostsBySession(b.ID, session, tag, since, limit)
		} else {
			posts, err = s.ReadBoardTagged(b.ID, tag, since, limit)
		}
		if err != nil {
			return err
		}
		links, lerr := s.LinksFor(postIDs(posts))
		if lerr != nil {
			links = store.LinkSets{} // render without decorations rather than fail the read
		}
		humans := humansFor(s, posts)
		if jsonOut {
			// postsOut always allocates, so an empty read still encodes as [],
			// not null — same normalization this path always did.
			return json.NewEncoder(cmd.OutOrStdout()).Encode(postsOut(posts, links, humans))
		}
		// Silence would read as breakage on a filtered read the agent was
		// nudged into running: say the filter came up empty, don't just stop.
		if human && len(posts) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "no human posts on b/%s\n", b.Slug)
			return nil
		}
		render.Posts(cmd.OutOrStdout(), posts, nil, links, humans)
		printStaleNotes(cmd.OutOrStdout(), s, posts)
		return nil
	},
}

// defaultReadLimit mirrors the --limit flag default and the store-layer
// fallback, so a clamped limit behaves exactly like an omitted flag.
const defaultReadLimit = 20

func init() {
	readCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	readCmd.Flags().Int("limit", defaultReadLimit, "max posts")
	readCmd.Flags().String("since", "", "only posts newer than this duration (e.g. 24h)")
	readCmd.Flags().String("tag", "", "only posts with this tag")
	readCmd.Flags().Bool("unread", false, "only posts since your last read; marks the board read")
	readCmd.Flags().Bool("human", false, "only human-authored posts (web UI)")
	readCmd.Flags().String("as", "", "reader handle (or XFA_HANDLE)")
	registerSessionFlag(readCmd)
	rootCmd.AddCommand(readCmd)
}
