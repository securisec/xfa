package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOpenMigratesSchema(t *testing.T) {
	s := openTemp(t)
	for _, table := range []string{"boards", "projects", "agents", "posts", "reminders"} {
		if !s.DB.Migrator().HasTable(table) {
			t.Errorf("missing table %s", table)
		}
	}
	var mode string
	s.DB.Raw("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestDefaultPathResolutionOrder(t *testing.T) {
	tests := []struct {
		name        string
		xfaDB       string
		xdgDataHome string
		want        string // exact match, unless wantSuffix
		wantSuffix  string
	}{
		{
			name:        "XFA_DB wins even when XDG_DATA_HOME is set",
			xfaDB:       "/tmp/x/board.db",
			xdgDataHome: "/tmp/xdg",
			want:        "/tmp/x/board.db",
		},
		{
			name:        "XDG_DATA_HOME when XFA_DB unset",
			xfaDB:       "",
			xdgDataHome: "/tmp/xdg",
			want:        filepath.Join("/tmp/xdg", "xfa", "board.db"),
		},
		{
			name:        "falls back to ~/.local/share when neither set",
			xfaDB:       "",
			xdgDataHome: "",
			wantSuffix:  filepath.Join(".local", "share", "xfa", "board.db"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// DefaultPath treats empty as unset, so t.Setenv("...", "")
			// exercises the unset branch (desired behavior).
			t.Setenv("XFA_DB", tt.xfaDB)
			t.Setenv("XDG_DATA_HOME", tt.xdgDataHome)
			got := DefaultPath()
			if tt.wantSuffix != "" {
				if !strings.HasSuffix(got, tt.wantSuffix) {
					t.Errorf("DefaultPath = %q, want suffix %q", got, tt.wantSuffix)
				}
				return
			}
			if got != tt.want {
				t.Errorf("DefaultPath = %q, want %q", got, tt.want)
			}
		})
	}
}

// Pre-2026-08-28 DBs have time-only cursor rows (last_read_id NULL). Open must
// convert each to the last post at or before its last_read_at, exactly once —
// otherwise every existing agent re-reads its whole board.
func TestOpenBackfillsLegacyReadCursors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.EnsureBoard("b", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	p1, _ := s.CreatePost(b.ID, a.Handle, "before cursor", "", nil)
	time.Sleep(2 * time.Millisecond)
	cursorAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	p2, _ := s.CreatePost(b.ID, a.Handle, "after cursor", "", nil)
	// Real legacy shape: the column does not exist yet. Reopening must ADD it
	// nullable (a `default:0` tag on LastReadID would fill 0 here and silently
	// re-flood every agent — this test is what makes that fail loudly).
	if err := s.DB.Exec(`ALTER TABLE read_cursors DROP COLUMN last_read_id`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Exec(`INSERT INTO read_cursors (handle, board_id, last_read_at) VALUES (?, ?, ?)`,
		"reader", b.ID, cursorAt).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := s.DB.DB()
	sqlDB.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.readCursorID(b.ID, "reader")
	if err != nil || id != p1.ID {
		t.Fatalf("backfill must land on the last post <= last_read_at (%d), got %d %v (p2=%d)", p1.ID, id, err, p2.ID)
	}
	// Idempotent: a second Open must not move an already-backfilled cursor.
	if err := s.MarkReadID("reader", b.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ = s.DB.DB()
	sqlDB.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := s.readCursorID(b.ID, "reader"); id != p2.ID {
		t.Fatalf("re-Open must not re-backfill: got %d want %d", id, p2.ID)
	}
}

// Steady-state Open must be write-free: with no legacy cursor rows, the
// backfill must not take the WAL write lock, or every read-only command and
// hook stalls behind any concurrent writer. Pin it by holding BEGIN IMMEDIATE
// on a second connection and requiring Open to return promptly.
func TestOpenIsWriteFreeWhenNothingToBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.EnsureBoard("b", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	if err := s.MarkReadID(a.Handle, b.ID, 0); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := s.DB.DB()
	sqlDB.Close()

	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tx, err := writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE read_cursors SET last_read_at = last_read_at`); err != nil {
		t.Fatal(err) // takes and holds the write lock
	}

	start := time.Now()
	if _, err := Open(path); err != nil {
		t.Fatalf("Open under a held write lock must not need to write: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Open waited %v on the write lock; steady-state Open must be write-free", d)
	}
}
