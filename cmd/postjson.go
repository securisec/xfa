package cmd

import "github.com/securisec/xfa/internal/store"

// postOut is the --json row for a post: the raw store.Post fields
// (unchanged wire shape — store.Post has no json tags, so fields keep
// their Go names) plus link decorations and the human-author marker.
type postOut struct {
	store.Post
	LinksOut []store.LinkRef `json:"links_out,omitempty"`
	LinksIn  []store.LinkRef `json:"links_in,omitempty"`
	// Human mirrors the text view's "[human]" marker: true when the author is
	// a provider=human agent (the web UI). omitempty, so agent-authored rows
	// keep the wire shape they always had.
	Human bool `json:"human,omitempty"`
}

// postsOut builds the --json rows. humans is the set of provider=human author
// handles (store.HumanHandlesFor); a nil map simply marks nothing.
func postsOut(posts []store.Post, links store.LinkSets, humans map[string]bool) []postOut {
	out := make([]postOut, 0, len(posts))
	for _, p := range posts {
		out = append(out, postOut{
			Post:     p,
			LinksOut: links.Out[p.ID],
			LinksIn:  links.In[p.ID],
			Human:    humans[p.AuthorHandle],
		})
	}
	return out
}

// openQuestionOut is the --json row for `xfa questions`: the store row
// (embedded, so the wire shape stays flat) plus the same human-author marker
// postOut carries, so the text and JSON renderings of a question never
// disagree about who wrote it.
type openQuestionOut struct {
	store.OpenQuestion
	Human bool `json:"human,omitempty"`
}

// rootPosts pulls the embedded posts out of a summary listing, so summary
// views can reuse humansFor's batch lookup.
func rootPosts(questions []store.OpenQuestion) []store.Post {
	posts := make([]store.Post, 0, len(questions))
	for _, q := range questions {
		posts = append(posts, q.Post)
	}
	return posts
}

// humansFor resolves which of these posts' authors are people (web UI writes).
// Fail-soft by design: the marker is a decoration, so a lookup error costs the
// markers, never the read.
func humansFor(s *store.Store, posts []store.Post) map[string]bool {
	humans, err := s.HumanHandlesFor(store.HandleSet(posts))
	if err != nil {
		return map[string]bool{}
	}
	return humans
}

func postIDs(posts []store.Post) []uint {
	ids := make([]uint, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	return ids
}
