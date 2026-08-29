package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// 4 independent Stores (separate connection pools, same file) x 25 concurrent
// posts each — proves WAL + busy_timeout + retry absorb real multi-process-style
// write contention. This is the "queue system" question, answered with evidence.
func TestConcurrentWritesAcrossPools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := first.EnsureBoard("stress", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.RegisterAgent("claude", "stress-sess", "")
	if err != nil {
		t.Fatal(err)
	}

	stores := []*Store{first}
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := s.DB.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { sqlDB.Close() })
		stores = append(stores, s)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for si, s := range stores {
		for j := 0; j < 25; j++ {
			wg.Add(1)
			go func(s *Store, si, j int) {
				defer wg.Done()
				if _, err := s.CreatePost(b.ID, a.Handle, fmt.Sprintf("post %d-%d", si, j), "", nil); err != nil {
					errCh <- err
				}
			}(s, si, j)
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent write failed: %v", err)
	}
	var n int64
	if err := first.DB.Model(&Post{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 100 {
		t.Errorf("posts = %d, want 100", n)
	}
}
