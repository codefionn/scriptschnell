package session

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

var adjectives = []string{
	"amber", "azure", "bold", "brave", "bright",
	"calm", "clear", "cold", "cool", "coral",
	"crisp", "dark", "dawn", "deep", "dry",
	"dusk", "early", "east", "fair", "fast",
	"firm", "flat", "fresh", "frost", "full",
	"glad", "gold", "grand", "gray", "green",
	"half", "high", "hot", "icy", "iron",
	"keen", "kind", "late", "lean", "light",
	"lime", "live", "long", "low", "mild",
	"mint", "mist", "near", "neat", "next",
	"north", "odd", "old", "opal", "open",
	"pale", "peak", "pine", "pink", "plain",
	"prime", "pure", "quiet", "rare", "raw",
	"red", "rich", "ripe", "rose", "ruby",
	"rust", "safe", "sage", "silk", "slim",
	"slow", "soft", "south", "stark", "steel",
	"still", "stone", "sun", "swift", "tall",
	"teal", "thin", "true", "warm", "west",
	"white", "wide", "wild", "wise", "young",
}

var nouns = []string{
	"arch", "ash", "bay", "beam", "birch",
	"bloom", "bolt", "brook", "cape", "cave",
	"cedar", "cliff", "cloud", "coast", "cove",
	"crane", "creek", "crest", "crow", "dale",
	"dart", "dawn", "delta", "dove", "drift",
	"dune", "dust", "elm", "ember", "fern",
	"field", "finch", "flame", "flint", "fog",
	"forge", "fox", "frost", "gale", "gate",
	"glen", "grove", "hare", "hawk", "heath",
	"hill", "hive", "hollow", "ivy", "jade",
	"jay", "lake", "lark", "leaf", "ledge",
	"lily", "loft", "maple", "marsh", "mesa",
	"mist", "moss", "moth", "oak", "orbit",
	"otter", "owl", "palm", "path", "peak",
	"pine", "plum", "pond", "quail", "rain",
	"raven", "reef", "ridge", "river", "rock",
	"sage", "shore", "sky", "slope", "snow",
	"spark", "spruce", "star", "stone", "storm",
	"stream", "swift", "thorn", "tide", "trail",
	"vale", "vine", "wave", "willow", "wolf",
}

// GenerateWordID generates a human-friendly ID in the form "adjective-adjective-noun".
func GenerateWordID() string {
	adj1 := pickRandom(adjectives)
	adj2 := pickRandom(adjectives)
	noun := pickRandom(nouns)
	return fmt.Sprintf("%s-%s-%s", adj1, adj2, noun)
}

func pickRandom(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		// Fallback: just use first element (should never happen)
		return list[0]
	}
	return list[n.Int64()]
}
