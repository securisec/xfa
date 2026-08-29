package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNoBoard = errors.New("no board registered for this directory (run `xfa init` or pass --board)")

var slugScrub = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify lowercases name and collapses every non-alphanumeric run to "-".
// Names with no ASCII alphanumerics (e.g. "!!!", non-Latin scripts) yield "";
// callers must handle that — EnsureBoard rejects an empty slug.
func Slugify(name string) string {
	s := slugScrub.ReplaceAllString(strings.ToLower(name), "-")
	return strings.Trim(s, "-")
}

// normalizePath cleans p and resolves symlinks so that registration and
// resolution agree on one physical form (e.g. macOS /var vs /private/var,
// provider-supplied cwd vs os.Getwd). If p does not fully exist, the deepest
// existing ancestor is resolved and the nonexistent suffix re-attached; if
// nothing resolves, the cleaned path is returned as-is.
func normalizePath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir := p
	var suffix []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		suffix = append([]string{filepath.Base(dir)}, suffix...)
		dir = parent
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{r}, suffix...)...)
		}
	}
}

func (s *Store) EnsureBoard(slug, desc string) (*Board, error) {
	if slug == "" {
		return nil, errors.New("empty board slug — pass an explicit board name")
	}
	b := Board{Slug: slug, Description: desc}
	err := s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoNothing: true,
	}).Create(&b).Error
	if err != nil {
		return nil, err
	}
	if err := s.DB.Where("slug = ?", slug).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// GetBoardBySlug fetches a board by slug, wrapping not-found in ErrNoBoard.
func (s *Store) GetBoardBySlug(slug string) (*Board, error) {
	var b Board
	if err := s.DB.Where("slug = ?", slug).First(&b).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: b/%s", ErrNoBoard, slug)
		}
		return nil, fmt.Errorf("look up board b/%s: %w", slug, err)
	}
	return &b, nil
}

func (s *Store) RegisterProject(absPath string, boardID uint) error {
	p := Project{Path: normalizePath(absPath), BoardID: boardID}
	return s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{"board_id"}),
	}).Create(&p).Error
}

func (s *Store) ResolveBoard(cwd string) (*Board, error) {
	dir := normalizePath(cwd)
	for {
		var p Project
		err := s.DB.Where("path = ?", dir).First(&p).Error
		if err == nil {
			var b Board
			if err := s.DB.First(&b, p.BoardID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, fmt.Errorf("%w: project registered but board %d missing", ErrNoBoard, p.BoardID)
				}
				return nil, err
			}
			return &b, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNoBoard
		}
		dir = parent
	}
}

func (s *Store) ListBoards() ([]Board, error) {
	var bs []Board
	return bs, s.DB.Order("slug").Find(&bs).Error
}
