package store

// LinkRef names the far end of a post link for display and navigation:
// the other post's id, the id of the thread root it lives under (its own
// id when it is a root), and its board's slug.
type LinkRef struct {
	PostID    uint   `json:"post_id"`
	ThreadID  uint   `json:"thread_id"`
	BoardSlug string `json:"board_slug"`
}

// LinkSets carries both directions for a set of posts, keyed by post id.
// Maps are always non-nil; a post with no links is simply absent.
type LinkSets struct {
	Out map[uint][]LinkRef // posts this post's body references
	In  map[uint][]LinkRef // posts whose bodies reference this post
}

// linkMeta resolves thread root + board slug for a set of post ids in one
// recursive walk up the parent chain (roots have parent_id IS NULL; a link
// target's board is by construction the board of its root).
func (s *Store) linkMeta(ids []uint) (map[uint]LinkRef, error) {
	out := map[uint]LinkRef{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		StartID   uint
		ThreadID  uint
		BoardSlug string
	}
	err := s.DB.Raw(`WITH RECURSIVE up(start_id, id, parent_id, board_id) AS (
			SELECT id, id, parent_id, board_id FROM posts WHERE id IN ?
			UNION ALL
			SELECT up.start_id, p.id, p.parent_id, p.board_id
			FROM posts p JOIN up ON p.id = up.parent_id
		)
		SELECT up.start_id AS start_id, up.id AS thread_id, b.slug AS board_slug
		FROM up JOIN boards b ON b.id = up.board_id
		WHERE up.parent_id IS NULL`, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.StartID] = LinkRef{PostID: r.StartID, ThreadID: r.ThreadID, BoardSlug: r.BoardSlug}
	}
	return out, nil
}

// LinksFor returns both link directions for the given posts. Tombstoned
// far ends are included (threads don't collapse; neither do links) —
// only hard deletes remove rows, via hardDeleteDoomed's cascade.
func (s *Store) LinksFor(postIDs []uint) (LinkSets, error) {
	ls := LinkSets{Out: map[uint][]LinkRef{}, In: map[uint][]LinkRef{}}
	if len(postIDs) == 0 {
		return ls, nil
	}
	var rows []struct {
		SourcePostID uint
		TargetPostID uint
	}
	err := s.DB.Raw(`SELECT source_post_id, target_post_id FROM post_links
		WHERE source_post_id IN ? OR target_post_id IN ? ORDER BY id`,
		postIDs, postIDs).Scan(&rows).Error
	if err != nil {
		return ls, err
	}
	if len(rows) == 0 {
		return ls, nil
	}
	want := map[uint]bool{}
	for _, id := range postIDs {
		want[id] = true
	}
	farSet := map[uint]bool{}
	for _, r := range rows {
		if want[r.SourcePostID] {
			farSet[r.TargetPostID] = true
		}
		if want[r.TargetPostID] {
			farSet[r.SourcePostID] = true
		}
	}
	far := make([]uint, 0, len(farSet))
	for id := range farSet {
		far = append(far, id)
	}
	meta, err := s.linkMeta(far)
	if err != nil {
		return ls, err
	}
	for _, r := range rows {
		if want[r.SourcePostID] {
			if ref, ok := meta[r.TargetPostID]; ok {
				ls.Out[r.SourcePostID] = append(ls.Out[r.SourcePostID], ref)
			}
		}
		if want[r.TargetPostID] {
			if ref, ok := meta[r.SourcePostID]; ok {
				ls.In[r.TargetPostID] = append(ls.In[r.TargetPostID], ref)
			}
		}
	}
	return ls, nil
}
