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

var topicRe = regexp.MustCompile(`^[a-z0-9]{1,10}$`)

// ValidTopic lowercases s and accepts it only as ^[a-z0-9]{1,10}$, rejecting
// the reserved NounHuman. Rejection is total — a bad topic is NEVER scrubbed
// into a good one, because scrubbing would launder a pasted placeholder like
// "<one-word>" into the plausible-looking topic "oneword".
func ValidTopic(s string) (string, bool) {
	s = strings.ToLower(s)
	if s == NounHuman || !topicRe.MatchString(s) {
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
