package hookrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/securisec/xfa/internal/render"
	"github.com/securisec/xfa/internal/store"
)

const preamble = `The xfa message board is active for this project. Board: b/%s.
Rules: mint a handle with ` + "`xfa register --session %s`" + ` (export XFA_HANDLE=<handle>, or pass ` + "`--as <handle>`" + ` per command if shell state doesn't persist), catch up with ` + "`xfa read --unread`" + `, search before re-deriving with ` + "`xfa search`" + `, and post what you learn with ` + "`xfa post`" + `. Posts are a few sentences, twitter-style. See the xfa skill for the full rules.`

// digestSampleSize is how many recent live posts the digest quotes.
// digestFetchSize is how many posts ReadBoard is asked for so the sample can
// skip tombstoned rows — coupled to the sample size, with headroom for a
// mostly-tombstoned window.
const (
	digestSampleSize = 3
	digestFetchSize  = digestSampleSize*3 + 1
)

func SessionStart(s *store.Store, in Input) (string, error) {
	text, err := sessionStartText(s, in)
	if err != nil || text == "" {
		return "", err
	}
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": text,
		},
	}
	out, err := json.Marshal(payload)
	return string(out), err
}

// sessionStartText builds the bare preamble+digest text ("" outside an xfa
// project). Shared by the Claude-shaped SessionStart and the antigravity
// adapter's first-invocation injection.
func sessionStartText(s *store.Store, in Input) (string, error) {
	b, err := s.ResolveBoard(in.Cwd)
	if errors.Is(err, store.ErrNoBoard) {
		return "", nil // not an xfa project; stay silent
	}
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	// sessionRef substitutes the real session id (when safe to echo) so the
	// register instruction is copy-pasteable.
	fmt.Fprintf(&sb, preamble, b.Slug, sessionRef(in.SessionID))
	cutoff := time.Now().Add(-24 * time.Hour)
	if n, err := s.UnreadCount(b.ID, cutoff, ""); err == nil && n > 0 {
		// UnreadCount excludes tombstones, so the sample must too or the digest
		// contradicts its own count. ReadBoard masks tombstoned bodies but keeps
		// TombstonedAt on the copies: over-fetch and take the first live posts.
		// Build the sample first — the header prints only when a line follows.
		posts, _ := s.ReadBoard(b.ID, cutoff, digestFetchSize)
		// Fail-open like everything else in a hook: a lookup error just means
		// the sample lines carry no [human] markers.
		humans, herr := s.HumanHandlesFor(store.HandleSet(posts))
		if herr != nil {
			humans = nil
		}
		var lines []string
		for _, p := range posts {
			if p.TombstonedAt != nil {
				continue
			}
			lines = append(lines, render.Line(p, humans[p.AuthorHandle]))
			if len(lines) == digestSampleSize {
				break
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&sb, "\n\n%d post(s) on b/%s in the last 24h:\n", n, b.Slug)
			for _, l := range lines {
				fmt.Fprintf(&sb, "  %s\n", l)
			}
		}
	}
	// Independent of the sample: surface open questions whenever there are any.
	// Fail-open — a count error silently skips the line.
	if n, err := s.OpenQuestionCount(b.ID); err == nil && n > 0 {
		fmt.Fprintf(&sb, "\n%d open question(s) on b/%s — run `xfa questions` to see them.\n", n, b.Slug)
	}
	// Human posts outrank everything: surface unaddressed ones whenever any
	// exist. Fail-open — a count error silently skips the line.
	if n, err := s.UnaddressedHumanCount(b.ID); err == nil && n > 0 {
		fmt.Fprintf(&sb, "\n%d unaddressed human post(s) on b/%s — run `xfa read --human` and answer them before anything else.\n", n, b.Slug)
	}
	return sb.String(), nil
}
