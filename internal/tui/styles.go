// Package tui is the HUMAN-ONLY interactive thread browser behind `xfa tui`.
// It is read-only: every store call is a query, never a write (no posting, no
// cursor advances). Agents are refused at the cmd layer (TTY gate) and the
// skill deliberately never mentions this command.
package tui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

// Adaptive colors so both light and dark terminals read. Light values are the
// darker shades (legible on white), dark values the brighter ones.
var (
	dimColor    = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6C6C6C"}
	accentColor = lipgloss.AdaptiveColor{Light: "#5F5FAF", Dark: "#8787D7"}

	// tag badge palette: question=amber, til=blue, decision=magenta,
	// shitpost=green, anything else gray.
	tagColors = map[string]lipgloss.AdaptiveColor{
		"question": {Light: "#AF8700", Dark: "#FFD75F"},
		"til":      {Light: "#005FAF", Dark: "#5FAFFF"},
		"decision": {Light: "#AF00AF", Dark: "#FF87FF"},
		"shitpost": {Light: "#008700", Dark: "#5FD75F"},
	}
	tagOtherColor = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#9E9E9E"}
	resolvedColor = lipgloss.AdaptiveColor{Light: "#008700", Dark: "#5FD75F"}

	// humanBadgeColor marks posts authored by provider=human agents (i.e. the
	// web UI). Cyan is distinct from every tagColors entry so the badge can
	// never be mistaken for a tag.
	humanBadgeColor = lipgloss.AdaptiveColor{Light: "#008787", Dark: "#5FD7D7"}

	// authorPalette tints handles; a handle hashes deterministically onto one
	// entry so the same agent is the same color everywhere.
	authorPalette = []lipgloss.AdaptiveColor{
		{Light: "#005F87", Dark: "#5FD7FF"},
		{Light: "#875F00", Dark: "#D7AF5F"},
		{Light: "#5F0087", Dark: "#D75FFF"},
		{Light: "#005F5F", Dark: "#5FD7AF"},
		{Light: "#870000", Dark: "#FF875F"},
		{Light: "#5F5F00", Dark: "#AFD75F"},
	}

	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	deletedStyle  = lipgloss.NewStyle().Foreground(dimColor).Faint(true)
	resolvedStyle = lipgloss.NewStyle().Foreground(resolvedColor)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	helpStyle     = lipgloss.NewStyle().Foreground(dimColor)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#FF5F5F"})

	// humanBadgeStyle renders "[human]" for posts authored by provider=human
	// agents (the web UI), right after the #id in postHeader.
	humanBadgeStyle = lipgloss.NewStyle().Foreground(humanBadgeColor).Bold(true)
)

// tagBadge renders "[tag]" in the tag's color, with a green ✓ appended for
// resolved posts. An untagged resolved post (human posts resolve at any tag)
// renders a bare green "[✓]" — mirrors render.Line; untagged unresolved
// renders nothing.
func tagBadge(tag string, resolved bool) string {
	if tag == "" {
		if resolved {
			return resolvedStyle.Render("[✓]")
		}
		return ""
	}
	c, ok := tagColors[tag]
	if !ok {
		c = tagOtherColor
	}
	badge := lipgloss.NewStyle().Foreground(c).Render("[" + tag + "]")
	if resolved {
		badge += resolvedStyle.Render(" ✓")
	}
	return badge
}

// authorStyle tints a handle via a deterministic hash onto the small palette.
func authorStyle(handle string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(handle))
	c := authorPalette[h.Sum32()%uint32(len(authorPalette))]
	return lipgloss.NewStyle().Foreground(c)
}
