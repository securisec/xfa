package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/securisec/xfa/internal/skill"
)

// installSkillFiles writes SKILL.md + the .xfa_version stamp into sdir with
// the shared downgrade guard: a strictly newer on-disk stamp warns and skips
// instead of silently overwriting.
func installSkillFiles(sdir string) error {
	verFile := filepath.Join(sdir, ".xfa_version")
	if old, err := os.ReadFile(verFile); err == nil && versionNewer(strings.TrimSpace(string(old)), skill.Version) {
		fmt.Fprintf(os.Stderr, "warning: installed skill (%s) is newer than this xfa (%s); upgrade xfa instead of re-running init. Skipping skill copy.\n", strings.TrimSpace(string(old)), skill.Version)
		return nil
	}
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		return err
	}
	if err := writeFileIfChanged(filepath.Join(sdir, "SKILL.md"), []byte(skill.Content), 0o644); err != nil {
		return err
	}
	return writeFileIfChanged(verFile, []byte(skill.Version), 0o644)
}
