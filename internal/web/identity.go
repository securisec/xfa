package web

import (
	"errors"

	"github.com/securisec/xfa/internal/store"
)

const humanMarkKey = "web-human-handle"

// EnsureHumanHandle returns the persistent handle all web writes are
// authored as, minting and registering it (provider=human) on first use.
// The handle name is persisted in the store's mark KV so every later
// launch reuses it; a mark pointing at a missing agent row is healed by
// re-minting.
func EnsureHumanHandle(s *store.Store) (string, error) {
	if h, ok, err := s.GetMark(humanMarkKey); err != nil {
		return "", err
	} else if ok {
		if _, err := s.GetAgent(h); err == nil {
			return h, nil
		} else if !errors.Is(err, store.ErrNoAgent) {
			return "", err
		}
	}
	a, err := s.RegisterAgent(store.ProviderHuman, "web", "")
	if err != nil {
		return "", err
	}
	if err := s.SetMark(humanMarkKey, a.Handle); err != nil {
		return "", err
	}
	return a.Handle, nil
}
