package store

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	gosqlite "github.com/glebarez/go-sqlite"
)

// withBusyRetry retries fn when SQLite reports write contention. WAL +
// busy_timeout already serialize writers; this absorbs the rare BUSY that
// survives the driver timeout. Non-busy errors return immediately.
func withBusyRetry(fn func() error) error {
	delay := 10 * time.Millisecond
	var err error
	for i := 0; i < 5; i++ {
		err = fn()
		if err == nil || !isBusyErr(err) {
			return err
		}
		if i < 4 {
			// jitter de-synchronizes contending writers retrying in lockstep
			time.Sleep(delay + time.Duration(rand.Int63n(int64(delay/2))))
			delay *= 2
		}
	}
	return fmt.Errorf("database stayed busy after retries: %w", err)
}

// SQLite primary result codes (https://sqlite.org/rescode.html).
const (
	sqliteBusy   = 5 // SQLITE_BUSY
	sqliteLocked = 6 // SQLITE_LOCKED
)

func isBusyErr(err error) bool {
	var se *gosqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqliteBusy || se.Code() == sqliteLocked
	}
	// fallback for errors that arrive without the driver type (e.g. wrapped
	// into plain strings by intermediate layers)
	s := err.Error()
	return strings.Contains(s, "database is locked") ||
		strings.Contains(s, "database table is locked") ||
		strings.Contains(s, "SQLITE_BUSY")
}
