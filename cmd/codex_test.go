package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/securisec/xfa/internal/install"
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

// A bare `xfa uninstall` defaults to every registered provider — the list is
// install.Names(), so a newly registered provider can never be orphaned by a
// stale hardcoded default.
func TestUninstallDefaultCoversEveryProvider(t *testing.T) {
	project, _ := markerProject(t)
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	runXfa(t, "init", "--board", "defaulttest", "--db", dbPath, "--provider", "codex")

	// The flag's registered default is the registry list.
	def := uninstallCmd.Flags().Lookup("provider").DefValue
	for _, name := range install.Names() {
		if !strings.Contains(def, name) {
			t.Fatalf("uninstall --provider default %q must include %q", def, name)
		}
	}
	out := runXfa(t, "uninstall")
	for _, name := range install.Names() {
		if !strings.Contains(out, "removed provider: "+name) {
			t.Errorf("bare uninstall must run provider %q, output:\n%s", name, out)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("bare uninstall must clean codex artifacts, stat err = %v", err)
	}
}
