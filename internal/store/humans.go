package store

import (
	"path/filepath"
	"time"
)

// ProviderHuman is the agents.provider value minted for web-UI writes
// (internal/web/identity.go registers the web handle with it).
const ProviderHuman = "human"

// Author is the per-handle decoration every listing fans out: the [human]
// marker and, on a shared multi-project DB, the absolute project path the
// handle registered from. json tags let cmd embed it flat in --json rows.
type Author struct {
	Human       bool   `json:"human,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
}

// Project is the short label (folder basename) for human-facing surfaces.
func (a Author) Project() string {
	if a.ProjectPath == "" {
		return ""
	}
	return filepath.Base(a.ProjectPath)
}

// AuthorsFor resolves the decorations for these handles in two queries (a
// project count and one join). Project
// paths are returned only when the DB holds more than one project — a
// single-repo DB shows exactly what it did before the column existed.
func (s *Store) AuthorsFor(handles []string) (map[string]Author, error) {
	out := map[string]Author{}
	if len(handles) == 0 {
		return out, nil
	}
	var projects int64
	if err := s.DB.Model(&Project{}).Count(&projects).Error; err != nil {
		return nil, err
	}
	var rows []struct {
		Handle   string
		Provider string
		Path     string
	}
	err := s.DB.Table("agents").
		Select("agents.handle, agents.provider, projects.path").
		Joins("LEFT JOIN projects ON projects.id = agents.project_id").
		Where("agents.handle IN ?", handles).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		a := Author{Human: r.Provider == ProviderHuman}
		if projects > 1 {
			a.ProjectPath = r.Path
		}
		out[r.Handle] = a
	}
	return out, nil
}

// isHumanPost reports whether the post's author is a provider=human agent.
// A missing author row is not human.
func (s *Store) isHumanPost(p *Post) bool {
	a, err := s.GetAgent(p.AuthorHandle)
	return err == nil && a.Provider == ProviderHuman
}

// HandleSet returns the deduped author handles of posts.
func HandleSet(posts []Post) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range posts {
		if !seen[p.AuthorHandle] {
			seen[p.AuthorHandle] = true
			out = append(out, p.AuthorHandle)
		}
	}
	return out
}

// ReadBoardHuman is ReadBoardTagged narrowed to human-authored posts.
func (s *Store) ReadBoardHuman(boardID uint, tag string, since time.Time, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	var posts []Post
	err := s.readBoardQuery(boardID, tag, since).
		Where("author_handle IN (SELECT handle FROM agents WHERE provider = ?)", ProviderHuman).
		Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}

// UnaddressedHumanCount counts live human-authored posts — top-level or
// reply — with no live non-human direct reply and no resolution — the
// orchestrator's queue. Deliberately NOT restricted to top-level: with web
// inline replies, human messages are frequently replies themselves, and each
// one still wants an agent's direct reply (or a resolution) to count as
// addressed. A human's own reply to their own post does not address the
// parent, and is itself counted as unaddressed until something non-human
// replies to it directly.
func (s *Store) UnaddressedHumanCount(boardID uint) (int64, error) {
	var n int64
	err := s.DB.Raw(`SELECT COUNT(*) FROM posts p
		WHERE p.board_id = ?
		  AND p.tombstoned_at IS NULL
		  AND p.resolved_at IS NULL
		  AND p.author_handle IN (SELECT handle FROM agents WHERE provider = ?)
		  AND NOT EXISTS (
			SELECT 1 FROM posts r
			JOIN agents a ON a.handle = r.author_handle
			WHERE r.parent_id = p.id
			  AND r.tombstoned_at IS NULL
			  AND a.provider <> ?
		  )`, boardID, ProviderHuman, ProviderHuman).Scan(&n).Error
	return n, err
}

// PostsByAuthor lists one handle's own posts and replies, newest first.
// boardID 0 means all boards (Stats' convention). Tombstoned posts are kept
// and masked — the author's own deletions still belong in their own list.
func (s *Store) PostsByAuthor(boardID uint, handle string, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	q := s.DB.Model(&Post{}).Where("author_handle = ?", handle)
	if boardID != 0 {
		q = q.Where("board_id = ?", boardID)
	}
	var posts []Post
	err := q.Order("id DESC").Limit(limit).Find(&posts).Error
	return maskTombstones(posts), err
}
