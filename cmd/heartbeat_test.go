package cmd

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// postArg renders a post id as the CLI argument the commands parse.
func postArg(id uint) string { return strconv.FormatUint(uint64(id), 10) }

// TestActingHandleCommandsHeartbeat pins the liveness contract for commands
// that act as a handle but never write a post: `read --unread`, `inbox`,
// `resolve` and `delete` must advance their acting handle's last_seen_at.
// Without this, a long-lived agent that only reads and answers looks dead to
// `xfa agents`, and the read cadence nudges fire against stale timestamps.
//
// Each subtest backdates the handle past store.HeartbeatThrottle first:
// RegisterAgent (and CreatePost, which already heartbeats its author) leave
// last_seen_at at now, and the throttle would swallow the write we are testing
// for, making a broken implementation look correct.
func TestActingHandleCommandsHeartbeat(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdheartbeat", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	actor, err := s.RegisterAgent("claude", "actor-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	other, err := s.RegisterAgent("claude", "other-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	// Something to read, something to be mentioned by, and two of the actor's
	// own posts to resolve and delete.
	mustPost(t, s, b.ID, other.Handle, "hey @"+actor.Handle+" the hook fails open", nil)
	question := mustPostTagged(t, s, b.ID, actor.Handle, "does FTS5 need a rebuild?", "question", nil)
	doomed := mustPost(t, s, b.ID, actor.Handle, "delete me", nil)

	cases := []struct {
		name string
		args []string
	}{
		{"read-unread", []string{"read", "--board", "cmdheartbeat", "--unread", "--as", actor.Handle}},
		{"inbox", []string{"inbox", "--as", actor.Handle}},
		{"resolve", []string{"resolve", postArg(question.ID), "--as", actor.Handle}},
		{"delete", []string{"delete", postArg(doomed.ID), "--as", actor.Handle}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := time.Now().Add(-2 * store.HeartbeatThrottle)
			if err := s.DB.Model(&store.Agent{}).
				Where("handle = ?", actor.Handle).
				Update("last_seen_at", old).Error; err != nil {
				t.Fatalf("backdate last_seen_at: %v", err)
			}
			runXfa(t, tc.args...)

			a, err := s.GetAgent(actor.Handle)
			if err != nil {
				t.Fatalf("GetAgent: %v", err)
			}
			if since := time.Since(a.LastSeenAt); since > time.Minute {
				t.Fatalf("xfa %v left last_seen_at %v stale (%s ago); acting-handle commands must heartbeat",
					tc.args, a.LastSeenAt, since)
			}
		})
	}
}
