package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegisterAgentUniqueAndLineage(t *testing.T) {
	s := openTemp(t)
	lead, err := s.RegisterAgent("claude", "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.RegisterAgent("claude", "sess-1", lead.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Handle == lead.Handle {
		t.Error("handles must be unique")
	}
	if sub.ParentHandle != lead.Handle {
		t.Errorf("lineage lost: %q", sub.ParentHandle)
	}
}

func TestRegisterAgentManyUnique(t *testing.T) {
	s := openTemp(t)
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		a, err := s.RegisterAgent("claude", "sess-many", "")
		if err != nil {
			t.Fatalf("register #%d: %v", i, err)
		}
		if seen[a.Handle] {
			t.Fatalf("duplicate handle %q at #%d", a.Handle, i)
		}
		seen[a.Handle] = true
	}
}

func TestRegisterAgentSurfacesDBError(t *testing.T) {
	s := openTemp(t)
	if err := s.DB.Migrator().DropTable(&Agent{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	_, err := s.RegisterAgent("claude", "sess-x", "")
	if err == nil {
		t.Fatal("expected error after dropping agents table")
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Errorf("error must mention the real cause, got: %v", err)
	}
}

func TestTouchAndGetAgent(t *testing.T) {
	s := openTemp(t)
	a, err := s.RegisterAgent("opencode", "sess-t", "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	// TouchAgent is throttled to one write per HeartbeatThrottle, so backdate
	// last_seen_at past the window to exercise the advancing path.
	before := time.Now().Add(-2 * HeartbeatThrottle)
	if err := s.DB.Model(&Agent{}).Where("handle = ?", a.Handle).
		Update("last_seen_at", before).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.TouchAgent(a.Handle); err != nil {
		t.Fatalf("TouchAgent: %v", err)
	}
	got, err := s.GetAgent(a.Handle)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Provider != "opencode" || got.SessionID != "sess-t" || got.ParentHandle != "parent-1" {
		t.Errorf("fields lost in round-trip: %+v", got)
	}
	if !got.LastSeenAt.After(before) {
		t.Errorf("LastSeenAt did not advance: before=%v after=%v", before, got.LastSeenAt)
	}

	missing, err := s.GetAgent("no-such-handle")
	if !errors.Is(err, ErrNoAgent) {
		t.Errorf("want ErrNoAgent for unknown handle, got: %v", err)
	}
	if missing != nil {
		t.Errorf("GetAgent must return nil agent on error, got: %+v", missing)
	}
}

func TestTouchAgentThrottled(t *testing.T) {
	s := openTemp(t)
	a, err := s.RegisterAgent("claude", "sess-t", "")
	if err != nil {
		t.Fatal(err)
	}
	// Freshly registered => last_seen_at is now => a touch inside the
	// throttle window must NOT move it.
	before, _ := s.GetAgent(a.Handle)
	if err := s.TouchAgent(a.Handle); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetAgent(a.Handle)
	if !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Fatalf("touch inside throttle window moved last_seen_at: %v -> %v", before.LastSeenAt, after.LastSeenAt)
	}
	// Backdate past the throttle => touch must move it.
	old := time.Now().Add(-2 * HeartbeatThrottle)
	if err := s.DB.Model(&Agent{}).Where("handle = ?", a.Handle).
		Update("last_seen_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.TouchAgent(a.Handle); err != nil {
		t.Fatal(err)
	}
	after, _ = s.GetAgent(a.Handle)
	if !after.LastSeenAt.After(old.Add(time.Second)) {
		t.Fatalf("touch past throttle window did not advance last_seen_at: %v", after.LastSeenAt)
	}
}

func TestTouchAgentUnknownHandle(t *testing.T) {
	s := openTemp(t)
	if err := s.TouchAgent("no-such-handle-1"); err == nil {
		t.Fatal("expected error touching unknown handle")
	}
}

func TestLastSeenForBatch(t *testing.T) {
	s := openTemp(t)
	a, err := s.RegisterAgent("claude", "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RegisterAgent("claude", "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.LastSeenFor([]string{a.Handle, b.Handle, "ghost-fox-9"})
	if err != nil {
		t.Fatalf("LastSeenFor: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LastSeenFor returned %d entries, want 2: %+v", len(got), got)
	}
	for _, h := range []string{a.Handle, b.Handle} {
		if ts, ok := got[h]; !ok || ts.IsZero() {
			t.Errorf("missing/zero last_seen_at for %s: %v ok=%v", h, ts, ok)
		}
	}
	if _, ok := got["ghost-fox-9"]; ok {
		t.Error("unregistered handle must be absent from the map, not an error")
	}

	empty, err := s.LastSeenFor(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("LastSeenFor(nil) = %+v, %v; want empty map, nil error", empty, err)
	}
}

func TestSessionLeadAgent(t *testing.T) {
	s := openTemp(t)
	lead, err := s.RegisterAgent("claude", "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent("claude", "s1", lead.Handle); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionLeadAgent("s1")
	if err != nil || got == nil || got.Handle != lead.Handle {
		t.Fatalf("SessionLeadAgent(s1) = %+v, %v; want %s", got, err, lead.Handle)
	}

	// Two non-parented agents in one session: most recently seen wins.
	older, err := s.RegisterAgent("claude", "s2", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.RegisterAgent("claude", "s2", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&Agent{}).Where("handle = ?", older.Handle).
		Update("last_seen_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	got, err = s.SessionLeadAgent("s2")
	if err != nil || got == nil || got.Handle != newer.Handle {
		t.Fatalf("SessionLeadAgent(s2) = %+v, %v; want most recently seen %s", got, err, newer.Handle)
	}

	if _, err := s.SessionLeadAgent("empty"); !errors.Is(err, ErrNoAgent) {
		t.Errorf("SessionLeadAgent(empty) err = %v; want ErrNoAgent", err)
	}
}

func TestSessionHandles(t *testing.T) {
	s := openTemp(t)
	lead, err := s.RegisterAgent("claude", "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.RegisterAgent("claude", "s1", lead.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent("claude", "other", ""); err != nil {
		t.Fatal(err)
	}
	hs, err := s.SessionHandles("s1")
	if err != nil {
		t.Fatalf("SessionHandles: %v", err)
	}
	if len(hs) != 2 {
		t.Fatalf("SessionHandles(s1) = %v; want lead+child", hs)
	}
	seen := map[string]bool{hs[0]: true, hs[1]: true}
	if !seen[lead.Handle] || !seen[child.Handle] {
		t.Errorf("SessionHandles(s1) = %v; want %s and %s", hs, lead.Handle, child.Handle)
	}
	none, err := s.SessionHandles("nobody")
	if err != nil || len(none) != 0 {
		t.Errorf("SessionHandles(nobody) = %v, %v; want empty", none, err)
	}
}

func TestMarksRoundTrip(t *testing.T) {
	s := openTemp(t)
	if v, ok, err := s.GetMark("k"); err != nil || ok || v != "" {
		t.Fatalf("GetMark(unset) = %q, %v, %v; want \"\", false, nil", v, ok, err)
	}
	if err := s.SetMark("k", "5"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.GetMark("k"); err != nil || !ok || v != "5" {
		t.Fatalf("GetMark after set = %q, %v, %v; want \"5\", true, nil", v, ok, err)
	}
	if err := s.SetMark("k", "9"); err != nil {
		t.Fatalf("SetMark must upsert, not conflict: %v", err)
	}
	if v, ok, err := s.GetMark("k"); err != nil || !ok || v != "9" {
		t.Fatalf("GetMark after upsert = %q, %v, %v; want \"9\", true, nil", v, ok, err)
	}

	// The Stop hook's fire-once rows (plain Create, unique-violation guard)
	// must keep working alongside marks, with an empty Value.
	if err := s.DB.Create(&Reminder{SessionID: "stop-sess"}).Error; err != nil {
		t.Fatalf("fire-once reminder create broke: %v", err)
	}
	if err := s.DB.Create(&Reminder{SessionID: "stop-sess"}).Error; err == nil {
		t.Error("second fire-once create must violate the unique index")
	}
	if v, ok, err := s.GetMark("stop-sess"); err != nil || !ok || v != "" {
		t.Fatalf("GetMark(fire-once row) = %q, %v, %v; want \"\", true, nil", v, ok, err)
	}
}
