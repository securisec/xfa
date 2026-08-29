package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MarkerName is the per-project database marker file, written by
// `xfa init --db` at the project root. Content is a JSON object whose "db"
// key holds the absolute path to the SQLite database for that project.
const MarkerName = ".xfa.json"

// markerDBKey is the marker's JSON key holding the database path.
const markerDBKey = "db"

// LocalDirName is the conventional project data directory, created by a bare
// `xfa init` at the project root. Its presence pins the project to the
// database inside it — no marker file needed.
const LocalDirName = ".xfa"

// LocalDBPath returns the project-local database path for a project rooted at
// dir. Same board.db filename as the global database.
func LocalDBPath(dir string) string {
	return filepath.Join(dir, LocalDirName, "board.db")
}

// ResolvePath resolves the database path for cwd:
//
//  1. XFA_DB env var (non-empty), matching DefaultPath's treatment of
//     empty-as-unset,
//  2. walking up from cwd to the filesystem root, the nearest directory
//     holding either a .xfa.json marker (its "db" path) or a .xfa directory
//     (<dir>/.xfa/board.db). At the SAME level the explicit marker wins over
//     the conventional directory; across levels the nearest wins,
//  3. DefaultPath() (the XDG global database).
//
// cwd is the directory to resolve FOR — usually the process working
// directory, but the hook path passes the hook payload's cwd (the same cwd it
// resolves the board from), since providers may invoke `xfa hook` from
// anywhere. A relative or empty cwd is made absolute against the process
// working directory.
//
// A marker that exists but is corrupt (unreadable, not a JSON object, missing
// a non-empty string "db", or holding a relative path) is a loud error naming
// the marker file — as is a .xfa that exists but is not a directory. Never a
// silent fall-through to the global database, which would fork board data.
func ResolvePath(cwd string) (string, error) {
	if p := os.Getenv("XFA_DB"); p != "" {
		return p, nil
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	for {
		marker := filepath.Join(dir, MarkerName)
		if _, err := os.Lstat(marker); err == nil {
			return readMarkerDB(marker)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("checking xfa marker %s: %w", marker, err)
		}
		local := filepath.Join(dir, LocalDirName)
		if fi, err := os.Lstat(local); err == nil {
			if !fi.IsDir() {
				return "", fmt.Errorf("%s exists but is not a directory — it should be xfa's project data directory (or be removed)", local)
			}
			return LocalDBPath(dir), nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("checking xfa directory %s: %w", local, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return DefaultPath(), nil
		}
		dir = parent
	}
}

// readMarkerDB parses the marker at path and returns its database path.
func readMarkerDB(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unreadable xfa marker %s: %w", path, err)
	}
	m, err := parseMarker(path, raw)
	if err != nil {
		return "", err
	}
	var db string
	if v, ok := m[markerDBKey]; ok {
		if err := json.Unmarshal(v, &db); err != nil {
			return "", fmt.Errorf("xfa marker %s: %q must be a string", path, markerDBKey)
		}
	}
	if db == "" {
		return "", fmt.Errorf("xfa marker %s: missing or empty %q key", path, markerDBKey)
	}
	if !filepath.IsAbs(db) {
		return "", fmt.Errorf("xfa marker %s: %q must be an absolute path, got %q", path, markerDBKey, db)
	}
	return db, nil
}

// WriteMarker writes (or updates) dir/.xfa.json to pin the project to dbPath,
// preserving any other keys an existing marker holds. Output is compact JSON
// plus a trailing newline. A corrupt existing marker is an error, never
// clobbered.
func WriteMarker(dir, dbPath string) error {
	path := filepath.Join(dir, MarkerName)
	m := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if m, err = parseMarker(path, raw); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("unreadable xfa marker %s: %w", path, err)
	}
	enc, err := json.Marshal(dbPath)
	if err != nil {
		return err
	}
	m[markerDBKey] = enc
	out, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// parseMarker decodes marker bytes, requiring a JSON object (a literal `null`
// decodes to an empty object so its absence of "db" errors loudly upstream).
func parseMarker(path string, raw []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("xfa marker %s is not a JSON object: %w", path, err)
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}
