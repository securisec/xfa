package install

// Provider installs and uninstalls xfa's integration (hooks, skill, awareness
// block) for one agent provider. Implementations are thin wrappers over the
// per-provider install logic; the registry gives cmd a single place to
// validate --provider values and dispatch.
type Provider interface {
	Name() string
	Install(projectDir, exePath string) error
	Uninstall(projectDir string) error
}

type claudeProvider struct{}

func (claudeProvider) Name() string { return "claude" }
func (claudeProvider) Install(projectDir, exePath string) error {
	return InstallClaude(projectDir, exePath)
}
func (claudeProvider) Uninstall(projectDir string) error { return UninstallClaude(projectDir) }

type opencodeProvider struct{}

func (opencodeProvider) Name() string { return "opencode" }
func (opencodeProvider) Install(projectDir, exePath string) error {
	return InstallOpencode(projectDir, exePath)
}
func (opencodeProvider) Uninstall(projectDir string) error { return UninstallOpencode(projectDir) }

type piProvider struct{}

func (piProvider) Name() string { return "pi" }
func (piProvider) Install(projectDir, exePath string) error {
	return InstallPi(projectDir, exePath)
}
func (piProvider) Uninstall(projectDir string) error { return UninstallPi(projectDir) }

type codexProvider struct{}

func (codexProvider) Name() string { return "codex" }
func (codexProvider) Install(projectDir, exePath string) error {
	return InstallCodex(projectDir, exePath)
}
func (codexProvider) Uninstall(projectDir string) error { return UninstallCodex(projectDir) }

type geminiProvider struct{}

func (geminiProvider) Name() string { return "gemini" }
func (geminiProvider) Install(projectDir, exePath string) error {
	return InstallGemini(projectDir, exePath)
}
func (geminiProvider) Uninstall(projectDir string) error { return UninstallGemini(projectDir) }

type antigravityProvider struct{}

func (antigravityProvider) Name() string { return "antigravity" }
func (antigravityProvider) Install(projectDir, exePath string) error {
	return InstallAntigravity(projectDir, exePath)
}
func (antigravityProvider) Uninstall(projectDir string) error {
	return UninstallAntigravity(projectDir)
}

// registry holds every supported provider in deterministic order.
var registry = []Provider{claudeProvider{}, opencodeProvider{}, piProvider{}, codexProvider{}, geminiProvider{}, antigravityProvider{}}

// Providers returns the registered providers in deterministic order.
func Providers() []Provider { return append([]Provider(nil), registry...) }

// Get returns the provider registered under name.
func Get(name string) (Provider, bool) {
	for _, p := range registry {
		if p.Name() == name {
			return p, true
		}
	}
	return nil, false
}

// Names returns the registered provider names in registry order (for
// validation error messages and help text).
func Names() []string {
	names := make([]string, len(registry))
	for i, p := range registry {
		names[i] = p.Name()
	}
	return names
}
