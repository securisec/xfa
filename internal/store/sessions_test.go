package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// tick separates two writes into distinct milliseconds: created_at compares
// are ms-quantized (see read.go), and session ordering leans on them.
func tick() { time.Sleep(2 * time.Millisecond) }

func postIDs(posts []Post) []uint {
	out := make([]uint, len(posts))
	for i, p := range posts {
		out[i] = p.ID
	}
	return out
}

func hasID(posts []Post, id uint) bool {
	for _, p := range posts {
		if p.ID == id {
			return true
		}
	}
	return false
}

func summaryFor(t *testing.T, list []SessionSummary, sessionID string) SessionSummary {
	t.Helper()
	for _, s := range list {
		if s.SessionID == sessionID {
			return s
		}
	}
	t.Fatalf("session %q missing from %+v", sessionID, list)
	return SessionSummary{}
}

func TestSetSessionNameUpsertsAndRenames(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSessionName("sess-1", "indexing the parser"); err != nil {
		t.Fatalf("SetSessionName: %v", err)
	}
	got, err := s.GetSessionName("sess-1")
	if err != nil || got != "indexing the parser" {
		t.Fatalf("GetSessionName = %q, %v; want %q", got, err, "indexing the parser")
	}
	if err := s.SetSessionName("sess-1", "  renamed  "); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err = s.GetSessionName("sess-1")
	if err != nil || got != "renamed" {
		t.Fatalf("after rename GetSessionName = %q, %v; want %q", got, err, "renamed")
	}
	var n int64
	if err := s.DB.Model(&Session{}).Where("session_id = ?", "sess-1").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rename created %d rows, want 1 (upsert, not insert)", n)
	}
}

func TestSetSessionNameValidates(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSessionName("", "a name"); !errors.Is(err, ErrNoSessionID) {
		t.Errorf("empty sessionID err = %v, want ErrNoSessionID", err)
	}
	if err := s.SetSessionName("sess-1", "   "); err == nil {
		t.Error("whitespace-only name accepted")
	}
	if err := s.SetSessionName("sess-1", strings.Repeat("é", MaxSessionNameLen)); err != nil {
		t.Errorf("%d-rune name rejected: %v", MaxSessionNameLen, err)
	}
	if err := s.SetSessionName("sess-1", strings.Repeat("é", MaxSessionNameLen+1)); err == nil {
		t.Errorf("%d-rune name accepted", MaxSessionNameLen+1)
	}
}

// A name like "\x1b[2J" is non-empty after TrimSpace and well under the rune
// cap, so the earlier checks pass it — but it sanitizes to nothing everywhere
// it is displayed (render.SessionDisplayName strips control sequences before
// a name reaches a terminal), leaving a session that occupies the DB while
// rendering as unnamed. SetSessionName must reject it outright rather than
// storing a name with no visible content.
func TestSetSessionNameRejectsNoVisibleContent(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSessionName("sess-1", "\x1b[2J"); err == nil {
		t.Error("a name with no visible content after control-stripping was accepted")
	}
	// A normal name is unaffected.
	if err := s.SetSessionName("sess-1", "auth refactor"); err != nil {
		t.Errorf("normal name rejected: %v", err)
	}
	got, err := s.GetSessionName("sess-1")
	if err != nil || got != "auth refactor" {
		t.Fatalf("GetSessionName = %q, %v; want %q", got, err, "auth refactor")
	}
}

func TestGetSessionNameMissingRowIsNotAnError(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSessionName("never-named")
	if err != nil || got != "" {
		t.Fatalf("GetSessionName = %q, %v; want \"\", nil", got, err)
	}
}

func TestListSessionsScopingOrderingAndNames(t *testing.T) {
	s := openTemp(t)
	b1, _ := s.EnsureBoard("one", "")
	b2, _ := s.EnsureBoard("two", "")
	a1, _ := s.RegisterAgent("claude", "s1", "")
	tick()
	a2, _ := s.RegisterAgent("claude", "s2", "")

	s.CreatePost(b1.ID, a1.Handle, "s1 on b1", "", nil)
	tick()
	s.CreatePost(b1.ID, a2.Handle, "s2 on b1", "", nil)
	tick()
	s.CreatePost(b2.ID, a1.Handle, "s1 on b2", "", nil)
	if err := s.SetSessionName("s1", "alpha"); err != nil {
		t.Fatal(err)
	}

	scoped, err := s.ListSessions(b1.ID)
	if err != nil {
		t.Fatalf("ListSessions(b1): %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("ListSessions(b1) len = %d, want 2: %+v", len(scoped), scoped)
	}
	if scoped[0].SessionID != "s2" {
		t.Errorf("ListSessions(b1)[0] = %q, want s2 (most recent activity first)", scoped[0].SessionID)
	}
	one := summaryFor(t, scoped, "s1")
	if one.Posts != 1 {
		t.Errorf("s1 posts on b1 = %d, want 1 (board-scoped)", one.Posts)
	}
	if one.Name != "alpha" {
		t.Errorf("s1 name = %q, want alpha", one.Name)
	}
	if one.LeadHandle != a1.Handle {
		t.Errorf("s1 lead = %q, want %q", one.LeadHandle, a1.Handle)
	}
	if summaryFor(t, scoped, "s2").Name != "" {
		t.Errorf("unnamed session carried a name")
	}

	all, err := s.ListSessions(0)
	if err != nil {
		t.Fatalf("ListSessions(0): %v", err)
	}
	if len(all) != 2 || all[0].SessionID != "s1" {
		t.Fatalf("ListSessions(0) = %+v; want s1 first with both sessions", all)
	}
	if got := summaryFor(t, all, "s1").Posts; got != 2 {
		t.Errorf("s1 posts across boards = %d, want 2", got)
	}
}

func TestListSessionsLeadHandleAndFallback(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")

	lead, _ := s.RegisterAgent("claude", "with-lead", "")
	tick()
	sub, _ := s.RegisterAgent("claude", "with-lead", lead.Handle)
	s.CreatePost(b.ID, sub.Handle, "from the subagent", "", nil)

	first, _ := s.RegisterAgent("claude", "no-lead", "outside-parent-1")
	tick()
	second, _ := s.RegisterAgent("claude", "no-lead", "outside-parent-1")
	s.CreatePost(b.ID, second.Handle, "orphan session post", "", nil)

	list, err := s.ListSessions(b.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := summaryFor(t, list, "with-lead").LeadHandle; got != lead.Handle {
		t.Errorf("lead = %q, want the parent_handle='' agent %q", got, lead.Handle)
	}
	fallback := summaryFor(t, list, "no-lead")
	if fallback.LeadHandle != first.Handle {
		t.Errorf("fallback lead = %q, want earliest-created agent %q", fallback.LeadHandle, first.Handle)
	}
	if d := fallback.StartedAt.Sub(first.CreatedAt); d > 5*time.Millisecond || d < -5*time.Millisecond {
		t.Errorf("StartedAt = %v, want earliest agent CreatedAt %v", fallback.StartedAt, first.CreatedAt)
	}
	if fallback.LastActivity.Before(fallback.StartedAt) {
		t.Errorf("LastActivity %v precedes StartedAt %v", fallback.LastActivity, fallback.StartedAt)
	}
}

func TestListSessionsExcludesEmptyIDsAndTombstoneOnlySessions(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")

	ghost, _ := s.RegisterAgent("claude", "", "")
	s.CreatePost(b.ID, ghost.Handle, "no session id", "", nil)

	gone, _ := s.RegisterAgent("claude", "all-deleted", "")
	p, _ := s.CreatePost(b.ID, gone.Handle, "oops", "", nil)
	if err := s.Tombstone(p.ID, gone.Handle); err != nil {
		t.Fatal(err)
	}

	s.RegisterAgent("claude", "never-posted", "") // registered, silent

	list, err := s.ListSessions(b.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListSessions = %+v; want none (empty id, all-tombstoned, no posts)", list)
	}
}

// participated semantics: any live post in the thread by the session matches,
// and the matched thread comes back whole.
func TestBoardPostsBySessionParticipatedSemantics(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")
	a, _ := s.RegisterAgent("claude", "sa", "")
	other, _ := s.RegisterAgent("claude", "sb", "")

	// thread 1: root by sa, reply by sb (later tombstoned)
	t1root, _ := s.CreatePost(b.ID, a.Handle, "t1 root", "", nil)
	t1reply, _ := s.CreatePost(b.ID, other.Handle, "t1 reply", "", &t1root.ID)
	if err := s.Tombstone(t1reply.ID, other.Handle); err != nil {
		t.Fatal(err)
	}
	// thread 2: sb only — must not match
	t2root, _ := s.CreatePost(b.ID, other.Handle, "t2 root", "", nil)
	s.CreatePost(b.ID, other.Handle, "t2 reply", "", &t2root.ID)
	// thread 3: root by sb, reply-only participation by sa (plus a grandchild)
	t3root, _ := s.CreatePost(b.ID, other.Handle, "t3 root", "", nil)
	t3reply, _ := s.CreatePost(b.ID, a.Handle, "t3 reply", "", &t3root.ID)
	t3grand, _ := s.CreatePost(b.ID, other.Handle, "t3 grandchild", "", &t3reply.ID)
	// thread 4: root by sb, sa's only post there is tombstoned — no participation
	t4root, _ := s.CreatePost(b.ID, other.Handle, "t4 root", "", nil)
	t4reply, _ := s.CreatePost(b.ID, a.Handle, "t4 reply", "", &t4root.ID)
	if err := s.Tombstone(t4reply.ID, a.Handle); err != nil {
		t.Fatal(err)
	}

	got, err := s.BoardPostsBySession(b.ID, "sa")
	if err != nil {
		t.Fatalf("BoardPostsBySession: %v", err)
	}
	want := []uint{t1root.ID, t1reply.ID, t3root.ID, t3reply.ID, t3grand.ID}
	gotIDs := postIDs(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("ids = %v, want %v", gotIDs, want)
	}
	for i, id := range want {
		if gotIDs[i] != id {
			t.Fatalf("ids = %v, want %v (id ASC, whole threads)", gotIDs, want)
		}
	}
	if hasID(got, t2root.ID) || hasID(got, t4root.ID) {
		t.Errorf("non-participating thread leaked: %v", gotIDs)
	}
	for _, p := range got {
		if p.ID == t1reply.ID && p.Body != "[deleted]" {
			t.Errorf("tombstone body leaked: %q", p.Body)
		}
	}
}

func TestBoardPostsBySessionUnknownSessionIsEmpty(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")
	a, _ := s.RegisterAgent("claude", "sa", "")
	s.CreatePost(b.ID, a.Handle, "hi", "", nil)
	got, err := s.BoardPostsBySession(b.ID, "nobody-here")
	if err != nil {
		t.Fatalf("BoardPostsBySession: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown session returned %d posts, want 0", len(got))
	}
}

func TestPostsBySessionFiltersFlatListing(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")
	other, _ := s.EnsureBoard("other", "")
	a, _ := s.RegisterAgent("claude", "sa", "")
	sb, _ := s.RegisterAgent("claude", "sb", "")

	mine, _ := s.CreatePost(b.ID, a.Handle, "mine", "til", nil)
	tick()
	theirs, _ := s.CreatePost(b.ID, sb.Handle, "theirs", "til", nil)
	tick()
	tagged, _ := s.CreatePost(b.ID, a.Handle, "mine tagged", "question", nil)
	s.CreatePost(other.ID, a.Handle, "off board", "", nil)

	got, err := s.PostsBySession(b.ID, "sa", "", time.Time{}, 10)
	if err != nil {
		t.Fatalf("PostsBySession: %v", err)
	}
	if len(got) != 2 || hasID(got, theirs.ID) {
		t.Fatalf("ids = %v, want only sa's board posts (%d, %d)", postIDs(got), tagged.ID, mine.ID)
	}
	if got[0].ID != tagged.ID {
		t.Errorf("ids = %v, want newest first", postIDs(got))
	}

	byTag, err := s.PostsBySession(b.ID, "sa", "til", time.Time{}, 10)
	if err != nil || len(byTag) != 1 || byTag[0].ID != mine.ID {
		t.Fatalf("tag-filtered = %v, %v; want [%d]", postIDs(byTag), err, mine.ID)
	}

	limited, err := s.PostsBySession(b.ID, "sa", "", time.Time{}, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limited = %v, %v; want 1 post", postIDs(limited), err)
	}

	cutoff := time.Now()
	tick()
	fresh, _ := s.CreatePost(b.ID, a.Handle, "fresh", "", nil)
	since, err := s.PostsBySession(b.ID, "sa", "", cutoff, 10)
	if err != nil || len(since) != 1 || since[0].ID != fresh.ID {
		t.Fatalf("since-filtered = %v, %v; want [%d]", postIDs(since), err, fresh.ID)
	}
}

func TestPostsBySessionMasksTombstones(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")
	a, _ := s.RegisterAgent("claude", "sa", "")
	p, _ := s.CreatePost(b.ID, a.Handle, "secret", "", nil)
	if err := s.Tombstone(p.ID, a.Handle); err != nil {
		t.Fatal(err)
	}
	got, err := s.PostsBySession(b.ID, "sa", "", time.Time{}, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("PostsBySession = %v, %v; want 1 post", postIDs(got), err)
	}
	if got[0].Body != "[deleted]" {
		t.Errorf("tombstone body leaked: %q", got[0].Body)
	}
}

func TestSessionScopedReadsRequireSessionID(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")
	if _, err := s.BoardPostsBySession(b.ID, ""); !errors.Is(err, ErrNoSessionID) {
		t.Errorf("BoardPostsBySession(\"\") err = %v, want ErrNoSessionID", err)
	}
	if _, err := s.PostsBySession(b.ID, "", "", time.Time{}, 10); !errors.Is(err, ErrNoSessionID) {
		t.Errorf("PostsBySession(\"\") err = %v, want ErrNoSessionID", err)
	}
}

func TestSessionsByHandleIndexesEveryHandleInASession(t *testing.T) {
	s := openTemp(t)
	lead, _ := s.RegisterAgent("claude", "s1", "")
	tick()
	sub, _ := s.RegisterAgent("claude", "s1", lead.Handle)
	other, _ := s.RegisterAgent("claude", "s2", "")
	if err := s.SetSessionName("s1", "alpha"); err != nil {
		t.Fatal(err)
	}

	idx, err := s.SessionsByHandle()
	if err != nil {
		t.Fatalf("SessionsByHandle: %v", err)
	}
	for _, h := range []string{lead.Handle, sub.Handle} {
		sum, ok := idx[h]
		if !ok {
			t.Fatalf("handle %q missing from index %+v", h, idx)
		}
		if sum.SessionID != "s1" {
			t.Errorf("%s session = %q, want s1", h, sum.SessionID)
		}
		if sum.Name != "alpha" {
			t.Errorf("%s name = %q, want alpha", h, sum.Name)
		}
		if sum.LeadHandle != lead.Handle {
			t.Errorf("%s lead = %q, want %q", h, sum.LeadHandle, lead.Handle)
		}
		if d := sum.StartedAt.Sub(lead.CreatedAt); d > 5*time.Millisecond || d < -5*time.Millisecond {
			t.Errorf("%s StartedAt = %v, want earliest agent CreatedAt %v", h, sum.StartedAt, lead.CreatedAt)
		}
	}
	if got := idx[other.Handle]; got.SessionID != "s2" || got.Name != "" {
		t.Errorf("s2 summary = %+v, want session s2 with no name", got)
	}
}

// A labelling index is not a picker listing: it must include a session whose
// only posts are tombstoned (or which has not posted at all), because a post
// card still has to name its author's session, and it deliberately leaves the
// counting fields to ListSessions.
func TestSessionsByHandleSkipsSessionlessAgentsAndCountsNothing(t *testing.T) {
	s := openTemp(t)
	b, _ := s.EnsureBoard("b", "")
	ghost, _ := s.RegisterAgent("claude", "", "")
	s.CreatePost(b.ID, ghost.Handle, "no session id", "", nil)
	silent, _ := s.RegisterAgent("claude", "never-posted", "")

	idx, err := s.SessionsByHandle()
	if err != nil {
		t.Fatalf("SessionsByHandle: %v", err)
	}
	if _, ok := idx[ghost.Handle]; ok {
		t.Errorf("agent with an empty session id appeared in the index: %+v", idx)
	}
	sum, ok := idx[silent.Handle]
	if !ok {
		t.Fatalf("silent session missing from index %+v", idx)
	}
	if sum.Posts != 0 || !sum.LastActivity.IsZero() {
		t.Errorf("summary = %+v, want zero Posts/LastActivity (labels only)", sum)
	}
}

func TestSessionsByHandleEmptyStore(t *testing.T) {
	s := openTemp(t)
	idx, err := s.SessionsByHandle()
	if err != nil {
		t.Fatalf("SessionsByHandle: %v", err)
	}
	if len(idx) != 0 {
		t.Errorf("index = %+v, want empty", idx)
	}
}
