package store

import (
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	DB *gorm.DB
}

func DefaultPath() string {
	if p := os.Getenv("XFA_DB"); p != "" {
		return p
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "xfa", "board.db")
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// _txlock=immediate: gorm's write txns BEGIN IMMEDIATE, so busy_timeout
	// applies at BEGIN instead of failing fast on the deferred read->write
	// lock upgrade (the SQLITE_BUSY class that ignores busy_timeout).
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// one connection per Store: any future db.Transaction()/Begin() that
	// queries the outer DB would self-deadlock — do not introduce gorm
	// transactions
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&Board{}, &Project{}, &Agent{}, &Post{}, &ReadCursor{}, &Reminder{}, &Mention{}, &Session{}, &PostLink{}); err != nil {
		return nil, err
	}
	if err := backfillReadCursors(db); err != nil {
		return nil, err
	}
	if err := migrateFTS(db); err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}

// backfillReadCursors converts pre-2026-08-28 time-only cursor rows to the id
// cursor exactly once: the last post on the row's board at or before its
// last_read_at. Idempotent (WHERE last_read_id IS NULL — AutoMigrate adds the
// column nullable, and fresh rows are written as 0, never NULL); without it
// every existing agent would re-read its whole board.
//
// Gated on a SELECT so steady-state Open stays write-free: an unconditional
// UPDATE takes the WAL write lock on every Open, which made every read-only
// command and every hook stall behind a concurrent writer (busy_timeout, then
// SQLITE_BUSY). Stays after AutoMigrate so rows an older binary inserts during
// a mixed-version window self-heal on the next Open.
func backfillReadCursors(db *gorm.DB) error {
	var legacy []int
	if err := db.Raw(`SELECT 1 FROM read_cursors WHERE last_read_id IS NULL LIMIT 1`).Scan(&legacy).Error; err != nil {
		return err
	}
	if len(legacy) == 0 {
		return nil
	}
	return db.Exec(`UPDATE read_cursors SET last_read_id = (
		SELECT COALESCE(MAX(p.id), 0) FROM posts p
		WHERE p.board_id = read_cursors.board_id
		  AND strftime('%Y-%m-%d %H:%M:%f', p.created_at) <= strftime('%Y-%m-%d %H:%M:%f', read_cursors.last_read_at)
	) WHERE last_read_id IS NULL`).Error
}
