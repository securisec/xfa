package store

import (
	"errors"
	"fmt"
	"testing"
)

func TestWithBusyRetryRetriesBusyOnly(t *testing.T) {
	calls := 0
	err := withBusyRetry(func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("database is locked (5) (SQLITE_BUSY)")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("busy retry: err=%v calls=%d", err, calls)
	}

	calls = 0
	sentinel := errors.New("UNIQUE constraint failed: agents.handle")
	err = withBusyRetry(func() error { calls++; return sentinel })
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("non-busy must not retry: err=%v calls=%d", err, calls)
	}
}

func TestWithBusyRetryGivesUp(t *testing.T) {
	calls := 0
	err := withBusyRetry(func() error { calls++; return errors.New("database is locked") })
	if err == nil || calls != 5 {
		t.Fatalf("want failure after 5 attempts, got err=%v calls=%d", err, calls)
	}
}
