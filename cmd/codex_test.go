package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/install"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

// `xfa init --provider codex` installs the codex skill + hooks through the
// registry, and `xfa uninstall --provider codex` removes them again.
func TestInitAndUninstallProviderCodex(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	out := runXfa(t, "init", "--board", "codextest", "--db", dbPath, "--provider", "codex")
	if !strings.Contains(out, "installed provider: codex") {
		t.Fatalf("init output: %q", out)
	}
	for _, p := range []string{
		filepath.Join(project, ".codex", "hooks.json"),
		filepath.Join(project, ".agents", "skills", "xfa", "SKILL.md"),
		filepath.Join(project, "AGENTS.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("init --provider codex must create %s: %v", p, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(project, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"session-start", "stop", "subagent-stop", "user-prompt"} {
		if !strings.Contains(string(raw), "hook "+sub) {
			t.Errorf("hooks.json missing %q:\n%s", sub, raw)
		}
	}

	out = runXfa(t, "uninstall", "--provider", "codex")
	if !strings.Contains(out, "removed provider: codex") {
		t.Fatalf("uninstall output: %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("uninstall --provider codex must prune .agents, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("uninstall --provider codex must drop an AGENTS.md that was ours alone, stat err = %v", err)
	}
	// hooks.json itself is never deleted, only stripped. (That the xfa entries
	// are actually removed is asserted in internal/install's codex tests: here
	// the installed exe path is the go test binary, whose name has no "xfa" in
	// it, so isXfaHookCommand rightly does not claim those entries as ours.)
	if _, err := os.Stat(filepath.Join(project, ".codex", "hooks.json")); err != nil {
		t.Fatalf("hooks.json must survive uninstall: %v", err)
	}
}

// A bare `xfa uninstall` mirrors init: it removes claude only, leaving other
// installed providers and the .xfa.json marker alone (a partial uninstall must
// not unpin the DB for providers still installed).
func TestUninstallDefaultsToClaude(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	runXfa(t, "init", "--board", "defaulttest", "--db", dbPath, "--provider", "claude,codex")

	if def := uninstallCmd.Flags().Lookup("provider").DefValue; def != "[claude]" {
		t.Fatalf("uninstall --provider default = %q, want [claude]", def)
	}
	out := runXfa(t, "uninstall")
	if out != "removed provider: claude\n" {
		t.Fatalf("bare uninstall output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "xfa")); !os.IsNotExist(err) {
		t.Fatalf("bare uninstall must remove claude artifacts, stat err = %v", err)
	}
	for _, p := range []string{
		filepath.Join(project, ".agents", "skills", "xfa", "SKILL.md"),
		filepath.Join(project, store.MarkerName),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("bare uninstall must leave %s: %v", p, err)
		}
	}
}

// `--all` removes every registered provider and drops the marker.
func TestUninstallAll(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	runXfa(t, "init", "--board", "alltest", "--db", dbPath, "--provider", "claude,codex")

	out := runXfa(t, "uninstall", "--all")
	for _, name := range install.Names() {
		if strings.Count(out, "removed provider: "+name+"\n") != 1 {
			t.Errorf("--all must run provider %q exactly once, output:\n%s", name, out)
		}
	}
	for _, p := range []string{
		filepath.Join(project, ".claude", "skills", "xfa"),
		filepath.Join(project, ".agents"),
		filepath.Join(project, store.MarkerName),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("--all must remove %s, stat err = %v", p, err)
		}
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("uninstall must keep the DB file: %v", err)
	}
}

// Naming every provider by hand removes them all but is NOT --all: the marker
// stays, since only --all means "unpin this project".
func TestUninstallEveryNameKeepsMarker(t *testing.T) {
	project, _ := markerProject(t)
	runXfa(t, "init", "--board", "namestest", "--db", filepath.Join(t.TempDir(), "custom.db"), "--provider", "claude,codex")

	out := runXfa(t, "uninstall", "--provider", strings.Join(install.Names(), ","))
	for _, name := range install.Names() {
		if !strings.Contains(out, "removed provider: "+name+"\n") {
			t.Errorf("must run provider %q, output:\n%s", name, out)
		}
	}
	if strings.Contains(out, store.MarkerName) {
		t.Errorf("explicit names must not report the marker, output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(project, store.MarkerName)); err != nil {
		t.Fatalf("explicit names must keep the marker: %v", err)
	}
}

// --all and --provider are mutually exclusive (cobra rejects before RunE), so
// nothing is removed.
func TestUninstallAllWithProviderRejected(t *testing.T) {
	project, _ := markerProject(t)
	runXfa(t, "init", "--board", "excltest", "--db", filepath.Join(t.TempDir(), "custom.db"), "--provider", "claude")

	out, err := runXfaErr(t, "uninstall", "--all", "--provider", "claude")
	if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
		t.Fatalf("--all --provider must be rejected by cobra, got err=%v out=%q", err, out)
	}
	if strings.Contains(out, "removed provider") {
		t.Fatalf("nothing may run, got %q", out)
	}
	for _, p := range []string{filepath.Join(project, ".claude", "skills", "xfa", "SKILL.md"), filepath.Join(project, store.MarkerName)} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("rejected uninstall must remove nothing, %s: %v", p, err)
		}
	}
}

// --provider completion: init and uninstall both offer exactly the registry.
func TestProviderCompletion(t *testing.T) {
	for _, c := range []*cobra.Command{initCmd, uninstallCmd} {
		fn, ok := c.GetFlagCompletionFunc("provider")
		if !ok {
			t.Fatalf("%s: no --provider completion registered", c.Name())
		}
		got, dir := fn(c, nil, "")
		if !reflect.DeepEqual(got, install.Names()) || dir != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%s completion = %v (%v), want %v", c.Name(), got, dir, install.Names())
		}
	}
}
