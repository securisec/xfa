package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/store"
)

// NOTE (harness footgun, see read_test.go): pflag values persist between
// Execute() calls on the shared rootCmd, so --json is set explicitly on every
// run, including the text ones.

// A #id in a body is a real cross-link: `xfa thread` decorates the
// referencing post with an outbound line and the referenced post with a
// backlink, each naming the far end's board — the point being that an agent
// reading one thread learns the conversation continues elsewhere without
// running a second command. The link rows are written by CreatePost, so this
// drives the whole path store-side through the real cobra command.
func TestThreadShowsCrossLinks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b1, err := s.EnsureBoard("cmdlinks", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	// A second board, so the rendered link lines have a board slug to prove
	// they carry (a same-board link would render the slug either way).
	b2, err := s.EnsureBoard("cmdlinks-other", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "links-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	target := mustPost(t, s, b1.ID, a.Handle, "the original finding", nil)
	source := mustPost(t, s, b2.ID, a.Handle,
		fmt.Sprintf("hit the same thing, see #%d", target.ID), nil)

	// The referenced post shows the backlink, pointing back at the other board.
	backlink := fmt.Sprintf("← linked from #%d (b/cmdlinks-other)", source.ID)
	out := runXfa(t, "thread", fmt.Sprint(target.ID), "--json=false")
	if !strings.Contains(out, backlink) {
		t.Errorf("thread %d must show %q:\n%s", target.ID, backlink, out)
	}

	// The referencing post shows the outbound link.
	outbound := fmt.Sprintf("→ #%d (b/cmdlinks)", target.ID)
	out = runXfa(t, "thread", fmt.Sprint(source.ID), "--json=false")
	if !strings.Contains(out, outbound) {
		t.Errorf("thread %d must show %q:\n%s", source.ID, outbound, out)
	}

	// --json carries the same links as structured rows.
	out = runXfa(t, "thread", fmt.Sprint(source.ID), "--json=true")
	for _, want := range []string{`"links_out"`, `"post_id"`, `"board_slug":"cmdlinks"`} {
		if !strings.Contains(out, want) {
			t.Errorf("thread --json must contain %s:\n%s", want, out)
		}
	}
	// A post with no links carries no empty link keys (omitempty).
	plain := mustPost(t, s, b1.ID, a.Handle, "no references here", nil)
	out = runXfa(t, "thread", fmt.Sprint(plain.ID), "--json=true")
	if strings.Contains(out, "links_out") || strings.Contains(out, "links_in") {
		t.Errorf("link-free post must omit link keys:\n%s", out)
	}
}

// `read` decorates its flat listing the same way, so the board-level catch-up
// surfaces cross-links too — including the backlink on a post whose reply
// landed on another board.
func TestReadShowsCrossLinks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdreadlinks", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "readlinks-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	target := mustPost(t, s, b.ID, a.Handle, "the original finding", nil)
	source := mustPost(t, s, b.ID, a.Handle,
		fmt.Sprintf("same here, see #%d", target.ID), nil)

	args := []string{"read", "--board", "cmdreadlinks", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false"}
	out := runXfa(t, append(append([]string{}, args...), "--json=false")...)
	for _, want := range []string{
		fmt.Sprintf("→ #%d (b/cmdreadlinks)", target.ID),
		fmt.Sprintf("← linked from #%d (b/cmdreadlinks)", source.ID),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("read must show %q:\n%s", want, out)
		}
	}

	out = runXfa(t, append(append([]string{}, args...), "--json=true")...)
	if !strings.Contains(out, `"links_out"`) || !strings.Contains(out, `"links_in"`) {
		t.Errorf("read --json must carry both link directions:\n%s", out)
	}
}

// An empty read still encodes as [], never null — the normalization the JSON
// path had before postOut existed.
func TestReadJSONEmptyIsArray(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := s.EnsureBoard("cmdemptyread", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	out := runXfa(t, "read", "--board", "cmdemptyread", "--session", "", "--tag", "",
		"--since", "", "--limit", "20", "--unread=false", "--json=true")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty read --json = %q, want []", out)
	}
}

// `xfa thread` accepts any id in a thread: a reply id resolves to its root and
// the whole thread renders, with a note naming both ids so the agent learns
// the root. --json stays a bare array (the note would corrupt it).
func TestThreadFromReplyIDShowsRootWithNote(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	t.Setenv("XFA_DB", dbPath)
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	b, err := s.EnsureBoard("cmdthreadroot", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	a, err := s.RegisterAgent("claude", "threadroot-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	root := mustPost(t, s, b.ID, a.Handle, "the root post", nil)
	reply := mustPost(t, s, b.ID, a.Handle, "the reply", &root.ID)

	out := runXfa(t, "thread", fmt.Sprint(reply.ID), "--json=false")
	note := fmt.Sprintf("showing thread #%d (you asked for #%d)\n", root.ID, reply.ID)
	if !strings.HasPrefix(out, note) {
		t.Errorf("thread from reply id must start with %q:\n%s", note, out)
	}
	ri, rj := strings.Index(out, "the root post"), strings.Index(out, "the reply")
	if ri < 0 || rj < 0 || ri > rj {
		t.Errorf("root line must precede reply line:\n%s", out)
	}

	// Asking for the root itself prints no note.
	out = runXfa(t, "thread", fmt.Sprint(root.ID), "--json=false")
	if strings.Contains(out, "showing thread") {
		t.Errorf("root id must not print the note:\n%s", out)
	}

	out = runXfa(t, "thread", fmt.Sprint(reply.ID), "--json=true")
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(out), &arr); err != nil || len(arr) != 2 {
		t.Errorf("thread --json from reply id must decode as an array of 2 (err=%v):\n%s", err, out)
	}
	if strings.Contains(out, "showing thread") {
		t.Errorf("--json must not carry the note:\n%s", out)
	}
}
