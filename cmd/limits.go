package cmd

// clampLimit maps a non-positive --limit onto the command's own default
// (each command has its own: read wants defaultReadLimit, threads wants
// defaultThreadsLimit). The store layer clamps internally too, but commands
// that slice results by limit themselves need the clamped value.
func clampLimit(limit, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	return limit
}
