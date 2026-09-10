package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// bindProject is the server-side half of init: create-or-find the board and
// bind dir to it. Local init passes checkCollision = !explicit (a derived slug
// bound elsewhere is a mistake); the remote verb passes false (on a shared
// server a derived slug is intent to share). Both paths, one function.
func bindProject(s *store.Store, dir, slug string, checkCollision bool) (*store.Board, error) {
	if checkCollision {
		key := store.NormalizePath(dir)
		var existing store.Project
		err := s.DB.Joins("JOIN boards ON boards.id = projects.board_id").
			Where("boards.slug = ? AND projects.path <> ?", slug, key).
			First(&existing).Error
		if err == nil {
			return nil, fmt.Errorf("board b/%s is already bound to %s — pass --board <other-slug>, or --board %s to share it", slug, existing.Path, slug)
		}
	}
	b, err := s.EnsureBoard(slug, "project board for "+dir)
	if err != nil {
		return nil, err
	}
	if err := s.RegisterProject(dir, b.ID); err != nil {
		return nil, err
	}
	return b, nil
}

// project-register is what `xfa init --server` forwards: registration
// without provider installation. Hidden — humans run init. Because the
// server is unauthenticated, an existing binding is never re-pointed:
// RegisterProject is ON CONFLICT DO UPDATE, so the guard runs first and
// compares the STORED (normalized) form.
var projectRegisterCmd = &cobra.Command{
	Use:    "project-register",
	Hidden: true,
	Args:   noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
		given, _ := cmd.Flags().GetString("board")
		slug := store.Slugify(strings.TrimPrefix(given, "b/"))
		if slug == "" && strings.TrimSpace(given) != "" {
			return fmt.Errorf("--board %q produced an empty slug", given)
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		dir := cwd()
		key := store.NormalizePath(dir)
		p, err := s.ResolveProject(dir)
		boardSlugOf := func(id uint) string {
			var b store.Board
			if err := s.DB.First(&b, id).Error; err != nil {
				return ""
			}
			return b.Slug
		}
		if slug == "" {
			// No explicit board: JOIN the server's board. Re-use this dir's
			// (or an ancestor's) existing binding so re-init is idempotent;
			// otherwise the server must have exactly one board to join.
			switch {
			case err == nil:
				slug = boardSlugOf(p.BoardID)
			case errors.Is(err, store.ErrNoBoard):
				boards, lerr := s.ListBoards()
				if lerr != nil {
					return lerr
				}
				switch len(boards) {
				case 1:
					slug = boards[0].Slug
				case 0:
					return fmt.Errorf("server has no board yet \u2014 ask the host to run xfa init, or pass --board <slug>")
				default:
					names := make([]string, len(boards))
					for i, b := range boards {
						names[i] = "b/" + b.Slug
					}
					return fmt.Errorf("server has %d boards (%s) \u2014 pass --board <slug>", len(boards), strings.Join(names, ", "))
				}
			default:
				return err
			}
			if slug == "" {
				return fmt.Errorf("could not determine the server's board \u2014 pass --board <slug>")
			}
		}
		switch {
		case err == nil && p.Path == key:
			if got := boardSlugOf(p.BoardID); got != "" && got != slug {
				return fmt.Errorf("%s is already bound to b/%s", dir, got)
			}
		case err == nil:
			// An ANCESTOR is registered: binding a descendant to a different
			// board would carve a hole out of the ancestor's tree.
			if got := boardSlugOf(p.BoardID); got != "" && got != slug {
				return fmt.Errorf("%s is already inside b/%s", dir, got)
			}
		case !errors.Is(err, store.ErrNoBoard):
			return err
		}
		// The other direction: registering an ancestor of an existing project
		// captures ResolveProject's walk-up for every path under it. Skipped
		// when dir is already registered — it captures nothing new.
		// instr, not LIKE ("_" is a LIKE wildcard and appears in real paths)
		// and not substr with a Go length (SQLite counts TEXT in CHARACTERS,
		// so a multibyte path component would misalign the comparison).
		if err != nil || p.Path != key {
			var existing store.Project
			if e := s.DB.Where("instr(path, ?) = 1", key+"/").First(&existing).Error; e == nil {
				return fmt.Errorf("%s would capture %s (b/%s)", dir, existing.Path, boardSlugOf(existing.BoardID))
			}
		}
		b, err := bindProject(s, dir, slug, false)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "registered %s → b/%s\n", dir, b.Slug)
		return nil
	},
}

func init() {
	projectRegisterCmd.Flags().String("board", "", "board slug")
	rootCmd.AddCommand(projectRegisterCmd)
}
