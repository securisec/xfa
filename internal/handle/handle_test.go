package handle

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

func TestMintFormat(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pat := regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9]{1,2}$`)
	for i := 0; i < 100; i++ {
		h := Mint(rng, "", "")
		if !pat.MatchString(h) {
			t.Fatalf("bad handle %q", h)
		}
	}
}

func TestMintSlots(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, tc := range []struct {
		topic, noun string
	}{
		{"parser", ""},
		{"", "human"},
		{"web3", "human"},
	} {
		h := Mint(rng, tc.topic, tc.noun)
		parts := strings.Split(h, "-")
		if len(parts) != 3 {
			t.Fatalf("Mint(%q,%q) = %q: want 3 parts", tc.topic, tc.noun, h)
		}
		if tc.topic != "" && parts[0] != tc.topic {
			t.Errorf("Mint(%q,%q) = %q: topic slot %q", tc.topic, tc.noun, h, parts[0])
		}
		if tc.noun != "" && parts[1] != tc.noun {
			t.Errorf("Mint(%q,%q) = %q: noun slot %q", tc.topic, tc.noun, h, parts[1])
		}
	}
}

func TestValidTopic(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"parser", "parser", true},
		{"Parser", "parser", true},
		{"WEB3", "web3", true},
		{"a", "a", true},
		{"0123456789", "0123456789", true},
		{"", "", false},
		{"01234567890", "0123456789", true},  // 11 chars: length truncates
		{"orchestrator", "orchestrat", true}, // the real incident
		{"ORCHESTRATOR", "orchestrat", true}, // lowercase before truncating
		{"humanxxxxxx", "humanxxxxx", true},  // truncation cannot land on NounHuman
		{"<one-word>", "", false},            // pasted placeholder: charset rejects, never scrubs
		{"<orchestrator>", "", false},        // long AND bad charset: still rejected
		{"orchestratör", "", false},          // non-ascii that stays non-ascii after ToLower rejects
		{"one-word", "", false},
		{"one_word", "", false},
		{"one word", "", false},
		{"human", "", false},
		{"Human", "", false},
		{"réel", "", false},
	} {
		got, ok := ValidTopic(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ValidTopic(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNoDuplicateNouns(t *testing.T) {
	seen := map[string]bool{}
	for _, n := range nouns {
		if seen[n] {
			t.Errorf("duplicate noun %q", n)
		}
		seen[n] = true
	}
	if seen[NounHuman] {
		t.Errorf("NounHuman %q must stay out of nouns: it is never minted randomly", NounHuman)
	}
	if len(nouns) < 70 {
		t.Errorf("noun list is the within-topic discriminator; got %d", len(nouns))
	}
	pat := regexp.MustCompile(`^[a-z]+$`)
	for _, n := range nouns {
		if !pat.MatchString(n) {
			t.Errorf("bad noun %q", n)
		}
	}
}
