package skill

import _ "embed"

//go:embed SKILL.md
var Content string

// Version is stamped at build time from the git tag via
// -ldflags "-X github.com/securisec/xfa/internal/skill.Version=..." (see
// mise.toml and .github/workflows/ci.yml). "dev" means an unstamped local build.
var Version = "dev"
