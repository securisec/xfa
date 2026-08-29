package store

import "testing"

func TestStats(t *testing.T) {
	s, b, a := seed(t)
	other, _ := s.RegisterAgent("claude", "o-sess", "")
	s.CreatePost(b.ID, a.Handle, "one", "", nil)
	s.CreatePost(b.ID, a.Handle, "two", "question", nil)
	s.CreatePost(b.ID, other.Handle, "three", "", nil)

	st, err := s.Stats(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Posts != 3 || st.Agents != 2 || st.OpenQuestions != 1 || st.Posts24h != 3 {
		t.Errorf("stats = %+v", st)
	}
	if len(st.TopPosters) != 2 || st.TopPosters[0].Handle != a.Handle || st.TopPosters[0].Count != 2 {
		t.Errorf("top posters = %+v", st.TopPosters)
	}
}

// Equal post counts tie-break by ascending handle, so the ordering is
// deterministic regardless of insertion or scan order.
func TestStatsTopPosterTieBreak(t *testing.T) {
	s, b, a := seed(t)
	other, _ := s.RegisterAgent("claude", "o-sess", "")
	// Insert in descending-handle order so a stable-by-insertion tie can't
	// pass by accident.
	first, second := a.Handle, other.Handle
	if first < second {
		first, second = second, first
	}
	s.CreatePost(b.ID, first, "one", "", nil)
	s.CreatePost(b.ID, second, "two", "", nil)

	st, err := s.Stats(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.TopPosters) != 2 || st.TopPosters[0].Count != 1 || st.TopPosters[1].Count != 1 {
		t.Fatalf("top posters = %+v", st.TopPosters)
	}
	if st.TopPosters[0].Handle >= st.TopPosters[1].Handle {
		t.Errorf("tie not broken by ascending handle: %+v", st.TopPosters)
	}
}

// Stats(0) spans all boards; tombstoned posts still count toward totals and
// top posters (tombstones are never filtered, v1 invariant).
func TestStatsAllBoardsAndTombstones(t *testing.T) {
	s, b, a := seed(t)
	b2, _ := s.EnsureBoard("second", "")
	p, _ := s.CreatePost(b.ID, a.Handle, "doomed", "", nil)
	if err := s.Tombstone(p.ID, a.Handle); err != nil {
		t.Fatal(err)
	}
	s.CreatePost(b2.ID, a.Handle, "elsewhere", "question", nil)

	st, err := s.Stats(0)
	if err != nil {
		t.Fatal(err)
	}
	if st.Posts != 2 || st.Agents != 1 || st.OpenQuestions != 1 {
		t.Errorf("all-board stats = %+v", st)
	}
	if len(st.TopPosters) != 1 || st.TopPosters[0].Count != 2 {
		t.Errorf("top posters = %+v", st.TopPosters)
	}

	// Scoped to b: the tombstoned post counts, b2's question does not.
	st, err = s.Stats(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Posts != 1 || st.OpenQuestions != 0 {
		t.Errorf("board-scoped stats = %+v", st)
	}
}
