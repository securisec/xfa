package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/securisec/xfa/internal/install"
	"github.com/securisec/xfa/internal/remote"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Args:  noPositional,
	Short: "Enable the xfa message board for this project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		slug, _ := cmd.Flags().GetString("board")
		explicit := slug != ""
		if !explicit {
			slug = store.Slugify(filepath.Base(cwd))
			if slug == "" {
				return fmt.Errorf("could not derive a board slug from directory name %q — pass --board <slug>", filepath.Base(cwd))
			}
		} else {
			// Accept the display form users copy out of the docs ("b/shared")
			// and normalize it the same way every other board lookup does,
			// so the board created here is reachable via --board later.
			given := slug
			slug = store.Slugify(strings.TrimPrefix(slug, "b/"))
			if slug == "" {
				return fmt.Errorf("board name %q produced an empty slug — pass --board <slug>", given)
			}
		}
		// Validate providers before touching the DB or the marker so a bad
		// --provider leaves no residue behind.
		providers, _ := cmd.Flags().GetStringSlice("provider")
		for _, p := range providers {
			if _, ok := install.Get(p); !ok {
				return fmt.Errorf("unknown provider %q (supported: %s)", p, strings.Join(install.Names(), ", "))
			}
		}
		// --global and --db name two different databases; refuse both before
		// anything is written.
		global, _ := cmd.Flags().GetBool("global")
		db, _ := cmd.Flags().GetString("db")
		server, _ := cmd.Flags().GetString("server")
		if global && db != "" {
			return fmt.Errorf("--global and --db are mutually exclusive")
		}
		if server != "" && (global || db != "") {
			return fmt.Errorf("--server is mutually exclusive with --db and --global")
		}
		// --db pins this project to a specific database via the .xfa.json
		// marker. Write it BEFORE opening the store so the board/project
		// registration from this very init lands in the custom DB. Re-init
		// without --db leaves an existing marker alone (resolution below
		// still picks it up).
		rollbackMarker := func() {} // set by the --server arm only
		if server != "" {
			// --server pins the project to a remote xfa server: same marker,
			// a URL instead of a path. Nothing local is created or opened.
			if !store.IsRemote(server) {
				return fmt.Errorf("--server %s must be an http(s) URL without credentials", server)
			}
			if env := os.Getenv("XFA_DB"); env != "" && env != server {
				return fmt.Errorf("XFA_DB=%s is set and would shadow the server pin; unset it first", env)
			}
			if fi, err := os.Lstat(filepath.Join(cwd, store.LocalDirName)); err == nil && fi.IsDir() {
				return fmt.Errorf("this project has a local %s/ database; move or remove it before pinning to a server", store.LocalDirName)
			}
			// Snapshot the marker so a failed registration leaves the OLD pin
			// in place (a re-pin that fails must not strand the project on
			// the URL that just refused it), or no marker if there was none.
			markerPath := filepath.Join(cwd, store.MarkerName)
			prev, prevErr := os.ReadFile(markerPath)
			if err := store.WriteMarker(cwd, server); err != nil {
				return err
			}
			rollbackMarker = func() {
				if prevErr == nil {
					os.WriteFile(markerPath, prev, 0o644)
				} else {
					os.Remove(markerPath)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pinned server %s (%s)\n", server, store.MarkerName)
		} else if db != "" {
			abs, err := filepath.Abs(db)
			if err != nil {
				return err
			}
			if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
				return fmt.Errorf("--db %s is a directory — pass the path of a database file", abs)
			}
			if err := store.WriteMarker(cwd, abs); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pinned database %s (%s)\n", abs, store.MarkerName)
		} else {
			// No explicit --db: the project goes LOCAL by default, unless
			// something already pins it (a marker or .xfa/ up the tree, or
			// XFA_DB) or --global asks for the XDG database.
			resolved, err := store.ResolvePath(cwd)
			if err != nil {
				return err
			}
			// DefaultPath() honors XFA_DB too, so `resolved ==
			// DefaultPath()` alone cannot tell "nothing pins this project"
			// apart from "XFA_DB pins everything". Treat a non-empty XFA_DB
			// as the explicit pin it is.
			pinned := os.Getenv("XFA_DB") != "" || resolved != store.DefaultPath()
			switch {
			case global:
				if pinned {
					return fmt.Errorf("this project already resolves to %s — remove %s/ or %s (or unset XFA_DB) before using --global", resolved, store.LocalDirName, store.MarkerName)
				}
			case pinned:
				fmt.Fprintf(cmd.OutOrStdout(), "using database %s\n", resolved)
			default:
				local := filepath.Join(cwd, store.LocalDirName)
				if err := os.MkdirAll(local, 0o755); err != nil {
					return err
				}
				// The SQLite file must never end up committed; an existing
				// .gitignore (possibly user-edited) is left alone.
				gi := filepath.Join(local, ".gitignore")
				if _, err := os.Lstat(gi); os.IsNotExist(err) {
					if err := os.WriteFile(gi, []byte("*\n"), 0o644); err != nil {
						return err
					}
				}
				warnPreviousGlobalRegistration(cmd, cwd)
				fmt.Fprintf(cmd.OutOrStdout(), "created %s/ — project database %s\n", store.LocalDirName, filepath.Join(store.LocalDirName, "board.db"))
			}
		}
		resolved, err := store.ResolvePath(cwd)
		if err != nil {
			return err
		}
		boardSlug := slug
		if store.IsRemote(resolved) {
			// Remote branch, entered by RESOLUTION (flag, committed marker, or
			// XFA_DB=<url> alike): registration runs on the server; nothing
			// local is opened.
			resp, ferr := remote.Forward(resolved, "project-register",
				remote.Request{Args: []string{"--board", slug}, Cwd: cwd}, forwardTimeout)
			if ferr == nil && resp.Code != 0 {
				ferr = fmt.Errorf("%s", strings.TrimSpace(strings.TrimPrefix(resp.Stderr, "Error: ")))
			}
			if ferr != nil {
				rollbackMarker()
				return ferr
			}
		} else {
			s, err := openStore()
			if err != nil {
				return err
			}
			// Explicit --board is intent to share; the collision guard only
			// protects derived slugs.
			b, err := bindProject(s, cwd, slug, !explicit)
			if err != nil {
				return err
			}
			boardSlug = b.Slug
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		for _, name := range providers {
			p, _ := install.Get(name) // validated above
			if err := p.Install(cwd, exe); err != nil {
				return fmt.Errorf("%s install: %w", p.Name(), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed provider: %s\n", p.Name())
		}
		fmt.Fprintf(cmd.OutOrStdout(), "board b/%s ready. Agents in this directory will discover it at session start.\n", boardSlug)
		return nil
	},
}

// warnPreviousGlobalRegistration tells the user that going local forks a
// project whose board history lives in the global database. Best-effort: every
// failure is swallowed, and the global DB is never OPENED when its file is
// absent, since opening would create it.
func warnPreviousGlobalRegistration(cmd *cobra.Command, cwd string) {
	globalPath := store.DefaultPath()
	if _, err := os.Stat(globalPath); err != nil {
		return
	}
	s, err := store.Open(globalPath)
	if err != nil {
		return
	}
	defer func() {
		if sqlDB, err := s.DB.DB(); err == nil {
			sqlDB.Close()
		}
	}()
	var p store.Project
	if err := s.DB.Where("path = ?", store.NormalizePath(cwd)).First(&p).Error; err != nil {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "note: this project was previously registered in the global database (%s); its board history stays there. Pass --global to keep using it.\n", globalPath)
}

// providerCompletion completes --provider (init and uninstall) with every
// registered provider name.
func providerCompletion(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return install.Names(), cobra.ShellCompDirectiveNoFileComp
}

func init() {
	initCmd.Flags().StringSlice("provider", []string{"claude"}, "providers to set up ("+strings.Join(install.Names(), ", ")+")")
	initCmd.Flags().String("board", "", "board slug (default: slugified directory name)")
	initCmd.Flags().String("db", "", "pin this project to a specific database file (writes "+store.MarkerName+")")
	initCmd.Flags().String("server", "", "pin this project to a remote xfa server URL (writes "+store.MarkerName+")")
	initCmd.Flags().Bool("global", false, "use the global XDG database instead of a project-local "+store.LocalDirName+"/ directory")

	if err := initCmd.RegisterFlagCompletionFunc("provider", providerCompletion); err != nil {
		log.Fatalf("%+v", err)
	}

	rootCmd.AddCommand(initCmd)
}
