package store

import (
	"errors"
	"testing"
)

func TestMentionsParsedOnCreate(t *testing.T) {
	s, b, a := seed(t)
	p, err := s.CreatePost(b.ID, a.Handle, "ping @wry-vole-3 and @wry-vole-3 again, also @keen-ibis-12", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var ms []Mention
	s.DB.Where("post_id = ?", p.ID).Order("handle").Find(&ms)
	if len(ms) != 2 || ms[0].Handle != "keen-ibis-12" || ms[1].Handle != "wry-vole-3" {
		t.Fatalf("mentions = %+v, want deduped keen-ibis-12 + wry-vole-3", ms)
	}
}

func TestInboxMentionsAndReplies(t *testing.T) {
	s, b, me := seed(t)
	other, _ := s.RegisterAgent("claude", "other-sess", "")
	mine, _ := s.CreatePost(b.ID, me.Handle, "my question", "question", nil)
	s.CreatePost(b.ID, other.Handle, "an answer", "", &mine.ID)           // reply to me
	s.CreatePost(b.ID, other.Handle, "hey @"+me.Handle+" look", "", nil)  // mentions me
	s.CreatePost(b.ID, other.Handle, "unrelated", "", nil)                // neither
	s.CreatePost(b.ID, me.Handle, "self reply @"+me.Handle, "", &mine.ID) // my own — excluded

	in, err := s.Inbox(me.Handle, 10)
	if err != nil || len(in) != 2 {
		t.Fatalf("inbox: %v len=%d want 2", err, len(in))
	}
	// Newest-first: the mention was created after the reply.
	if in[0].Body != "hey @"+me.Handle+" look" || in[1].Body != "an answer" {
		t.Fatalf("inbox = [%q, %q], want mention then reply, newest-first",
			in[0].Body, in[1].Body)
	}
}

func TestInboxUnknownHandle(t *testing.T) {
	s, _, _ := seed(t)
	if _, err := s.Inbox("ghost-vole-9", 10); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("Inbox(unknown) err = %v, want ErrNoAgent (a typo'd handle must not look like an empty inbox)", err)
	}
}
