package store

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/securisec/xfa/internal/handle"
)

var ErrNoAgent = errors.New("no agent with that handle (run `xfa register`)")

// newSeed draws a seed from crypto/rand so that same-microsecond callers
// (parallel subagents registering at once) never share a mint sequence.
// Falls back to time^pid only if crypto/rand fails.
func newSeed() int64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err == nil {
		return int64(binary.LittleEndian.Uint64(b[:]))
	}
	return time.Now().UnixNano() ^ int64(os.Getpid())
}

func (s *Store) RegisterAgent(provider, sessionID, parentHandle string) (*Agent, error) {
	return s.RegisterAgentWithRepo(provider, sessionID, parentHandle, "")
}

// RegisterAgentWithRepo is RegisterAgent plus a repo display hint, shown
// after the handle wherever authors are rendered.
func (s *Store) RegisterAgentWithRepo(provider, sessionID, parentHandle, repo string) (*Agent, error) {
	rng := rand.New(rand.NewSource(newSeed()))
	var lastErr error
	for i := 0; i < 10; i++ {
		a := Agent{
			Handle:       handle.Mint(rng),
			Provider:     provider,
			SessionID:    sessionID,
			ParentHandle: parentHandle,
			Repo:         repo,
			LastSeenAt:   time.Now(),
		}
		err := withBusyRetry(func() error { return s.DB.Create(&a).Error })
		if err == nil {
			return &a, nil
		}
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			// Not a handle collision — retrying cannot help; surface the real cause.
			return nil, fmt.Errorf("register agent: %w", err)
		}
		lastErr = err
	}
	return nil, fmt.Errorf("could not mint a unique handle after 10 tries: %w", lastErr)
}

func (s *Store) GetAgent(handleName string) (*Agent, error) {
	var a Agent
	if err := s.DB.Where("handle = ?", handleName).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrNoAgent, handleName)
		}
		return nil, err
	}
	return &a, nil
}

// TouchAgent advances the handle's last_seen_at heartbeat, throttled to at
// most one write per HeartbeatThrottle. This is the ONLY heartbeat mechanism
// — every command path (posting and reading alike) goes through it. The
// read-then-conditional-write races benignly: losing the race just means the
// timestamp is at most a throttle-window stale, which the probabilistic
// display absorbs.
func (s *Store) TouchAgent(handleName string) error {
	a, err := s.GetAgent(handleName)
	if err != nil {
		return err
	}
	if time.Since(a.LastSeenAt) < HeartbeatThrottle {
		return nil
	}
	return withBusyRetry(func() error {
		return s.DB.Model(&Agent{}).Where("handle = ?", handleName).
			Update("last_seen_at", time.Now()).Error
	})
}

// LastSeenFor batch-fetches last_seen_at for a page of handles in one query.
// Unregistered handles are simply absent from the map — mention-before-register
// is legal, so absence is data, not an error.
func (s *Store) LastSeenFor(handles []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(handles))
	if len(handles) == 0 {
		return out, nil
	}
	var agents []Agent
	if err := s.DB.Where("handle IN ?", handles).Find(&agents).Error; err != nil {
		return nil, err
	}
	for _, a := range agents {
		out[a.Handle] = a.LastSeenAt
	}
	return out, nil
}

// SessionLeadAgent returns the session's non-parented handle — the acting
// identity for session-scoped hooks. Agents do re-register, so ties go to the
// most recently seen.
func (s *Store) SessionLeadAgent(sessionID string) (*Agent, error) {
	var a Agent
	err := s.DB.Where("session_id = ? AND parent_handle = ''", sessionID).
		Order("last_seen_at DESC").First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: session %s", ErrNoAgent, sessionID)
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// SessionHandles returns every handle registered under the session (lead and
// subagents) — the exclusion list for "did someone ELSE post?" checks.
func (s *Store) SessionHandles(sessionID string) ([]string, error) {
	var hs []string
	err := s.DB.Model(&Agent{}).Where("session_id = ?", sessionID).
		Pluck("handle", &hs).Error
	return hs, err
}

// SetMark upserts a key->value row in the reminders table (keyed on its unique
// session_id column). Reused for the user-prompt high-water-mark with keys like
// "<session>:prompt-hwm" — the same namespace trick as the SubagentStop
// "<session>:subagent" fire-once rows.
func (s *Store) SetMark(key, value string) error {
	return withBusyRetry(func() error {
		return s.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "session_id"}},
			DoUpdates: clause.Assignments(map[string]any{"value": value}),
		}).Create(&Reminder{SessionID: key, Value: value}).Error
	})
}

// GetMark reads a SetMark row; ok=false when the key has never been set. A
// plain fire-once reminder row reads back as an empty value with ok=true.
func (s *Store) GetMark(key string) (string, bool, error) {
	var r Reminder
	err := s.DB.Where("session_id = ?", key).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return r.Value, true, nil
}
