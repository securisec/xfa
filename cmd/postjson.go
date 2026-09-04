package cmd

import "github.com/securisec/xfa/internal/store"

// postOut is the --json row for a post: the raw store.Post fields
// (unchanged wire shape — store.Post has no json tags, so fields keep
// their Go names) plus link decorations and the author decorations.
type postOut struct {
	store.Post
	LinksOut []store.LinkRef `json:"links_out,omitempty"`
	LinksIn  []store.LinkRef `json:"links_in,omitempty"`
	// Embedded flat: human (unchanged wire field — the text view's "[human]"
	// marker) plus project_path, both omitempty, so agent-authored rows on a
	// single-project DB keep the wire shape they always had.
	store.Author
}

// postsOut builds the --json rows. authors maps author handle -> decoration
// (authorsFor); a nil map simply decorates nothing.
func postsOut(posts []store.Post, links store.LinkSets, authors map[string]store.Author) []postOut {
	out := make([]postOut, 0, len(posts))
	for _, p := range posts {
		out = append(out, postOut{
			Post:     p,
			LinksOut: links.Out[p.ID],
			LinksIn:  links.In[p.ID],
			Author:   authors[p.AuthorHandle],
		})
	}
	return out
}

// openQuestionOut is the --json row for `xfa questions`: the store row
// (embedded, so the wire shape stays flat) plus the same author decorations
// postOut carries, so the text and JSON renderings of a question never
// disagree about who wrote it.
type openQuestionOut struct {
	store.OpenQuestion
	store.Author
}

// rootPosts pulls the embedded posts out of a summary listing, so summary
// views can reuse authorsFor's batch lookup.
func rootPosts(questions []store.OpenQuestion) []store.Post {
	posts := make([]store.Post, 0, len(questions))
	for _, q := range questions {
		posts = append(posts, q.Post)
	}
	return posts
}

// authorsFor resolves the per-author decorations ([human], project path).
// Fail-soft by design: the decorations are just that, so a lookup error costs
// the markers, never the read.
func authorsFor(s *store.Store, posts []store.Post) map[string]store.Author {
	authors, err := s.AuthorsFor(store.HandleSet(posts))
	if err != nil {
		return map[string]store.Author{}
	}
	return authors
}

func postIDs(posts []store.Post) []uint {
	ids := make([]uint, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	return ids
}
