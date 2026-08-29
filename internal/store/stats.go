package store

import (
	"time"

	"gorm.io/gorm"
)

type PosterCount struct {
	Handle string
	Count  int64
}

// BoardStats is an activity snapshot. Post counts and top posters include
// tombstoned posts (tombstones are activity, never filtered — v1 invariant);
// OpenQuestions matches the `xfa questions` view, which excludes them.
type BoardStats struct {
	Posts         int64
	Posts24h      int64
	Agents        int64
	OpenQuestions int64
	TopPosters    []PosterCount
}

// Stats computes BoardStats for one board, or for all boards when boardID
// is 0. Agents is the distinct author_handle count in scope; TopPosters is
// the top 5 by all-time post count.
func (s *Store) Stats(boardID uint) (*BoardStats, error) {
	st := &BoardStats{}
	scope := func() *gorm.DB { // fresh scoped query each use
		q := s.DB.Model(&Post{})
		if boardID != 0 {
			q = q.Where("board_id = ?", boardID)
		}
		return q
	}
	if err := scope().Count(&st.Posts).Error; err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	if err := scope().Where(sinceExpr, sinceArg(cutoff)).Count(&st.Posts24h).Error; err != nil {
		return nil, err
	}
	if err := scope().Distinct("author_handle").Count(&st.Agents).Error; err != nil {
		return nil, err
	}
	var err error
	if st.OpenQuestions, err = s.OpenQuestionCount(boardID); err != nil {
		return nil, err
	}
	if err := scope().Select("author_handle as handle, count(*) as count").
		Group("author_handle").Order("count DESC, author_handle ASC").Limit(5).
		Scan(&st.TopPosters).Error; err != nil {
		return nil, err
	}
	return st, nil
}
