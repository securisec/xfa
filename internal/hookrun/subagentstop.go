package hookrun

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/securisec/xfa/internal/store"
)

// subagentNudge fires on every first subagent finish, open questions or not:
// a finished subagent is the moment findings are freshest. Unlike the Stop
// nudge it never echoes the session id — subagents mint their own handles, so
// there is nothing session-specific to render.
const subagentNudge = "Before you finish: if you found anything non-obvious, post it now: " +
	"`xfa post \"<finding>\" --tag finding --as <handle>` — one post per discovery. " +
	"If you have no handle or nothing to add, finish now."

// subagentQuestions is appended only when open questions exist; %d is the
// count, %s the board slug.
const subagentQuestions = " Also: %d open question(s) on b/%s. " +
	"Run `xfa questions` and `xfa inbox --as <handle>`; if you can answer one, reply with `xfa reply <post-id> \"...\" --as <handle>`, " +
	"and resolve your own answered questions (`xfa resolve <post-id> --as <handle>`)."

// subagentReminderSuffix distinguishes this hook's fire-once key from the Stop
// nudge's in the shared reminders table. Subagent identity is not reliably
// present in the hook payload, so one nudge per session is the safe floor.
const subagentReminderSuffix = ":subagent"

// SubagentStop nudges a finishing Task subagent's session to post findings
// (and answer open board questions, when there are any).
// The session-scoped SessionStart digest and Stop nudge never reach subagents;
// this is their only exposure to the board.
func SubagentStop(s *store.Store, in Input) (string, error) {
	// A session that already continued from a block must never be re-blocked.
	if in.SessionID == "" || in.StopHookActive {
		return "", nil
	}
	b, err := s.ResolveBoard(in.Cwd)
	if errors.Is(err, store.ErrNoBoard) {
		return "", nil // not an xfa project; stay silent
	} else if err != nil {
		return "", err
	}
	// Fire once per session: the unique index on reminders.session_id is the
	// guard; the suffix keeps the Stop nudge's key untouched.
	if err := s.DB.Create(&store.Reminder{SessionID: in.SessionID + subagentReminderSuffix}).Error; err != nil {
		return "", nil // already reminded
	}
	reason := subagentNudge
	// Fail-open: a count error silently skips the sentence.
	if n, err := s.OpenQuestionCount(b.ID); err == nil && n > 0 {
		reason += fmt.Sprintf(subagentQuestions, n, b.Slug)
	}
	out, err := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return string(out), err
}
