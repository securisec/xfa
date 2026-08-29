package hookrun

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/securisec/xfa/internal/store"
)

// promptHWMSuffix namespaces the per-session high-water-mark row in the
// reminders table, alongside the SubagentStop "<session>:subagent" key. It must
// never be dropped: a bare session-id row would satisfy the Stop hook's
// fire-once guard and silence the end-of-session nudge. Every key this file
// writes obeys that rule — see humanNudgeSuffix.
const promptHWMSuffix = ":prompt-hwm"

// humanNudgeSuffix namespaces the per-session human-nudge throttle row under
// the same rule as promptHWMSuffix above: never a bare session id.
const humanNudgeSuffix = ":human-nudge"

// humanNudgeEvery is the sliding window between human-post nudges for one
// session — often enough to stay urgent, rare enough not to nag every prompt.
const humanNudgeEvery = 10 * time.Minute

// UserPrompt emits the session's prompt-time context: the unread digest (gated
// on a post-id high-water-mark) plus a throttled nudge about unaddressed human
// posts. The two lines are independent — a human post must reach the session
// even when the unread digest has nothing to say. Scope-honest by design: this
// reaches interactive sessions only — subagents get no user prompts.
// Fail open everywhere: any error path returns "", nil.
func UserPrompt(s *store.Store, in Input) (string, error) {
	text := userPromptText(s, in)
	if text == "" {
		return "", nil
	}
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": text,
		},
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", nil
	}
	return string(out), nil
}

// userPromptText builds the bare digest/nudge lines ("" when there is nothing
// to say). Shared by the Claude-shaped UserPrompt and the antigravity
// adapter's later-invocation injections.
func userPromptText(s *store.Store, in Input) string {
	if in.SessionID == "" {
		return ""
	}
	// Unregistered cwd (store.ErrNoBoard) and every other lookup failure are
	// the same thing here: nothing to say.
	b, err := s.ResolveBoard(in.Cwd)
	if err != nil {
		return ""
	}
	var lines []string
	if l := unreadLine(s, b, in); l != "" {
		lines = append(lines, l)
	}
	if l := humanLine(s, b, in); l != "" {
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// unreadLine is the one-line unread digest for the session's lead handle, gated
// on a post-id high-water-mark so consecutive prompts stay silent until someone
// OUTSIDE the session posts something new. Returns "" on every silent path.
func unreadLine(s *store.Store, b *store.Board, in Input) string {
	lead, err := s.SessionLeadAgent(in.SessionID)
	if err != nil {
		return "" // no registered lead: SessionStart already primed this session
	}
	mine, err := s.SessionHandles(in.SessionID)
	if err != nil {
		return ""
	}
	key := in.SessionID + promptHWMSuffix
	var hwm uint
	if v, ok, gerr := s.GetMark(key); gerr == nil && ok {
		if n, perr := strconv.ParseUint(v, 10, 64); perr == nil {
			hwm = uint(n)
		}
	}
	fresh, maxID, err := s.NewForeignPosts(b.ID, hwm, mine)
	if err != nil || fresh == 0 {
		return ""
	}
	unread, err := s.UnreadCountFor(b.ID, lead.Handle)
	if err != nil || unread == 0 {
		return "" // caught up: don't nag, don't advance the mark
	}
	_ = s.SetMark(key, strconv.FormatUint(uint64(maxID), 10)) // best-effort
	return fmt.Sprintf("%d unread on b/%s — xfa read --unread --as %s",
		unread, b.Slug, lead.Handle)
}

// humanLine surfaces unaddressed human posts at most once per humanNudgeEvery
// per session. Sliding throttle via SetMark upsert — deliberately NOT the
// fire-once raw-Create guard, which never re-fires. With zero unaddressed posts
// the mark is left untouched, so the first real human post nudges immediately.
func humanLine(s *store.Store, b *store.Board, in Input) string {
	n, err := s.UnaddressedHumanCount(b.ID)
	if err != nil || n == 0 {
		return ""
	}
	key := in.SessionID + humanNudgeSuffix
	if v, ok, gerr := s.GetMark(key); gerr == nil && ok {
		if last, perr := strconv.ParseInt(v, 10, 64); perr == nil &&
			time.Since(time.Unix(last, 0)) < humanNudgeEvery {
			return ""
		}
	}
	_ = s.SetMark(key, strconv.FormatInt(time.Now().Unix(), 10)) // best-effort
	return fmt.Sprintf("%d unaddressed human post(s) on b/%s — xfa read --human", n, b.Slug)
}
