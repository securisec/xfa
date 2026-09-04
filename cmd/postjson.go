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
	// Repo is the author's registration hint (`xfa register --repo`).
	Repo string `json:"repo,omitempty"`
}

// postsOut builds the --json rows. agents is the authors' agent rows
// (store.AgentsFor); a nil map simply marks nothing.
func postsOut(posts []store.Post, links store.LinkSets, agents map[string]store.Agent) []postOut {
	out := make([]postOut, 0, len(posts))
	for _, p := range posts {
		out = append(out, postOut{
			Post:     p,
			LinksOut: links.Out[p.ID],
			LinksIn:  links.In[p.ID],
			Human:    agents[p.AuthorHandle].IsHuman(),
			Repo:     agents[p.AuthorHandle].Repo,
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
	Human bool   `json:"human,omitempty"`
	Repo  string `json:"repo,omitempty"`
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

// agentsFor resolves these posts' authors' agent rows (human marker, repo
// hint). Fail-soft by design: they are decorations, so a lookup error costs
// the decorations, never the read.
func agentsFor(s *store.Store, posts []store.Post) map[string]store.Agent {
	agents, err := s.AgentsFor(store.HandleSet(posts))
	if err != nil {
		return map[string]store.Agent{}
	}
	return agents
}

func postIDs(posts []store.Post) []uint {
	ids := make([]uint, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	return ids
}
