package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/install"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/pflag"
)

// resetProviderFlags restores the --provider StringSlice flags to their
// defaults. Cobra keeps flag state across Execute calls in-process, and a
// StringSlice APPENDS on every later explicit set, so without this a value
// from one test (e.g. "bogus") leaks into the next.
func resetProviderFlags(t *testing.T) {
	t.Helper()
	reset := func(fs *pflag.FlagSet, def []string) {
		f := fs.Lookup("provider")
		if err := f.Value.(pflag.SliceValue).Replace(def); err != nil {
			t.Fatalf("reset provider flag: %v", err)
		}
		f.Changed = false
	}
	reset(initCmd.Flags(), []string{"claude"})
	reset(uninstallCmd.Flags(), install.Names())
}

// markerProject prepares a temp project dir as cwd with XFA_DB unset (empty
// falls through to the marker) and XDG_DATA_HOME pointed at a guard dir, so a
// resolution regression can never touch the developer's real global DB. It
// returns the project dir and the guarded global DB path (which must stay
// absent unless a test wants it).
func markerProject(t *testing.T) (projectDir, globalDB string) {
	t.Helper()
	resetProviderFlags(t)
	t.Cleanup(func() { resetProviderFlags(t) })
	projectDir = t.TempDir()
	xdg := filepath.Join(projectDir, ".xdg-guard")
	t.Chdir(projectDir)
	t.Setenv("XFA_DB", "")
	t.Setenv("XDG_DATA_HOME", xdg)
	return projectDir, filepath.Join(xdg, "xfa", "board.db")
}

// resetGlobalFlag restores init's --global to false. Cobra keeps flag state
// across Execute calls in-process, so a test that passes --global would
// otherwise leak "true" into every later init in the same process.
func resetGlobalFlag(t *testing.T) {
	t.Helper()
	f := initCmd.Flags().Lookup("global")
	if f == nil {
		t.Fatal("init has no --global flag")
	}
	if err := f.Value.Set("false"); err != nil {
		t.Fatalf("reset global flag: %v", err)
	}
	f.Changed = false
}

// localProject is markerProject plus the --global flag reset, for the tests
// that exercise the local-by-default database.
func localProject(t *testing.T) (projectDir, globalDB string) {
	t.Helper()
	projectDir, globalDB = markerProject(t)
	resetGlobalFlag(t)
	t.Cleanup(func() { resetGlobalFlag(t) })
	return projectDir, globalDB
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, store.MarkerName))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return string(raw)
}

func boardExists(t *testing.T, dbPath, slug string) bool {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer func() {
		if sqlDB, err := s.DB.DB(); err == nil {
			sqlDB.Close()
		}
	}()
	var n int64
	if err := s.DB.Model(&store.Board{}).Where("slug = ?", slug).Count(&n).Error; err != nil {
		t.Fatalf("count boards: %v", err)
	}
	return n > 0
}

// boardCount counts every board row in the DB at dbPath.
func boardCount(t *testing.T, dbPath string) int64 {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer func() {
		if sqlDB, err := s.DB.DB(); err == nil {
			sqlDB.Close()
		}
	}()
	var n int64
	if err := s.DB.Model(&store.Board{}).Count(&n).Error; err != nil {
		t.Fatalf("count boards: %v", err)
	}
	return n
}

// projectRegistered reports whether the DB at dbPath has a projects row for
// dir (matching RegisterProject's symlink-resolved key).
func projectRegistered(t *testing.T, dbPath, dir string) bool {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer func() {
		if sqlDB, err := s.DB.DB(); err == nil {
			sqlDB.Close()
		}
	}()
	key := projectKey(dir)
	var n int64
	if err := s.DB.Model(&store.Project{}).Where("path = ?", key).Count(&n).Error; err != nil {
		t.Fatalf("count projects: %v", err)
	}
	return n > 0
}

// `xfa init --db` writes the .xfa.json marker BEFORE opening the store, so
// this very init's board lands in the custom DB — and the global DB is never
// created at all.
func TestInitDBWritesMarkerAndRegistersInCustomDB(t *testing.T) {
	project, globalDB := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "nested", "custom.db")

	out := runXfa(t, "init", "--board", "markertest", "--db", dbPath, "--provider", "claude")
	if !strings.Contains(out, "board b/markertest ready") {
		t.Fatalf("init output: %q", out)
	}

	if got, want := readMarker(t, project), `{"db":"`+dbPath+`"}`+"\n"; got != want {
		t.Fatalf("marker = %q, want %q", got, want)
	}
	if !boardExists(t, dbPath, "markertest") {
		t.Fatal("board must be registered in the custom DB")
	}
	if _, err := os.Stat(globalDB); !os.IsNotExist(err) {
		t.Fatalf("global DB must not be touched, stat err = %v", err)
	}
}

// Re-init WITHOUT --db in a marked project leaves the marker alone and still
// registers into the marker DB, not the global one.
func TestReinitWithoutDBStaysOnMarkerDB(t *testing.T) {
	project, globalDB := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	runXfa(t, "init", "--board", "markertest", "--db", dbPath, "--provider", "claude")
	before := readMarker(t, project)

	// --db "" is how a fresh CLI invocation looks to cobra state shared
	// across Execute calls in tests: empty means "not given".
	runXfa(t, "init", "--board", "markertest2", "--db", "", "--provider", "claude")

	if after := readMarker(t, project); after != before {
		t.Fatalf("re-init without --db must not touch the marker: %q -> %q", before, after)
	}
	if !boardExists(t, dbPath, "markertest2") {
		t.Fatal("re-init must register into the marker DB")
	}
	if _, err := os.Stat(globalDB); !os.IsNotExist(err) {
		t.Fatalf("global DB must not be touched, stat err = %v", err)
	}
}

// Re-init WITH a different --db updates the marker to the new path.
func TestReinitWithNewDBUpdatesMarker(t *testing.T) {
	project, _ := markerProject(t)
	db1 := filepath.Join(t.TempDir(), "one.db")
	db2 := filepath.Join(t.TempDir(), "two.db")

	runXfa(t, "init", "--board", "markertest", "--db", db1, "--provider", "claude")
	runXfa(t, "init", "--board", "markertest", "--db", db2, "--provider", "claude")

	if got, want := readMarker(t, project), `{"db":"`+db2+`"}`+"\n"; got != want {
		t.Fatalf("marker = %q, want %q", got, want)
	}
	if !boardExists(t, db2, "markertest") {
		t.Fatal("board must be registered in the new DB")
	}
}

// --db pointing at an existing directory is rejected before anything is
// written.
func TestInitDBRejectsDirectory(t *testing.T) {
	project, _ := markerProject(t)
	dir := t.TempDir()

	_, err := runXfaErr(t, "init", "--board", "markertest", "--db", dir, "--provider", "claude")
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("init --db <dir> must be rejected, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, store.MarkerName)); !os.IsNotExist(statErr) {
		t.Fatal("rejected init must not write a marker")
	}
}

// A bad --provider is rejected BEFORE any DB/marker mutation, with the
// supported names listed dynamically from the registry.
func TestInitUnknownProviderLeavesNoResidue(t *testing.T) {
	project, globalDB := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	_, err := runXfaErr(t, "init", "--board", "markertest", "--db", dbPath, "--provider", "bogus")
	if err == nil {
		t.Fatal("unknown provider must be rejected")
	}
	if !strings.Contains(err.Error(), `"bogus"`) || !strings.Contains(err.Error(), "claude, opencode, pi") {
		t.Fatalf("error must name the provider and list supported ones, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, store.MarkerName)); !os.IsNotExist(statErr) {
		t.Fatal("bad --provider must not write a marker")
	}
	for _, p := range []string{dbPath, globalDB} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Fatalf("bad --provider must leave no DB at %s", p)
		}
	}
}

// `xfa init --provider pi` installs the pi extension + skill through the
// registry, and `xfa uninstall --provider pi` removes them again.
func TestInitAndUninstallProviderPi(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	out := runXfa(t, "init", "--board", "pitest", "--db", dbPath, "--provider", "pi")
	if !strings.Contains(out, "installed provider: pi") {
		t.Fatalf("init output: %q", out)
	}
	for _, p := range []string{
		filepath.Join(project, ".pi", "extensions", "xfa.ts"),
		filepath.Join(project, ".pi", "skills", "xfa", "SKILL.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("init --provider pi must create %s: %v", p, err)
		}
	}

	out = runXfa(t, "uninstall", "--provider", "pi")
	if !strings.Contains(out, "removed provider: pi") {
		t.Fatalf("uninstall output: %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, ".pi")); !os.IsNotExist(err) {
		t.Fatalf("uninstall --provider pi must remove .pi, stat err = %v", err)
	}
}

// uninstall validates providers against the registry with the same dynamic
// name list.
func TestUninstallUnknownProviderRejected(t *testing.T) {
	markerProject(t)
	out, err := runXfaErr(t, "uninstall", "--provider", "bogus")
	if err == nil {
		t.Fatal("unknown provider must be rejected")
	}
	if !strings.Contains(err.Error(), `"bogus"`) || !strings.Contains(err.Error(), "claude, opencode, pi") {
		t.Fatalf("error must name the provider and list supported ones, got: %v", err)
	}
	if strings.Contains(out, "removed provider") {
		t.Fatalf("validation must happen before any provider runs, got output %q", out)
	}
}

// uninstall removes the marker from cwd (keeping the DB file), and is quiet
// about an absent marker.
func TestUninstallRemovesMarker(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	runXfa(t, "init", "--board", "markertest", "--db", dbPath, "--provider", "claude")

	out := runXfa(t, "uninstall", "--provider", "claude,opencode")
	if !strings.Contains(out, store.MarkerName) {
		t.Fatalf("uninstall must report the removed marker, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, store.MarkerName)); !os.IsNotExist(err) {
		t.Fatal("marker must be removed")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("uninstall must keep the DB file: %v", err)
	}

	// Second run: no marker left — no removal line, no error.
	out = runXfa(t, "uninstall", "--provider", "claude,opencode")
	if strings.Contains(out, store.MarkerName) {
		t.Fatalf("no marker to remove, but got %q", out)
	}
}

// reset resolves through the marker: it deletes the project's pinned DB (and
// prints that path), not the global one.
func TestResetTargetsMarkerDB(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	if err := store.WriteMarker(project, dbPath); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.EnsureBoard("resetmarker", ""); err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	if sqlDB, err := s.DB.DB(); err == nil {
		sqlDB.Close()
	}

	out := runXfa(t, "reset", "--yes")
	if !strings.Contains(out, dbPath) {
		t.Fatalf("reset must print the resolved marker DB path, got %q", out)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("reset must delete the marker DB")
	}
}

// A corrupt marker makes normal commands fail LOUDLY, naming the file...
func TestCorruptMarkerFailsCommandsLoudly(t *testing.T) {
	project, _ := markerProject(t)
	marker := filepath.Join(project, store.MarkerName)
	if err := os.WriteFile(marker, []byte(`{broken`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	_, err := runXfaErr(t, "boards")
	if err == nil || !strings.Contains(err.Error(), marker) {
		t.Fatalf("corrupt marker must fail loudly naming the file, got %v", err)
	}
}

// The hook resolves the DATABASE from the hook payload's cwd — the same cwd
// it already resolves the board from — not from the process cwd: providers
// may invoke `xfa hook` from a process cwd outside the marked project.
func TestHookResolvesMarkerDBFromPayloadCwd(t *testing.T) {
	markerProject(t) // process cwd: an UNMARKED temp dir, env+XDG guarded

	// The marked project lives elsewhere; its marker DB holds the board,
	// the project registration, and one fresh post for the digest.
	project := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	if err := store.WriteMarker(project, dbPath); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b, err := s.EnsureBoard("markerhook", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	if err := s.RegisterProject(project, b.ID); err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	a, err := s.RegisterAgent("claude", "hook-sess", "")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := s.CreatePost(b.ID, a.Handle, "payload cwd wins", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if sqlDB, err := s.DB.DB(); err == nil {
		sqlDB.Close()
	}

	// Feed the hook payload on stdin, the way a provider invokes it.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		r.Close()
	})
	payload, err := json.Marshal(map[string]string{"cwd": project, "session_id": "hook-sess"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	w.Write(payload)
	w.Close()

	out := runXfa(t, "hook", "session-start")
	if !strings.Contains(out, "b/markerhook") {
		t.Fatalf("hook must resolve the DB from the payload cwd (marker DB), got %q", out)
	}
}

// ...but the hook path stays fail-OPEN: a corrupt marker must never block or
// break an agent session — no output, exit 0.
func TestHookFailsOpenOnCorruptMarker(t *testing.T) {
	project, _ := markerProject(t)
	if err := os.WriteFile(filepath.Join(project, store.MarkerName), []byte(`{broken`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	for _, event := range []string{"session-start", "stop", "subagent-stop", "user-prompt"} {
		out := runXfa(t, "hook", event) // runXfa fatals if the command errors
		if out != "" {
			t.Fatalf("hook %s must emit nothing on store-open failure, got %q", event, out)
		}
	}
}

// A bare `xfa init` pins the project LOCALLY: it creates .xfa/ with a
// .gitignore (so the SQLite file is never committed), registers into
// .xfa/board.db, writes NO .xfa.json marker, and never touches the global DB.
func TestInitDefaultCreatesLocalDir(t *testing.T) {
	project, globalDB := localProject(t)

	out := runXfa(t, "init", "--board", "localtest", "--db", "", "--provider", "claude")
	if !strings.Contains(out, "created "+store.LocalDirName+"/") {
		t.Fatalf("init output must announce the created dir, got %q", out)
	}

	localDB := store.LocalDBPath(project)
	if _, err := os.Stat(localDB); err != nil {
		t.Fatalf("bare init must create %s: %v", localDB, err)
	}
	gi := filepath.Join(project, store.LocalDirName, ".gitignore")
	raw, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("bare init must write %s: %v", gi, err)
	}
	if string(raw) != "*\n" {
		t.Fatalf("%s = %q, want %q", gi, string(raw), "*\n")
	}
	if _, err := os.Stat(filepath.Join(project, store.MarkerName)); !os.IsNotExist(err) {
		t.Fatalf("bare init must not write a marker, stat err = %v", err)
	}
	if !boardExists(t, localDB, "localtest") {
		t.Fatal("board must be registered in the local DB")
	}
	if !projectRegistered(t, localDB, project) {
		t.Fatal("project must be registered in the local DB")
	}
	if _, err := os.Stat(globalDB); !os.IsNotExist(err) {
		t.Fatalf("global DB must not be touched, stat err = %v", err)
	}
}

// Re-running a bare init in an already-local project reuses the existing
// .xfa/ database instead of creating anything new.
func TestReinitReusesExistingLocalDir(t *testing.T) {
	project, _ := localProject(t)

	runXfa(t, "init", "--board", "localtest", "--db", "", "--provider", "claude")
	localDB := store.LocalDBPath(project)
	before := boardCount(t, localDB)

	out := runXfa(t, "init", "--board", "localtest", "--db", "", "--provider", "claude")
	if !strings.Contains(out, "using database "+localDB) {
		t.Fatalf("re-init must report the existing database, got %q", out)
	}
	if strings.Contains(out, "created "+store.LocalDirName+"/") {
		t.Fatalf("re-init must not claim to create the dir, got %q", out)
	}
	if after := boardCount(t, localDB); after != before {
		t.Fatalf("board count changed across re-init: %d -> %d", before, after)
	}
}

// --global opts back into the old behavior: the XDG global database, no .xfa/.
func TestInitGlobalUsesGlobalDB(t *testing.T) {
	project, globalDB := localProject(t)

	runXfa(t, "init", "--board", "globaltest", "--db", "", "--global", "--provider", "claude")

	if _, err := os.Stat(filepath.Join(project, store.LocalDirName)); !os.IsNotExist(err) {
		t.Fatalf("--global must not create %s, stat err = %v", store.LocalDirName, err)
	}
	if !boardExists(t, globalDB, "globaltest") {
		t.Fatal("--global must register in the global DB")
	}
}

// --global is refused when something already pins the project locally: the
// user must remove the pin first, and nothing is registered meanwhile.
func TestInitGlobalRefusedWhenLocalPinned(t *testing.T) {
	project, globalDB := localProject(t)
	local := filepath.Join(project, store.LocalDirName)
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := runXfaErr(t, "init", "--board", "globaltest", "--db", "", "--global", "--provider", "claude")
	if err == nil {
		t.Fatal("--global over a local pin must be refused")
	}
	if !strings.Contains(err.Error(), store.LocalDBPath(project)) {
		t.Fatalf("error must name the resolved path, got: %v", err)
	}
	if _, statErr := os.Stat(globalDB); !os.IsNotExist(statErr) {
		t.Fatalf("refused --global must register nothing, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(store.LocalDBPath(project)); !os.IsNotExist(statErr) {
		t.Fatalf("refused --global must register nothing locally, stat err = %v", statErr)
	}
}

// --global over a project pinned via a .xfa.json marker (rather than a .xfa/
// dir) is refused the same way: the error names the resolved path and nothing
// gets registered anywhere.
func TestInitGlobalRefusedWhenMarkerPinned(t *testing.T) {
	project, globalDB := localProject(t)
	pinned := filepath.Join(t.TempDir(), "pinned.db")
	if err := store.WriteMarker(project, pinned); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	_, err := runXfaErr(t, "init", "--board", "globaltest", "--db", "", "--global", "--provider", "claude")
	if err == nil {
		t.Fatal("--global over a marker pin must be refused")
	}
	if !strings.Contains(err.Error(), pinned) {
		t.Fatalf("error must name the resolved path, got: %v", err)
	}
	if _, statErr := os.Stat(globalDB); !os.IsNotExist(statErr) {
		t.Fatalf("refused --global must register nothing, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(pinned); !os.IsNotExist(statErr) {
		t.Fatalf("refused --global must register nothing at the marker's db, stat err = %v", statErr)
	}
}

// --global and --db name two different databases; asking for both is an error
// raised before any file or DB is touched.
func TestInitGlobalAndDBMutuallyExclusive(t *testing.T) {
	project, globalDB := localProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	_, err := runXfaErr(t, "init", "--board", "globaltest", "--db", dbPath, "--global", "--provider", "claude")
	if err == nil {
		t.Fatal("--global with --db must be rejected")
	}
	if !strings.Contains(err.Error(), "--global") || !strings.Contains(err.Error(), "--db") {
		t.Fatalf("error must name both flags, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, store.MarkerName)); !os.IsNotExist(statErr) {
		t.Fatal("rejected init must not write a marker")
	}
	for _, p := range []string{dbPath, globalDB, filepath.Join(project, store.LocalDirName)} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Fatalf("rejected init must leave nothing at %s", p)
		}
	}
}

// --db is unchanged by the local default: it still writes the marker, still
// registers in the custom DB, and creates no .xfa/ directory.
func TestInitDBStillWritesMarker(t *testing.T) {
	project, globalDB := localProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	runXfa(t, "init", "--board", "markertest", "--db", dbPath, "--provider", "claude")

	if got, want := readMarker(t, project), `{"db":"`+dbPath+`"}`+"\n"; got != want {
		t.Fatalf("marker = %q, want %q", got, want)
	}
	if !boardExists(t, dbPath, "markertest") {
		t.Fatal("board must be registered in the custom DB")
	}
	if _, err := os.Stat(filepath.Join(project, store.LocalDirName)); !os.IsNotExist(err) {
		t.Fatalf("--db must not create %s, stat err = %v", store.LocalDirName, err)
	}
	if _, err := os.Stat(globalDB); !os.IsNotExist(err) {
		t.Fatalf("global DB must not be touched, stat err = %v", err)
	}
}

// XFA_DB already pins every command in this environment, so a bare init uses
// it as-is rather than forking a local database beside it.
func TestInitRespectsXfaDBEnv(t *testing.T) {
	project, globalDB := localProject(t)
	envDB := filepath.Join(t.TempDir(), "env.db")
	t.Setenv("XFA_DB", envDB)

	out := runXfa(t, "init", "--board", "envtest", "--db", "", "--provider", "claude")
	if !strings.Contains(out, "using database "+envDB) {
		t.Fatalf("init must report the env database, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, store.LocalDirName)); !os.IsNotExist(err) {
		t.Fatalf("XFA_DB must suppress %s creation, stat err = %v", store.LocalDirName, err)
	}
	if !boardExists(t, envDB, "envtest") {
		t.Fatal("board must be registered in the env DB")
	}
	if _, err := os.Stat(globalDB); !os.IsNotExist(err) {
		t.Fatalf("global DB must not be touched, stat err = %v", err)
	}
}

// --global is likewise refused when XFA_DB pins the environment: DefaultPath
// honors XFA_DB too, so the guard cannot rely on a path comparison alone.
func TestInitGlobalRefusedWhenXfaDBSet(t *testing.T) {
	project, _ := localProject(t)
	envDB := filepath.Join(t.TempDir(), "env.db")
	t.Setenv("XFA_DB", envDB)

	_, err := runXfaErr(t, "init", "--board", "globaltest", "--db", "", "--global", "--provider", "claude")
	if err == nil {
		t.Fatal("--global under XFA_DB must be refused")
	}
	if !strings.Contains(err.Error(), envDB) {
		t.Fatalf("error must name the resolved path, got: %v", err)
	}
	if _, statErr := os.Stat(envDB); !os.IsNotExist(statErr) {
		t.Fatalf("refused --global must register nothing, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(project, store.LocalDirName)); !os.IsNotExist(statErr) {
		t.Fatal("refused --global must create no local dir")
	}
}

// Going local in a project that was previously registered in the global DB
// forks its board history; init proceeds but says so.
func TestInitLocalWarnsAboutPreviousGlobalRegistration(t *testing.T) {
	project, globalDB := localProject(t)

	// Pre-register this very directory in the global database.
	s, err := store.Open(globalDB)
	if err != nil {
		t.Fatalf("open global: %v", err)
	}
	b, err := s.EnsureBoard("oldglobal", "")
	if err != nil {
		t.Fatalf("EnsureBoard: %v", err)
	}
	if err := s.RegisterProject(project, b.ID); err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	if sqlDB, err := s.DB.DB(); err == nil {
		sqlDB.Close()
	}

	out := runXfa(t, "init", "--board", "localtest", "--db", "", "--provider", "claude")
	if !strings.Contains(out, "previously registered in the global database") {
		t.Fatalf("init must warn about the previous global registration, got %q", out)
	}
	if !strings.Contains(out, globalDB) {
		t.Fatalf("warning must name the global DB path, got %q", out)
	}
	if !boardExists(t, store.LocalDBPath(project), "localtest") {
		t.Fatal("init must still proceed locally")
	}
}

// A project with no global registration gets no warning (and an absent global
// DB is never opened — opening it would create it).
func TestInitLocalQuietWithoutPreviousGlobalRegistration(t *testing.T) {
	project, globalDB := localProject(t)

	out := runXfa(t, "init", "--board", "localtest", "--db", "", "--provider", "claude")
	if strings.Contains(out, "previously registered") {
		t.Fatalf("no previous registration, but got %q", out)
	}
	if _, err := os.Stat(globalDB); !os.IsNotExist(err) {
		t.Fatalf("init must not create the global DB just to check it, stat err = %v", err)
	}
	if !boardExists(t, store.LocalDBPath(project), "localtest") {
		t.Fatal("init must register locally")
	}
}
