package sessions

import "crypto/rand"

// sessionAdjectives and sessionNouns are the word pools for auto-generated
// session names. A name is one adjective + one noun joined by a hyphen
// (e.g. "brave-otter"). Words are short, lowercase, and unambiguous; the pools
// are kept roughly balanced so the combination space (len(adj)*len(noun)) is
// large enough to avoid frequent collisions while staying human-friendly.
var (
	sessionAdjectives = []string{
		"amber", "bold", "brave", "brisk", "calm", "clever", "cosmic", "crimson",
		"crisp", "dapper", "deft", "eager", "fancy", "fleet", "fuzzy", "gentle",
		"golden", "happy", "hidden", "jolly", "keen", "lively", "lucky", "lunar",
		"mellow", "merry", "mighty", "nimble", "noble", "plucky", "proud", "quick",
		"quiet", "rapid", "royal", "rusty", "sage", "shiny", "silent", "sleek",
		"snappy", "solar", "spry", "sturdy", "sunny", "swift", "tidy", "vivid",
		"witty", "zesty",
	}
	sessionNouns = []string{
		"otter", "falcon", "maple", "comet", "willow", "badger", "cedar", "heron",
		"lynx", "raven", "puffin", "marlin", "panda", "koala", "ferret", "beacon",
		"harbor", "canyon", "meadow", "summit", "ember", "pebble", "quartz", "onyx",
		"cobalt", "indigo", "violet", "ribbon", "anchor", "lantern", "compass", "kettle",
		"acorn", "thistle", "clover", "fennel", "ginger", "saffron", "walnut", "almond",
		"pixel", "cipher", "vector", "matrix", "module", "socket", "spruce", "nimbus",
		"zephyr", "tundra",
	}
)

// generateSessionName returns a random "<adjective>-<noun>" name, used as a
// session's default title when the caller supplies none. The name is cosmetic
// (the ULID id remains the key), so on the unlikely CSPRNG failure it returns
// "" and the UI falls back to the id.
func generateSessionName() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	adj := sessionAdjectives[int(b[0])%len(sessionAdjectives)]
	noun := sessionNouns[int(b[1])%len(sessionNouns)]
	return adj + "-" + noun
}
