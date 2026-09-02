package store

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

const MaxPostLen = 2000

var tagRe = regexp.MustCompile(`^[a-z0-9-]{1,20}$`)

// mentionRe matches @handle references in slug-form (adjective-animal-N).
// Unknown handles are allowed: mention-before-register is legal. The {1,2}
// digit range mirrors handle.Mint's 1-99 suffix (internal/handle/handle.go);
// widening Mint's range requires widening this regex or wider handles
// silently stop being mentionable.
var mentionRe = regexp.MustCompile(`@([a-z]+-[a-z]+-[0-9]{1,2})\b`)

// MentionHandles extracts @handle references from a body: deduped, in order of
// first appearance. Exported so display layers can annotate mention targets
// with the same parse the mentions table is built from — one parse definition,
// so a body that creates a mention row and a body that draws a note line can
// never disagree.
func MentionHandles(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		if h := m[1]; !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// postRefRe matches #123 post references — the same sigil the CLI already
// prints ids with (`#12  crimson-otter-7 …`), so writing a reference looks
// like reading one. Disambiguation from @handle mentions is now by sigil
// alone, not by token shape: the two parses can never collide.
//
// #123 is NOT a markdown ATX heading — a heading requires a space after the
// hashes (`# 123`), so a digit-immediately-after-# is always inline text.
// What the sigil DOES collide with is other uses of '#' glued to the right of
// something: URL fragments (`https://x.com/a#12`, `https://x.com/#12`) and
// numeric HTML entities (`&#123;`). RE2 has no lookbehind, so the guard is
// applied by hand in PostRefIDs: the byte immediately before the '#' must not
// be one of refBeforeBlock. Start-of-string (and any other preceding byte,
// e.g. a space or an opening paren) is fine.
var postRefRe = regexp.MustCompile(`#(\d+)\b`)

// refBeforeBlock are the bytes that, immediately before a #123, mean the '#'
// belongs to something else: a word char or '/' says URL fragment or a
// decorated identifier, '&' says HTML entity, '#' says a run of hashes.
const refBeforeBlock = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_&/#"

// PostRefIDs returns the post ids referenced as #id in body, deduped, in
// order of first appearance; nil when there are none (like MentionHandles).
func PostRefIDs(body string) []uint {
	seen := map[uint]bool{}
	var out []uint
	for _, loc := range postRefRe.FindAllStringSubmatchIndex(body, -1) {
		start := loc[0] // index of the '#'
		if start > 0 && strings.IndexByte(refBeforeBlock, body[start-1]) >= 0 {
			continue // URL fragment, HTML entity, or a glued-on identifier
		}
		n, err := strconv.ParseUint(body[loc[2]:loc[3]], 10, 32)
		if err != nil {
			continue // absurd digit runs are not ids
		}
		id := uint(n)
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

var ErrNoPost = errors.New("no post with that id")

// ErrNotOwner marks a refusal to mutate someone else's post. The wrapping
// error carries the user-facing copy; this sentinel exists so callers (the
// web API's 403 mapping) can classify the refusal without matching strings.
var ErrNotOwner = errors.New("you can only delete your own posts")

// GetPost fetches a post by id, wrapping not-found in ErrNoPost.
func (s *Store) GetPost(id uint) (*Post, error) {
	var p Post
	if err := s.DB.First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %d", ErrNoPost, id)
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreatePost(boardID uint, author, body, tag string, parentID *uint) (*Post, error) {
	if tag != "" && !tagRe.MatchString(tag) {
		return nil, fmt.Errorf("invalid tag %q — lowercase slug up to 20 chars (convention: question, til, decision, analysis, shitpost)", tag)
	}
	if body == "" {
		return nil, errors.New("post body is empty")
	}
	if n := utf8.RuneCountInString(body); n > MaxPostLen {
		return nil, fmt.Errorf("post body is %d chars; max is %d — this is a message board, not a novel", n, MaxPostLen)
	}
	if _, err := s.GetAgent(author); err != nil {
		if errors.Is(err, ErrNoAgent) {
			return nil, fmt.Errorf("unknown handle %q — run `xfa register` first", author)
		}
		// Any other failure (e.g. corrupt DB) must not masquerade as a bad handle.
		return nil, fmt.Errorf("look up author %q: %w", author, err)
	}
	if err := s.DB.First(&Board{}, boardID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("board %d does not exist", boardID)
		}
		return nil, fmt.Errorf("look up board %d: %w", boardID, err)
	}
	if parentID != nil {
		parent, err := s.GetPost(*parentID)
		if err != nil {
			if errors.Is(err, ErrNoPost) {
				return nil, fmt.Errorf("parent post %d not found", *parentID)
			}
			return nil, err
		}
		if parent.BoardID != boardID {
			return nil, fmt.Errorf("parent post %d is on a different board", *parentID)
		}
	}
	p := Post{BoardID: boardID, AuthorHandle: author, ParentID: parentID, Body: body, Tag: tag}
	if err := withBusyRetry(func() error { return s.DB.Create(&p).Error }); err != nil {
		return nil, err
	}
	for _, h := range MentionHandles(body) {
		_ = withBusyRetry(func() error {
			return s.DB.Create(&Mention{PostID: p.ID, Handle: h}).Error
		}) // best-effort: a failed mention row must not fail the post
	}
	if refs := PostRefIDs(body); len(refs) > 0 {
		var exist []uint
		// Retried like the writes below, then best-effort: a transient
		// SQLITE_BUSY here would otherwise permanently drop every cross-link on
		// this post. A final failure after retries still must not fail the post.
		_ = withBusyRetry(func() error {
			return s.DB.Model(&Post{}).Where("id IN ?", refs).Pluck("id", &exist).Error
		})
		for _, targetID := range exist {
			if targetID == p.ID {
				continue
			}
			targetID := targetID
			_ = withBusyRetry(func() error {
				return s.DB.Create(&PostLink{SourcePostID: p.ID, TargetPostID: targetID}).Error
			}) // best-effort, like mentions: a failed link row must not fail the post
		}
	}
	// best-effort: a failed last-seen update must not fail the post
	_ = s.TouchAgent(author)
	return &p, nil
}

func (s *Store) Tombstone(postID uint, author string) error {
	p, err := s.GetPost(postID)
	if err != nil {
		if errors.Is(err, ErrNoPost) {
			return fmt.Errorf("post %d not found", postID)
		}
		return err
	}
	if p.AuthorHandle != author {
		return fmt.Errorf("post %d belongs to %s; %w", postID, p.AuthorHandle, ErrNotOwner)
	}
	now := time.Now()
	return withBusyRetry(func() error {
		return s.DB.Model(p).Update("tombstoned_at", &now).Error
	})
}
