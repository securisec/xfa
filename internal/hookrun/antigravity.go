package hookrun

// Antigravity (Google's Antigravity IDE) hook adapter. Antigravity's hook I/O
// is not Claude-shaped (antigravity.google/docs/ide/hooks/, 2026-08-27):
// stdin fields are camelCase (conversationId / workspacePaths, not session_id
// / cwd), there is no SessionStart or UserPromptSubmit event, and outputs are
// injectSteps / decision:"continue" rather than hookSpecificOutput /
// decision:"block". These entry points map the antigravity payload onto the
// existing session-start / user-prompt / stop text builders and re-wrap the
// result in antigravity's shapes. Fail open everywhere, like every hook path.

import (
	"encoding/json"

	"github.com/securisec/xfa/internal/store"
)

// AntigravityInput is the antigravity hook stdin payload (the fields xfa uses).
type AntigravityInput struct {
	ConversationID string   `json:"conversationId"`
	WorkspacePaths []string `json:"workspacePaths"`
}

// agStartSuffix namespaces the "this conversation already got its
// session-start injection" mark. Like every mark key it MUST carry a suffix —
// a bare conversation-id row would satisfy the Stop hook's fire-once guard
// and silently swallow the end-of-session nudge.
const agStartSuffix = ":ag-start"

// toInput maps the antigravity payload onto the Claude-shaped Input. ok=false
// (fail open) when the payload lacks a conversation id or workspace path —
// which is also what decode garbage produces.
func (a AntigravityInput) toInput() (Input, bool) {
	if a.ConversationID == "" || len(a.WorkspacePaths) == 0 {
		return Input{}, false
	}
	return Input{SessionID: a.ConversationID, Cwd: a.WorkspacePaths[0]}, true
}

// AntigravityInvoke handles PreInvocation: the conversation's first
// invocation gets the session-start preamble+digest, every later one the
// user-prompt digest/nudge. First-ness is guarded by the mark store, not the
// payload's invocationNum (unclear whether that resets per execution).
func AntigravityInvoke(s *store.Store, a AntigravityInput) (string, error) {
	in, ok := a.toInput()
	if !ok {
		return "", nil
	}
	key := a.ConversationID + agStartSuffix
	_, seen, err := s.GetMark(key)
	if err != nil {
		seen = true // fail toward the quieter user-prompt path
	}
	var text string
	if seen {
		text = userPromptText(s, in)
	} else {
		_ = s.SetMark(key, "1") // best-effort; a failed mark just repeats the preamble
		text, _ = sessionStartText(s, in)
	}
	if text == "" {
		return "", nil
	}
	out, err := json.Marshal(map[string]any{
		"injectSteps": []any{map[string]string{"ephemeralMessage": text}},
	})
	if err != nil {
		return "", nil
	}
	return string(out), nil
}

// AntigravityStop handles Stop: the same fire-once nudge as the Claude Stop
// hook, re-wrapped as {"decision":"continue","reason":...} — antigravity's
// "continue" is Claude's "block": it prevents termination and injects reason.
func AntigravityStop(s *store.Store, a AntigravityInput) (string, error) {
	in, ok := a.toInput()
	if !ok {
		return "", nil
	}
	reason, err := stopReason(s, in)
	if err != nil || reason == "" {
		return "", nil
	}
	out, err := json.Marshal(map[string]string{"decision": "continue", "reason": reason})
	if err != nil {
		return "", nil
	}
	return string(out), nil
}
