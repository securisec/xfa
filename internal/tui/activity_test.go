package tui

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// newActivity seeds a board (seedTUI) and snapshots a poller on its OWN
// second store, the way cmd/tui.go wires it: writes through the returned
// seed store are foreign commits, so data_version moves and ticks scan.
func newActivity(t *testing.T) (*store.Store, *store.Board, *Activity, *bytes.Buffer) {
	t.Helper()
	s, b := seedTUI(t)
	ps, err := store.Open(os.Getenv("XFA_DB"))
	if err != nil {
		t.Fatalf("open poller store: %v", err)
	}
	var buf bytes.Buffer
	a, err := NewActivity(ps, &buf)
	if err != nil {
		t.Fatalf("NewActivity: %v", err)
	}
	return s, b, a, &buf
}

func lines(buf *bytes.Buffer) []string {
	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
}

func TestActivitySnapshotIsQuiet(t *testing.T) {
	_, _, a, buf := newActivity(t)
	a.tick(context.Background())
	a.tick(context.Background())
	if buf.Len() != 0 {
		t.Fatalf("seeded resolved/tombstoned/2 boards must not replay:\n%s", buf)
	}
}

func TestActivityPostAndReply(t *testing.T) {
	s, b, a, buf := newActivity(t)
	ag, _ := s.RegisterAgent("claude", "w", "")
	p, _ := s.CreatePost(b.ID, ag.Handle, "first line here\nsecond line", "til", nil)
	a.tick(context.Background())
	got := lines(buf)
	if len(got) != 1 {
		t.Fatalf("want 1 line, got %d:\n%s", len(got), buf)
	}
	for _, want := range []string{"post", "#" + itoa(p.ID), "[til]", ag.Handle, "b/tuiboard", "first line here"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("post line %q lacks %q", got[0], want)
		}
	}
	if strings.Contains(got[0], "second line") {
		t.Errorf("only the first body line belongs on the log line: %q", got[0])
	}

	buf.Reset()
	r, _ := s.CreatePost(b.ID, ag.Handle, "a reply", "", &p.ID)
	a.tick(context.Background())
	got = lines(buf)
	if len(got) != 1 {
		t.Fatalf("want 1 line, got %d:\n%s", len(got), buf)
	}
	for _, want := range []string{"reply", "#" + itoa(r.ID), "↳ #" + itoa(p.ID), "a reply"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("reply line %q lacks %q", got[0], want)
		}
	}
	buf.Reset()
	a.tick(context.Background())
	if buf.Len() != 0 {
		t.Fatalf("idle tick must print nothing:\n%s", buf)
	}
}

func TestActivityResolveAndDelete(t *testing.T) {
	s, b, a, buf := newActivity(t)
	ag, _ := s.RegisterAgent("claude", "w", "")
	q, _ := s.CreatePost(b.ID, ag.Handle, "q?", "question", nil)
	d, _ := s.CreatePost(b.ID, ag.Handle, "doomed", "", nil)
	a.tick(context.Background())
	buf.Reset()

	other, _ := s.RegisterAgent("claude", "w2", "")
	if err := s.Resolve(q.ID, other.Handle); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	a.tick(context.Background())
	got := lines(buf)
	if len(got) != 1 {
		t.Fatalf("want 1 line, got %d:\n%s", len(got), buf)
	}
	for _, want := range []string{"resolve", "#" + itoa(q.ID), "✓", "by " + other.Handle, "b/tuiboard"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("resolve line %q lacks %q", got[0], want)
		}
	}
	buf.Reset()
	a.tick(context.Background())
	if buf.Len() != 0 {
		t.Fatalf("a resolve must print once:\n%s", buf)
	}

	if err := s.Tombstone(d.ID, ag.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}
	a.tick(context.Background())
	got = lines(buf)
	if len(got) != 1 || !strings.Contains(got[0], "delete") || !strings.Contains(got[0], "#"+itoa(d.ID)) {
		t.Fatalf("want one delete line for #%d, got:\n%s", d.ID, buf)
	}
	if strings.Contains(got[0], "doomed") {
		t.Errorf("delete line must not leak the body: %q", got[0])
	}
	buf.Reset()

	// create+tombstone and create+resolve inside one tick each print ONCE,
	// already carrying the mark.
	dd, _ := s.CreatePost(b.ID, ag.Handle, "gone fast", "", nil)
	_ = s.Tombstone(dd.ID, ag.Handle)
	qq, _ := s.CreatePost(b.ID, ag.Handle, "fast q", "question", nil)
	_ = s.Resolve(qq.ID, ag.Handle)
	a.tick(context.Background())
	got = lines(buf)
	if len(got) != 2 {
		t.Fatalf("want exactly 2 lines, got %d:\n%s", len(got), buf)
	}
	if !strings.Contains(got[0], "post") || !strings.Contains(got[0], "[deleted]") || strings.Contains(got[0], "gone fast") {
		t.Errorf("create+tombstone must print once as a masked post: %q", got[0])
	}
	if !strings.Contains(got[1], "post") || !strings.Contains(got[1], "✓") {
		t.Errorf("create+resolve must print once as a resolved post: %q", got[1])
	}
	buf.Reset()
	a.tick(context.Background())
	if buf.Len() != 0 {
		t.Fatalf("no second line for marks seen at creation:\n%s", buf)
	}
}

func TestActivityNewBoard(t *testing.T) {
	s, _, a, buf := newActivity(t)
	nb, _ := s.EnsureBoard("fresh", "")
	a.tick(context.Background())
	got := lines(buf)
	if len(got) != 1 || !strings.Contains(got[0], "board") || !strings.Contains(got[0], "b/fresh created") {
		t.Fatalf("want a board-created line, got:\n%s", buf)
	}
	buf.Reset()
	ag, _ := s.RegisterAgent("claude", "w", "")
	s.CreatePost(nb.ID, ag.Handle, "hello fresh", "", nil)
	a.tick(context.Background())
	if got := lines(buf); len(got) != 1 || !strings.Contains(got[0], "b/fresh") {
		t.Fatalf("post on the new board must carry its slug, got:\n%s", buf)
	}
}

func TestActivityLineIsCleanAndTruncated(t *testing.T) {
	s, b, a, buf := newActivity(t)
	termWidth = func() int { return 60 }
	t.Cleanup(func() { termWidth = stdoutWidth })
	ag, _ := s.RegisterAgent("claude", "w", "")
	s.CreatePost(b.ID, ag.Handle, "bell\x07 and \x1b[31mred "+strings.Repeat("x", 200)+"\nmore", "", nil)
	a.tick(context.Background())
	got := lines(buf)
	if len(got) != 1 {
		t.Fatalf("want 1 line, got %d:\n%s", len(got), buf)
	}
	if strings.ContainsAny(got[0], "\x07\x1b") {
		t.Errorf("control bytes must be stripped: %q", got[0])
	}
	if n := len([]rune(got[0])); n > 60 || !strings.HasSuffix(got[0], "…") {
		t.Errorf("line must be truncated to width 60 with an ellipsis (len %d): %q", n, got[0])
	}
}

func TestActivityErrorPrintsOnce(t *testing.T) {
	_, _, a, buf := newActivity(t)
	if sqlDB, err := a.s.DB.DB(); err == nil {
		sqlDB.Close()
	}
	for i := 0; i < 3; i++ {
		a.tick(context.Background())
	}
	got := lines(buf)
	if len(got) != 1 || !strings.HasPrefix(got[0], "activity:") {
		t.Fatalf("a dead DB must print exactly one activity: line, got:\n%s", buf)
	}
}

func TestActivityCancelledContextIsSilent(t *testing.T) {
	s, b, a, buf := newActivity(t)
	ag, _ := s.RegisterAgent("claude", "w", "")
	s.CreatePost(b.ID, ag.Handle, "late", "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.tick(ctx)
	if buf.Len() != 0 {
		t.Fatalf("cancelled ctx must write nothing:\n%s", buf)
	}
}

// A failed scan must not advance the data_version gate, or the events it
// missed sit unprinted until the NEXT foreign commit. Breaking the schema
// through the poller's OWN connection doesn't move its data_version, so the
// second tick below only scans if the first failure left the gate open.
func TestActivityScanErrorKeepsGateOpen(t *testing.T) {
	s, b, a, buf := newActivity(t)
	ag, _ := s.RegisterAgent("claude", "w", "")
	s.CreatePost(b.ID, ag.Handle, "parked?", "", nil) // foreign commit: gate opens
	if err := a.s.DB.Exec("ALTER TABLE posts RENAME TO posts_broken").Error; err != nil {
		t.Fatalf("break schema: %v", err)
	}
	a.tick(context.Background())
	if got := lines(buf); len(got) != 1 || !strings.HasPrefix(got[0], "activity:") {
		t.Fatalf("broken scan must print one activity: line, got:\n%s", buf)
	}
	buf.Reset()
	if err := a.s.DB.Exec("ALTER TABLE posts_broken RENAME TO posts").Error; err != nil {
		t.Fatalf("restore schema: %v", err)
	}
	a.tick(context.Background())
	if got := lines(buf); len(got) != 1 || !strings.Contains(got[0], "parked?") {
		t.Fatalf("recovered scan must print the parked post, got:\n%s", buf)
	}
}

// C1: a post created AND marked in the gap between PostsAfter and
// MarkedPostIDs is above the watermark; mark() must leave it to the next
// PostsAfter (which prints it once, mark included) — never a resolve/delete
// line before its own post line.
func TestActivityMarkAboveWatermarkWaits(t *testing.T) {
	s, b, a, buf := newActivity(t)
	ag, _ := s.RegisterAgent("claude", "w", "")
	q, _ := s.CreatePost(b.ID, ag.Handle, "fast q", "question", nil)
	_ = s.Resolve(q.ID, ag.Handle)
	marks, _ := a.s.MarkedPostIDs()
	if err := a.mark(context.Background(), marks); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("mark above lastID must print nothing:\n%s", buf)
	}
	a.tick(context.Background())
	if got := lines(buf); len(got) != 1 || !strings.Contains(got[0], "post") || !strings.Contains(got[0], "✓") {
		t.Fatalf("the next scan must print it once as a resolved post, got:\n%s", buf)
	}
}

// C2: a mark whose post vanished (web hard delete racing the scan) is skipped,
// not an error.
func TestActivityMarkOnMissingPostIsSkipped(t *testing.T) {
	s, _, a, buf := newActivity(t)
	gone := uint(a.lastID) // the seeded tombstoned reply, below the watermark
	if err := s.DB.Exec("DELETE FROM posts WHERE id = ?", gone).Error; err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	err := a.mark(context.Background(), []store.PostMark{{ID: gone, Resolved: true, Tombstoned: true}})
	if err != nil || buf.Len() != 0 {
		t.Fatalf("missing post: err=%v out=%q, want nil and nothing", err, buf)
	}
}

// C3: after a recovery, the same error recurring prints again.
func TestActivityErrorReprintsAfterRecovery(t *testing.T) {
	s, b, a, buf := newActivity(t)
	ag, _ := s.RegisterAgent("claude", "w", "")
	brk := func() {
		if err := a.s.DB.Exec("ALTER TABLE posts RENAME TO posts_broken").Error; err != nil {
			t.Fatalf("break: %v", err)
		}
	}
	fix := func() {
		if err := a.s.DB.Exec("ALTER TABLE posts_broken RENAME TO posts").Error; err != nil {
			t.Fatalf("fix: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		s.CreatePost(b.ID, ag.Handle, "p", "", nil)
		brk()
		a.tick(context.Background())
		fix()
		a.tick(context.Background())
	}
	var errs int
	for _, l := range lines(buf) {
		if strings.HasPrefix(l, "activity:") {
			errs++
		}
	}
	if errs != 2 {
		t.Fatalf("want the error printed once per outage (2), got %d:\n%s", errs, buf)
	}
}

// S3: Run polls at activityInterval and stops on ctx cancel.
func TestActivityRunPollsAndStops(t *testing.T) {
	s, b := seedTUI(t)
	ps, err := store.Open(os.Getenv("XFA_DB"))
	if err != nil {
		t.Fatalf("open poller store: %v", err)
	}
	buf := &lockedBuf{}
	a, err := NewActivity(ps, buf)
	if err != nil {
		t.Fatalf("NewActivity: %v", err)
	}
	activityInterval = time.Millisecond
	t.Cleanup(func() { activityInterval = 2 * time.Second })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	ag, _ := s.RegisterAgent("claude", "w", "")
	s.CreatePost(b.ID, ag.Handle, "landed after start", "", nil)
	deadline := time.After(5 * time.Second)
	for !strings.Contains(buf.String(), "landed after start") {
		select {
		case <-deadline:
			t.Fatalf("Run never printed the post:\n%s", buf)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return on ctx cancel")
	}
}

// lockedBuf is a race-free writer for the Run test (Run writes from its own
// goroutine while the test polls).
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func itoa(id uint) string { return strconv.FormatUint(uint64(id), 10) }
