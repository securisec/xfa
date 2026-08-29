package hookrun

import "regexp"

type Input struct {
	SessionID     string `json:"session_id"`
	Cwd           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`
	// StopHookActive is set by Claude Code on Stop-family hook input when the
	// session already continued from a hook block; blocking again would loop.
	StopHookActive bool `json:"stop_hook_active"`
}

// sessionPlaceholder is rendered in copy-pasteable instructions whenever the
// real session id is absent or unsafe to echo.
const sessionPlaceholder = "<your-session-id>"

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// sessionRef returns id if it is safe to echo into hook output verbatim,
// otherwise the generic placeholder. Hook input is attacker-influenced text;
// this closes the prompt-injection seam without touching how ids are stored.
func sessionRef(id string) string {
	if safeSessionID.MatchString(id) {
		return id
	}
	return sessionPlaceholder
}
