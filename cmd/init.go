package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
		if server != "" {
			// --server pins the project to a remote xfa server: same marker,
			// a URL instead of a path. Nothing local is created or opened, and
			// the marker is written only AFTER the server accepts the
			// registration (below), so an interrupted or refused init — a
			// dead server, a rejected board, a Ctrl-C at the picker — strands
			// nothing on disk and needs no rollback.
			if !store.IsRemote(server) {
				return fmt.Errorf("--server %s must be an http(s) URL without credentials", server)
			}
			if env := os.Getenv("XFA_DB"); env != "" && env != server {
				return fmt.Errorf("XFA_DB=%s is set and would shadow the server pin; unset it first", env)
			}
			if fi, err := os.Lstat(filepath.Join(cwd, store.LocalDirName)); err == nil && fi.IsDir() {
				return fmt.Errorf("this project has a local %s/ database; move or remove it before pinning to a server", store.LocalDirName)
			}
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
		// The --server arm defers its marker, so ResolvePath cannot see it
		// yet; the URL is authoritative there. Every other arm resolves from
		// disk (the --db/local marker it wrote, or a committed one).
		resolved := server
		if server == "" {
			r, err := store.ResolvePath(cwd)
			if err != nil {
				return err
			}
			resolved = r
		}
		boardSlug := slug
		if store.IsRemote(resolved) {
			// Remote branch, entered by RESOLUTION (flag, committed marker, or
			// XFA_DB=<url> alike): registration runs on the server; nothing
			// local is opened. Without an explicit --board the client JOINS
			// the server's board rather than forking one named after its own
			// directory (two machines rarely share a directory basename).
			regArgs := []string(nil)
			if explicit {
				regArgs = []string{"--board", slug}
			}
			resp, ferr := remote.Forward(resolved, "project-register",
				remote.Request{Args: regArgs, Cwd: cwd}, verbForwardTimeout())
			// The server rejects a non-explicit register only when it cannot
			// choose a board (none, or several) — an already-bound dir or a
			// sole-board server succeeds on the first try, so re-init never
			// prompts. On a TTY, let the human disambiguate and retry;
			// pickRemoteBoard returns "" when it cannot help (0 or 1 board),
			// so the original error stands. A transport error is never a pick.
			if ferr == nil && resp.Code != 0 && !explicit && stdinIsTTY() {
				chosen, perr := pickRemoteBoard(cmd, resolved, cwd)
				if perr != nil {
					return perr
				}
				if chosen != "" {
					resp, ferr = remote.Forward(resolved, "project-register",
						remote.Request{Args: []string{"--board", chosen}, Cwd: cwd}, verbForwardTimeout())
				}
			}
			if ferr == nil && resp.Code != 0 {
				ferr = respErr(resp)
			}
			if ferr != nil {
				return ferr
			}
			// The server prints "registered <dir> → b/<slug>"; surface the
			// board it actually bound us to, not our own directory name.
			i := strings.LastIndex(resp.Stdout, "\u2192 b/")
			if i < 0 {
				return fmt.Errorf("server registered but reported no board: %q", resp.Stdout)
			}
			boardSlug = strings.TrimSpace(resp.Stdout[i+len("\u2192 b/"):])
			// Write the marker only now — after the server accepted us — so an
			// interrupted pick or a refused registration leaves nothing behind.
			if server != "" {
				if err := store.WriteMarker(cwd, server); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "pinned server %s (%s)\n", server, store.MarkerName)
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

// respErr turns a non-zero remote response into the same error text a local
// command would print, stripping the "Error: " prefix cobra adds on the wire.
func respErr(resp remote.Response) error {
	return fmt.Errorf("%s", strings.TrimSpace(strings.TrimPrefix(resp.Stderr, "Error: ")))
}

// pickRemoteBoard fetches the server's boards and, when there is more than
// one, prompts the human to join one by number or name a new one (scrubbed
// through Slugify). It returns "" for 0 or 1 board so the server keeps
// deciding (join the sole board, or report there is none). TTY-gated by the
// caller; a plain numbered stdin prompt, deliberately not a TUI.
func pickRemoteBoard(cmd *cobra.Command, base, dir string) (string, error) {
	resp, err := remote.Forward(base, "boards",
		remote.Request{Args: []string{"--json"}, Cwd: dir}, verbForwardTimeout())
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", respErr(resp)
	}
	var boards []store.Board
	if err := json.Unmarshal([]byte(resp.Stdout), &boards); err != nil {
		return "", fmt.Errorf("cannot read server boards: %w", err)
	}
	if len(boards) <= 1 {
		return "", nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "server has %d boards \u2014 enter a number to join, or type a new name (default 1):\n", len(boards))
	for i, b := range boards {
		fmt.Fprintf(out, "  %d) b/%s\n", i+1, b.Slug)
	}
	fmt.Fprint(out, "> ")
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		if e := sc.Err(); e != nil {
			return "", e
		}
		return "", fmt.Errorf("no board selected")
	}
	line := strings.TrimSpace(sc.Text())
	if line == "" {
		return boards[0].Slug, nil // default: the first board listed
	}
	if n, err := strconv.Atoi(line); err == nil {
		if n < 1 || n > len(boards) {
			return "", fmt.Errorf("choice %d is out of range (1-%d)", n, len(boards))
		}
		return boards[n-1].Slug, nil
	} else if errors.Is(err, strconv.ErrRange) {
		return "", fmt.Errorf("choice %q is out of range (1-%d)", line, len(boards))
	}
	slug := store.Slugify(strings.TrimPrefix(line, "b/"))
	if slug == "" {
		return "", fmt.Errorf("board name %q produced an empty slug", line)
	}
	return slug, nil
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
