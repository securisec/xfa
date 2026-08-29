package hookrun

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/securisec/xfa/internal/store"
)

// nudge is a fmt template; %s is the session id (or the placeholder), so the
// register command is copy-pasteable — registering without --session leaves
// the session invisible to the posted-check JOIN.
const nudge = "Before you finish: you haven't posted to the xfa board this session. " +
	"If you learned anything another agent could use — a gotcha, a decision, a dead end — " +
	"post it now (`xfa register --session %s` then `xfa post \"...\" --as <handle>`). If there is truly nothing to share, finish up; this reminder won't repeat."

func Stop(s *store.Store, in Input) (string, error) {
	reason, err := stopReason(s, in)
	if err != nil || reason == "" {
		return "", err
	}
	out, err := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return string(out), err
}

// stopReason runs the fire-once nudge logic and returns the bare reason text
// ("" when the session posted, was already reminded, or is outside a project).
// Shared by the Claude-shaped Stop and the antigravity adapter, so both shapes
// carry the same words and the same fire-once guard.
func stopReason(s *store.Store, in Input) (string, error) {
	if in.SessionID == "" {
		return "", nil
	}
	b, err := s.ResolveBoard(in.Cwd)
	if errors.Is(err, store.ErrNoBoard) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	var posted int64
	err = s.DB.Model(&store.Post{}).
		Joins("JOIN agents ON agents.handle = posts.author_handle").
		Where("agents.session_id = ?", in.SessionID).
		Count(&posted).Error
	if err != nil || posted > 0 {
		return "", err
	}
	// Fire once per session: the unique index on reminders.session_id is the guard.
	if err := s.DB.Create(&store.Reminder{SessionID: in.SessionID}).Error; err != nil {
		return "", nil // already reminded
	}
	reason := fmt.Sprintf(nudge, sessionRef(in.SessionID))
	// Appended only when the nudge already fires — fire/silence logic unchanged.
	// Fail-open: a count error silently skips the sentence.
	if n, err := s.OpenQuestionCount(b.ID); err == nil && n > 0 {
		reason += fmt.Sprintf(" There are %d open question(s) on this board — consider answering one (`xfa questions`).", n)
	}
	return reason, nil
}
