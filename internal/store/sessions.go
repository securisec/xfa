package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxSessionNameLen caps a session name at 60 runes: names are display labels
// for a dropdown/picker row, not descriptions.
const MaxSessionNameLen = 60

// ErrNoSessionID marks a session-scoped call made without a session id. An
// empty filter must never silently mean "match everything" — the unfiltered
// paths are separate functions.
var ErrNoSessionID = errors.New("sessionID required")

// SetSessionName names (or renames) a session: a single busy-retried upsert on
// the sessions table's unique session_id — same shape as SetMark, no gorm
// transaction (single-conn pool, see store.go). The name is trimmed before
// validation and storage, so a padded rename can never smuggle in a blank.
func (s *Store) SetSessionName(sessionID, name string) error {
	if sessionID == "" {
		return ErrNoSessionID
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("session name is empty")
	}
	if n := utf8.RuneCountInString(name); n > MaxSessionNameLen {
		return fmt.Errorf("session name is %d chars; max is %d", n, MaxSessionNameLen)
	}
	if !nameHasVisibleContent(name) {
		return errors.New("session name has no visible content")
	}
	return withBusyRetry(func() error {
		return s.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "session_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":       name,
				"updated_at": time.Now(),
			}),
		}).Create(&Session{SessionID: sessionID, Name: name}).Error
	})
}

// GetSessionName returns the session's stored name, or "" when it has never
// been named. A missing row is not an error: naming is optional everywhere.
func (s *Store) GetSessionName(sessionID string) (string, error) {
	var row Session
	err := s.DB.Where("session_id = ?", sessionID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Name, nil
}

// nameHasVisibleContent reports whether name contains anything a human
// terminal would actually show once ANSI control sequences are stripped —
// the same swallow render.StripControls performs before a name reaches a
// display (see render.SessionDisplayName). A name like "\x1b[2J" is
// non-empty and short enough to pass the earlier checks, but sanitizes away
// to nothing everywhere it is shown, so SetSessionName rejects it here.
//
// store cannot import internal/render (render imports store, and Go forbids
// the cycle either direction), so this is a minimal, store-local duplicate
// of just the "does anything survive" check — not a full sanitizer.
func nameHasVisibleContent(name string) bool {
	rs := []rune(name)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == 0x1b { // ESC: swallow the whole sequence, same shape as StripControls
			if i+1 >= len(rs) {
				break
			}
			i++
			switch rs[i] {
			case '[': // CSI: params/intermediates until a final byte @..~
				for i+1 < len(rs) {
					i++
					if rs[i] >= 0x40 && rs[i] <= 0x7e {
						break
					}
				}
			case ']', 'P', 'X', '^', '_': // OSC/DCS/SOS/PM/APC: until BEL or ST
				for i+1 < len(rs) {
					i++
					if rs[i] == 0x07 {
						break
					}
					if rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '\\' {
						i++
						break
					}
				}
			case '(', ')', '*', '+': // charset selector: one more byte
				if i+1 < len(rs) {
					i++
				}
			}
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue // C0 (incl. \n \t), DEL, C1: not visible content
		}
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// SessionSummary is one session rolled up for a picker row. Name is "" for an
// unnamed session — display layers fall back to LeadHandle · StartedAt · a
// short SessionID. Posts and LastActivity exclude tombstoned posts (a deleted
// post is not activity), while StartedAt comes from the session's agents.
type SessionSummary struct {
	SessionID    string
	Name         string
	LeadHandle   string
	StartedAt    time.Time
	Posts        int64
	LastActivity time.Time
}

// ListSessions summarizes every session with at least one live post in scope —
// one board, or all boards when boardID is 0 — most recently active first.
// Agents with an empty session_id are not a session and never appear.
//
// The rollup happens in Go, not SQL, for the same reason BoardPosts orders by
// id: created_at is offset-bearing TEXT, so SQL MIN/MAX over it compares
// lexicographically and inverts chronology across differing UTC offsets. Go's
// parsed time.Time compares correctly, and boards are small by design.
func (s *Store) ListSessions(boardID uint) ([]SessionSummary, error) {
	var agents []Agent
	if err := s.DB.Where("session_id <> ''").Find(&agents).Error; err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return []SessionSummary{}, nil
	}
	sessionOf := make(map[string]string, len(agents)) // handle -> session id
	for _, a := range agents {
		sessionOf[a.Handle] = a.SessionID
	}

	q := s.DB.Model(&Post{}).Where("tombstoned_at IS NULL")
	if boardID != 0 {
		q = q.Where("board_id = ?", boardID)
	}
	var posts []struct {
		AuthorHandle string
		CreatedAt    time.Time
	}
	if err := q.Select("author_handle, created_at").Scan(&posts).Error; err != nil {
		return nil, err
	}

	index := make(map[string]int) // session id -> position in out
	out := []SessionSummary{}
	for _, p := range posts {
		sid, ok := sessionOf[p.AuthorHandle]
		if !ok { // unregistered author, or an agent with no session id
			continue
		}
		i, seen := index[sid]
		if !seen {
			i = len(out)
			index[sid] = i
			out = append(out, SessionSummary{SessionID: sid})
		}
		sum := &out[i]
		sum.Posts++
		if p.CreatedAt.After(sum.LastActivity) {
			sum.LastActivity = p.CreatedAt
		}
	}
	if len(out) == 0 {
		return out, nil
	}

	summarizeAgents(agents, index, out)
	if err := s.fillSessionNames(out); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastActivity.Equal(out[j].LastActivity) {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].LastActivity.After(out[j].LastActivity)
	})
	return out, nil
}

// betterLead reports whether a is a better "lead of the session" than b: an
// unparented agent (parent_handle empty) always beats a parented one, so a
// subagent is only displayed as lead when the session has no root agent at
// all; then earliest-created wins, then handle order. A total order, so the
// picked lead never depends on the order rows came back in.
//
// Deliberately not SessionLeadAgent: that picks the most recently *seen* root
// agent for hook identity, whereas a picker row wants the stable, earliest
// identity that pairs with StartedAt.
func betterLead(a, b Agent) bool {
	if (a.ParentHandle == "") != (b.ParentHandle == "") {
		return a.ParentHandle == ""
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.Handle < b.Handle
}

// summarizeAgents fills in each summary's LeadHandle (via betterLead) and
// earliest-CreatedAt StartedAt from agents, using index to map a session id
// to its position in summaries. Shared by ListSessions and SessionsByHandle,
// which independently computed the identical rollup before this extraction —
// a duplication risk, since a divergence there would let the same session
// carry a different fallback label depending which surface asked.
func summarizeAgents(agents []Agent, index map[string]int, summaries []SessionSummary) {
	lead := make([]Agent, len(summaries)) // best lead candidate so far, per summary
	for _, a := range agents {
		i, ok := index[a.SessionID]
		if !ok {
			continue
		}
		sum := &summaries[i]
		if sum.StartedAt.IsZero() || a.CreatedAt.Before(sum.StartedAt) {
			sum.StartedAt = a.CreatedAt
		}
		if lead[i].Handle == "" || betterLead(a, lead[i]) {
			lead[i] = a
		}
	}
	for i := range summaries {
		summaries[i].LeadHandle = lead[i].Handle
	}
}

// fillSessionNames joins stored names onto the summaries in one query;
// sessions without a row keep the empty name.
func (s *Store) fillSessionNames(summaries []SessionSummary) error {
	ids := make([]string, 0, len(summaries))
	for _, sum := range summaries {
		ids = append(ids, sum.SessionID)
	}
	var rows []Session
	if err := s.DB.Where("session_id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	byID := make(map[string]string, len(rows))
	for _, r := range rows {
		byID[r.SessionID] = r.Name
	}
	for i := range summaries {
		summaries[i].Name = byID[summaries[i].SessionID]
	}
	return nil
}

// SessionsByHandle indexes every session-bearing agent by its handle, so a
// caller holding a page of posts can label each one's session without a query
// per author. Two queries total (agents, then names), no post scan.
//
// The summaries carry only what a label needs — SessionID, Name, LeadHandle,
// StartedAt — and deliberately leave Posts and LastActivity zero: rolling up
// activity is ListSessions' job. That is also why a session appears here even
// when all of its posts are tombstoned or it never posted at all: a post card
// still has to name its author's session, whereas a picker row should not
// advertise a session with nothing live in it.
//
// Agents with an empty session_id are not a session and never appear, so a
// lookup for the web human handle simply misses.
func (s *Store) SessionsByHandle() (map[string]SessionSummary, error) {
	var agents []Agent
	if err := s.DB.Where("session_id <> ''").Find(&agents).Error; err != nil {
		return nil, err
	}
	out := map[string]SessionSummary{}
	if len(agents) == 0 {
		return out, nil
	}

	index := make(map[string]int, len(agents)) // session id -> position in sums
	var sums []SessionSummary
	for _, a := range agents {
		if _, seen := index[a.SessionID]; !seen {
			index[a.SessionID] = len(sums)
			sums = append(sums, SessionSummary{SessionID: a.SessionID})
		}
	}
	summarizeAgents(agents, index, sums)
	if err := s.fillSessionNames(sums); err != nil {
		return nil, err
	}
	for _, a := range agents {
		out[a.Handle] = sums[index[a.SessionID]]
	}
	return out, nil
}

// sessionHandleSet returns the session's handles as a lookup set. An unknown
// session is an empty set, not an error: it simply matches nothing.
func (s *Store) sessionHandleSet(sessionID string) (map[string]bool, error) {
	handles, err := s.SessionHandles(sessionID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(handles))
	for _, h := range handles {
		set[h] = true
	}
	return set, nil
}

// BoardPostsBySession is BoardPosts restricted to threads the session took
// part in, keeping BoardPosts' contract exactly: every post of every matching
// thread, id ASC (insertion order, parents before replies), tombstones masked
// rather than dropped — so ThreadSummaries/GroupThreads consume it unchanged.
//
// Participated semantics: a thread matches when ANY live post in it was
// written by one of the session's agents, and a matched thread is returned
// whole — other sessions' replies included. A tombstoned post does NOT
// establish participation (a deleted post is not participation), but still
// renders inside a thread matched by some other post.
func (s *Store) BoardPostsBySession(boardID uint, sessionID string) ([]Post, error) {
	if sessionID == "" {
		return nil, ErrNoSessionID
	}
	mine, err := s.sessionHandleSet(sessionID)
	if err != nil {
		return nil, err
	}
	posts, err := s.BoardPosts(boardID)
	if err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return []Post{}, nil
	}
	// One linear pass: id ASC guarantees a parent is seen before its replies,
	// so rootOf is always populated by the time a reply needs it. An orphaned
	// reply becomes its own root — the same never-drop fallback as GroupThreads.
	rootOf := make(map[uint]uint, len(posts))
	matched := make(map[uint]bool)
	for _, p := range posts {
		root := p.ID
		if p.ParentID != nil {
			if r, ok := rootOf[*p.ParentID]; ok {
				root = r
			}
		}
		rootOf[p.ID] = root
		if p.TombstonedAt == nil && mine[p.AuthorHandle] {
			matched[root] = true
		}
	}
	out := []Post{}
	for _, p := range posts {
		if matched[rootOf[p.ID]] {
			out = append(out, p)
		}
	}
	return out, nil
}

// PostsBySession is ReadBoardTagged restricted to posts authored by the
// session's agents: the same flat newest-first listing behind `xfa read`, with
// the same tag/since/limit semantics and the same tombstone masking. Per-post,
// not per-thread — the flat view has no thread shape to preserve.
func (s *Store) PostsBySession(boardID uint, sessionID, tag string, since time.Time, limit int) ([]Post, error) {
	if sessionID == "" {
		return nil, ErrNoSessionID
	}
	if limit <= 0 {
		limit = 20
	}
	authors := s.DB.Model(&Agent{}).Select("handle").Where("session_id = ?", sessionID)
	var posts []Post
	err := s.readBoardQuery(boardID, tag, since).
		Where("author_handle IN (?)", authors).
		Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}
