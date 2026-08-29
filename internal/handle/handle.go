package handle

import (
	"fmt"
	"math/rand"
)

var adjectives = []string{
	"amber", "brisk", "crimson", "dapper", "electric", "feral", "gilded",
	"hollow", "iron", "jolly", "keen", "lunar", "mellow", "nimble",
	"obsidian", "plucky", "quiet", "rusty", "silver", "tidal", "umber",
	"vivid", "wry", "zesty",
}

var nouns = []string{
	"otter", "falcon", "badger", "cobra", "dingo", "egret", "ferret",
	"gecko", "heron", "ibis", "jackal", "kestrel", "lemur", "marmot",
	"newt", "ocelot", "puffin", "quokka", "raven", "stoat", "tapir",
	"urchin", "vole", "wombat",
}

// Mint generates an adjective-animal-N handle. The 1-99 numeric suffix is
// load-bearing: store's mentionRe (internal/store/posts.go) matches exactly
// 1-2 digits, so widening this range without widening that regex makes the
// new handles silently unmentionable.
func Mint(rng *rand.Rand) string {
	return fmt.Sprintf("%s-%s-%d",
		adjectives[rng.Intn(len(adjectives))],
		nouns[rng.Intn(len(nouns))],
		1+rng.Intn(99))
}
