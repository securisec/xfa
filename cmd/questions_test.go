package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// Pins Task 3's second nudge: every open question line carries its live reply
// count so answered-but-unresolved questions are visible at a glance.
func TestQuestionsShowsReplyCounts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdquestions", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	asker, err := s.RegisterAgent("claude", "asker-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	solver, err := s.RegisterAgent("claude", "solver-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	answered := mustPostTagged(t, s, b.ID, asker.Handle, "how do hooks fail open?", "question", nil)
	mustPost(t, s, b.ID, asker.Handle, "is FTS5 case-sensitive?", nil) // untagged, must not appear
	quiet := mustPostTagged(t, s, b.ID, asker.Handle, "who owns b/general?", "question", nil)
	mustPost(t, s, b.ID, solver.Handle, "they return exit 0 always", &answered.ID)
	dead := mustPost(t, s, b.ID, solver.Handle, "wrong answer", &answered.ID)
	if err := s.Tombstone(dead.ID, solver.Handle); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	out := runXfa(t, "questions", "--board", "cmdquestions", "--all=false", "--json=false")
	if !strings.Contains(out, "1 reply") {
		t.Errorf("answered question must show its live reply count (1 reply):\n%s", out)
	}
	if !strings.Contains(out, "0 replies") {
		t.Errorf("quiet question must show 0 replies:\n%s", out)
	}

	// --json keeps the flat post shape and gains a Replies field.
	out = runXfa(t, "questions", "--board", "cmdquestions", "--all=false", "--json")
	var got []struct {
		ID      uint
		Body    string
		Tag     string
		Replies int64
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad --json output %q: %v", out, err)
	}
	replies := map[uint]int64{}
	for _, q := range got {
		replies[q.ID] = q.Replies
	}
	if len(got) != 2 || replies[answered.ID] != 1 || replies[quiet.ID] != 0 {
		t.Fatalf("--json = %+v; want 2 open questions with replies 1 and 0", got)
	}
}

// questionsFixture seeds a board with one question from a long-idle asker and
// one from a just-registered asker, and returns their post IDs.
func questionsFixture(t *testing.T, board string, idleFor time.Duration) (*store.Store, uint, uint) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard(board, "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	idle, err := s.RegisterAgent("claude", "idle-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	fresh, err := s.RegisterAgent("claude", "fresh-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	stale := mustPostTagged(t, s, b.ID, idle.Handle, "does WAL survive a hard kill?", "question", nil)
	live := mustPostTagged(t, s, b.ID, fresh.Handle, "where does the skill install?", "question", nil)
	// Backdate after posting: CreatePost heartbeats its author, so an asker
	// only reads as idle once nothing has happened since they asked.
	if err := s.DB.Model(&store.Agent{}).Where("handle = ?", idle.Handle).
		Update("last_seen_at", time.Now().Add(-idleFor)).Error; err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}
	return s, stale.ID, live.ID
}

// A question from an asker idle past StaleReplyAge is annotated with how long
// ago they were seen plus the answer-anyway nudge. Language stays probabilistic
// ("last seen"), never binary — last_seen_at under-counts by design. A
// just-registered asker gets no annotation at all.
func TestQuestionsAnnotatesStaleAsker(t *testing.T) {
	_, staleID, liveID := questionsFixture(t, "cmdstale", store.StaleReplyAge+30*time.Minute)

	out := runXfa(t, "questions", "--board", "cmdstale", "--all=false", "--json=false")
	var staleLine, liveLine string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case strings.HasPrefix(l, fmt.Sprintf("#%d ", staleID)):
			staleLine = l
		case strings.HasPrefix(l, fmt.Sprintf("#%d ", liveID)):
			liveLine = l
		}
	}
	if staleLine == "" || liveLine == "" {
		t.Fatalf("both questions must be listed, got:\n%s", out)
	}
	if !strings.Contains(staleLine, "asker last seen") {
		t.Errorf("idle asker's line must say when they were last seen:\n%s", staleLine)
	}
	if !strings.Contains(staleLine, "answer for the record; anyone may resolve") {
		t.Errorf("past StaleReplyAge the line must nudge answering anyway:\n%s", staleLine)
	}
	for _, banned := range []string{"gone", "dead", "offline"} {
		if strings.Contains(staleLine, banned) {
			t.Errorf("annotation must stay probabilistic, found %q in:\n%s", banned, staleLine)
		}
	}
	if strings.Contains(liveLine, "asker last seen") {
		t.Errorf("fresh asker must get no annotation:\n%s", liveLine)
	}
}

// --json gains exactly one key, and only for stale askers: fresh entries must
// stay byte-identical to the pre-annotation shape (omitempty).
func TestQuestionsJSONHasAskerLastSeen(t *testing.T) {
	_, staleID, liveID := questionsFixture(t, "cmdstalejson", time.Hour)

	out := runXfa(t, "questions", "--board", "cmdstalejson", "--all=false", "--json")
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("bad --json output %q: %v", out, err)
	}
	byID := map[uint]map[string]json.RawMessage{}
	for _, e := range raw {
		var id uint
		if err := json.Unmarshal(e["ID"], &id); err != nil {
			t.Fatalf("entry without ID: %v", err)
		}
		byID[id] = e
	}
	field, ok := byID[staleID]["asker_last_seen_at"]
	if !ok {
		t.Fatalf("stale asker's entry must carry asker_last_seen_at, got %v", byID[staleID])
	}
	var seen time.Time
	if err := json.Unmarshal(field, &seen); err != nil {
		t.Fatalf("asker_last_seen_at %s does not parse as a time: %v", field, err)
	}
	if age := time.Since(seen); age < 55*time.Minute || age > 65*time.Minute {
		t.Errorf("asker_last_seen_at age = %v, want ~1h", age)
	}
	// omitempty + display gating: a fresh asker's entry is byte-identical to
	// the pre-annotation shape, so existing --json consumers see no change.
	if _, present := byID[liveID]["asker_last_seen_at"]; present {
		t.Errorf("fresh asker's entry must omit asker_last_seen_at, got %s", byID[liveID]["asker_last_seen_at"])
	}
}

// mustPostTagged is mustPost with a tag (mustPost lives in boardview_test.go).
func mustPostTagged(t *testing.T, s *store.Store, boardID uint, author, body, tag string, parentID *uint) *store.Post {
	t.Helper()
	p, err := s.CreatePost(boardID, author, body, tag, parentID)
	if err != nil {
		t.Fatalf("CreatePost(%q): %v", body, err)
	}
	return p
}
