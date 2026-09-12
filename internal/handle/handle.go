package handle

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

// NounHuman is the reserved noun slot for the web UI's single human identity.
// It is never minted randomly (it is absent from nouns) and never accepted as
// a topic, so `crimson-human-7` cannot be spoofed by an agent.
const NounHuman = "human"

var adjectives = []string{
	"amber", "brisk", "crimson", "dapper", "electric", "feral", "gilded",
	"hollow", "iron", "jolly", "keen", "lunar", "mellow", "nimble",
	"obsidian", "plucky", "quiet", "rusty", "silver", "tidal", "umber",
	"vivid", "wry", "zesty",
}

// nouns is the within-topic discriminator: agents sharing a topic differ only
// here, so the list is sized for birthday-collision headroom (~80 keeps a
// handful of same-topic agents comfortably distinct).
var nouns = []string{
	"alpaca", "anteater", "armadillo", "axolotl", "badger", "bison",
	"bobcat", "caracal", "caribou", "cheetah", "chinchilla", "civet",
	"cobra", "coyote", "dingo", "dormouse", "echidna", "egret", "falcon",
	"ferret", "fossa", "gannet", "gazelle", "gecko", "gibbon", "gopher",
	"grouse", "hedgehog", "heron", "ibex", "ibis", "impala", "jackal",
	"jaguar", "jerboa", "kestrel", "kingfisher", "koala", "kudu",
	"lemming", "lemur", "lynx", "manatee", "marmot", "meerkat", "mongoose",
	"muskox", "narwhal", "newt", "numbat", "ocelot", "okapi", "opossum",
	"osprey", "otter", "pangolin", "panther", "pelican", "puffin",
	"quokka", "quoll", "raccoon", "raven", "reindeer", "salamander",
	"serval", "shrew", "skink", "stoat", "tamarin", "tapir", "toucan",
	"urchin", "vicuna", "vole", "walrus", "weasel", "wolverine", "wombat",
	"zebra",
}

var topicRe = regexp.MustCompile(`^[a-z0-9]+$`)

// TopicMax is the topic slot's length cap; longer valid topics are truncated.
const TopicMax = 10

// ValidTopic lowercases s, requires ^[a-z0-9]+$, truncates to TopicMax, and
// rejects the reserved NounHuman. The two checks are deliberately asymmetric:
//
//   - Charset is a hard reject, never a scrub: stripping bad bytes would
//     launder a pasted placeholder like "<one-word>" into the plausible topic
//     "oneword", and a laundered handle is worse than a random one.
//   - Length truncates ("orchestrator" → "orchestrat"): an over-long word is
//     a real topic the agent chose, and rejecting it silently degraded a whole
//     subagent tree to a random adjective. Truncation cannot weaken the
//     charset property — a placeholder still fails ^[a-z0-9]+$ before any
//     cut, and a cut never introduces or removes a bad byte.
//
// NounHuman is compared after the cut so the returned value, the one that
// gets minted, is the one checked (a 10-char cut cannot equal a 5-char noun
// anyway, but comparing the pre-cut input is the form that could rot).
func ValidTopic(s string) (string, bool) {
	s = strings.ToLower(s)
	if !topicRe.MatchString(s) {
		return "", false
	}
	if len(s) > TopicMax { // ASCII-only past the regex, so bytes are runes
		s = s[:TopicMax]
	}
	if s == NounHuman {
		return "", false
	}
	return s, true
}

// Mint generates a topic-noun-N handle; an empty topic or noun is drawn at
// random (adjective and animal respectively), which is the pre-topic shape.
// The 1-99 numeric suffix is load-bearing: store's mentionRe
// (internal/store/posts.go) matches exactly 1-2 digits, so widening this range
// without widening that regex makes the new handles silently unmentionable.
// The topic slot is likewise alphanumeric-only to stay inside that regex's
// first group.
func Mint(rng *rand.Rand, topic, noun string) string {
	if topic == "" {
		topic = adjectives[rng.Intn(len(adjectives))]
	}
	if noun == "" {
		noun = nouns[rng.Intn(len(nouns))]
	}
	return fmt.Sprintf("%s-%s-%d", topic, noun, 1+rng.Intn(99))
}
