/*
===========================================================================

starambush.go - a fixed ambush per caravan star (port-only, not native)

Natively an ambush is the star rating plus one on half the draws, doubled
on 2% (the server's bandit spawn count, SpawnCount). The owner
approved the balance advisor's fixed table as a server setting
(2026-10-15): 1 star 2 bandits, 2 stars 3, 3 stars 4, 4 stars 7, 5 stars
10, with no extra draw and no doubling. The table replaces the shard's
bandit cap (banditcap.go) while it is on.

SRO_CARAVAN_STAR_AMBUSH "on" selects the table; unset or "off" is native.
The native count is still rolled first (action/caravan.go), so its two
draws, and every bandit's own draws after them, fall where natively.

===========================================================================
*/
package caravan

import (
	"os"
	"strings"
)

// EnvStarAmbush turns the fixed per-star ambush on ("on", "1", "true").
const EnvStarAmbush = "SRO_CARAVAN_STAR_AMBUSH"

// starAmbushCounts is the advisor's table, indexed by stars 1..5.
var starAmbushCounts = [...]int{1: 2, 2: 3, 3: 4, 4: 7, 5: 10}

/*
================
StarAmbushFromEnv
================
*/
func StarAmbushFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStarAmbush))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
StarAmbushCount

The table's bandits for a star rating. A rating of 0 counts as 1, as
the native spawn count does; above 5 counts as 5.
================
*/
func StarAmbushCount(stars uint32) int {
	switch {
	case stars < 1:
		stars = 1
	case stars >= uint32(len(starAmbushCounts)):
		stars = uint32(len(starAmbushCounts) - 1)
	}
	return starAmbushCounts[stars]
}
