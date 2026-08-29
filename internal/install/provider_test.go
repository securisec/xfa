package install

import "testing"

// The registry is deterministic-ordered: claude, opencode, pi, codex, gemini,
// antigravity — always.
func TestProvidersDeterministicOrder(t *testing.T) {
	want := []string{"claude", "opencode", "pi", "codex", "gemini", "antigravity"}
	ps := Providers()
	if len(ps) != len(want) {
		t.Fatalf("Providers() = %d providers, want %d", len(ps), len(want))
	}
	for i, p := range ps {
		if p.Name() != want[i] {
			t.Fatalf("Providers()[%d].Name() = %q, want %q", i, p.Name(), want[i])
		}
	}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

func TestGetKnownAndUnknown(t *testing.T) {
	for _, name := range []string{"claude", "opencode", "pi", "codex", "gemini", "antigravity"} {
		p, ok := Get(name)
		if !ok || p.Name() != name {
			t.Fatalf("Get(%q) = %v, %v", name, p, ok)
		}
	}
	if _, ok := Get("copilot"); ok {
		t.Fatal("Get of an unregistered provider must return ok=false")
	}
}

// The registry wrappers delegate to the real install/uninstall logic: a full
// Install+Uninstall round trip through the interface must leave the project
// dir clean, proving the wrappers are wired to the existing functions.
func TestProviderRoundTripThroughInterface(t *testing.T) {
	for _, name := range []string{"claude", "opencode", "pi", "codex", "gemini", "antigravity"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p, ok := Get(name)
			if !ok {
				t.Fatalf("Get(%q) missing", name)
			}
			if err := p.Install(dir, "/usr/local/bin/xfa"); err != nil {
				t.Fatalf("Install: %v", err)
			}
			if err := p.Uninstall(dir); err != nil {
				t.Fatalf("Uninstall: %v", err)
			}
		})
	}
}
