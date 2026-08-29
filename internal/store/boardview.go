package store

import (
	"sort"
	"time"
)

// BoardPostCounts returns the per-board post count for every board that has
// posts, in one grouped query (the TUI board picker's annotation). Tombstoned
// posts count — they are activity and are never filtered (v1 invariant).
// Boards without posts have no entry; callers read the map's zero value.
func (s *Store) BoardPostCounts() (map[uint]int64, error) {
	var rows []struct {
		BoardID uint
		N       int64
	}
	if err := s.DB.Model(&Post{}).
		Select("board_id, count(*) as n").
		Group("board_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[uint]int64, len(rows))
	for _, r := range rows {
		counts[r.BoardID] = r.N
	}
	return counts, nil
}

// ThreadSummary is one thread rolled up to its root: Replies is the subtree
// size minus one (all descendants, not just direct children) and LastActivity
// is the newest CreatedAt anywhere in the subtree.
type ThreadSummary struct {
	Root         Post
	Replies      int
	LastActivity time.Time
}

// ThreadSummaries groups a whole-board, insertion-ordered (id ASC) post slice
// — BoardPosts' contract — into per-root summaries, most recently active
// first. Insertion order guarantees a parent is seen before its replies, so
// one linear pass resolves every post's root. A reply whose parent is missing
// from the fetch cannot happen (parents are same-board by CreatePost
// validation), but if an ordering regression ever produced one it becomes its
// own root — visible, never dropped.
func ThreadSummaries(posts []Post) []ThreadSummary {
	rootOf := make(map[uint]uint, len(posts))
	index := make(map[uint]int) // root post ID -> position in threads
	threads := []ThreadSummary{}
	for _, p := range posts {
		if p.ParentID != nil {
			if root, ok := rootOf[*p.ParentID]; ok {
				rootOf[p.ID] = root
				t := &threads[index[root]]
				t.Replies++
				if p.CreatedAt.After(t.LastActivity) {
					t.LastActivity = p.CreatedAt
				}
				continue
			}
			// unresolved parent: fall through, p becomes its own root
		}
		rootOf[p.ID] = p.ID
		index[p.ID] = len(threads)
		threads = append(threads, ThreadSummary{Root: p, LastActivity: p.CreatedAt})
	}
	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i].LastActivity.After(threads[j].LastActivity)
	})
	return threads
}

// GroupThreads splits a whole-board, insertion-ordered (id ASC) post slice
// into one slice per thread, roots in input (chronological) order and each
// thread's posts parents-before-children (render.Depths' documented
// precondition). Orphans get the same never-dropped fallback as
// ThreadSummaries: an unresolved parent makes the post its own root.
func GroupThreads(posts []Post) [][]Post {
	rootOf := make(map[uint]uint, len(posts))
	index := make(map[uint]int) // root post ID -> position in threads
	var threads [][]Post
	for _, p := range posts {
		root := p.ID // top-level post — or orphan fallback: its own root
		if p.ParentID != nil {
			if r, ok := rootOf[*p.ParentID]; ok {
				root = r
			}
		}
		rootOf[p.ID] = root
		i, ok := index[root]
		if !ok {
			i = len(threads)
			index[root] = i
			threads = append(threads, nil)
		}
		threads[i] = append(threads[i], p)
	}
	return threads
}
