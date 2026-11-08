/*
===========================================================================

sundrops.go - counting Sun drops for one measurement cycle (#2538)

Port-only, not native, and count-only: nothing here changes a drop. The
decision record on #2538 restores a unique's first native round outside
the beta drop cap and defers every Sun question until one counting-only
cycle has measured how many Suns the shard actually pays and who
receives them. This hook is that measurement: every Seal of Sun item in
committed monster loot is counted once for the loot-winning
representative, with the item named - the weekly seal board
(sealboard.go) counts tiers per character but never names the item.

What never counts: the same settlements the seal board excludes (the
GM kill path settles with counting off, and a GM-spawned monster's loot
is conjured, not hunted), quest-supply drops (they join after this
hook), manual drops, trades and alchemy outcomes.

===========================================================================
*/
package action

import (
	"os"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
)

// EnvSunDropCount is the counting cycle's switch; off counts nothing.
const EnvSunDropCount = "SRO_SUN_DROP_COUNT"

/*
================
SunDropSink

Where counted Sun drops go. The composition root installs the counter
only while the counting cycle runs; nil (the default) counts nothing.
================
*/
type SunDropSink interface {
	NoteSunDrop(characterID int64, codename string, at time.Time)
}

/*
================
SunDropCountFromEnv

Reads EnvSunDropCount ("on", "1", "true"). Off is the native default:
nothing counts, exactly as with no hook installed.
================
*/
func SunDropCountFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSunDropCount))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
countSunDrops

Counts the Seal of Sun items in one committed monster-loot settlement
for its loot-winning representative (a pet or COS kill counts for the
owner the settlement chose). A nil sink, winner or item source counts
nothing. Count-only: the drops are read, never changed.
================
*/
func (rt *Runtime) countSunDrops(winner *enterworld.Character, drops []grounditem.Item, nowMs int64) {
	if rt == nil || rt.SunDrops == nil || winner == nil || winner.ID <= 0 {
		return
	}
	items := rt.deps.ItemReferences()
	if items == nil {
		return
	}
	at := time.UnixMilli(nowMs).UTC()
	for _, drop := range drops {
		ref, found := items.ItemRefByCodename(drop.Codename)
		if !found || ref == nil {
			continue
		}
		if tier, sealed := enterworld.EquipmentSeal(ref); sealed && tier == enterworld.SealSun {
			rt.SunDrops.NoteSunDrop(winner.ID, drop.Codename, at)
		}
	}
}
