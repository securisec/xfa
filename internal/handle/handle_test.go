package handle

import (
	"math/rand"
	"regexp"
	"testing"
)

func TestMintFormat(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pat := regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9]{1,2}$`)
	for i := 0; i < 100; i++ {
		h := Mint(rng)
		if !pat.MatchString(h) {
			t.Fatalf("bad handle %q", h)
		}
	}
}
