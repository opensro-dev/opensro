/*
===========================================================================

sealboard.go - counting seal drops for the weekly board (#1641, #1604 P9)

Port-only, not native. Every sealed equipment item that committed monster
loot pays is counted once for the loot-winning representative, classified
by enterworld.EquipmentSeal (Star, Moon, Sun at every degree - the notice
floor does not apply here), with its degree and, for a seal pity forced
drop (sealpity.go, #1690), a forced flag. The count is independent of the
seal-drop notice flag and its per-minute cap: the board reads the
settlement, not the announcement.

What never counts: manual drops, trades, alchemy outcomes, quest-supply
rewards (appended after this hook in settleMonsterInsideDoor), the GM kill
path (settles with counting off) and any GM-spawned monster, however it
died, because a conjured monster's loot is not hunted.

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

// EnvSealBoard is mirrored by platform/sealboard for the service switch;
// the constant lives here with the other action flags.
const EnvSealBoard = "SRO_SEAL_BOARD"

/*
================
SealDropSink

Where counted seal drops go. The composition root installs the
platform/sealboard service; nil (the default) counts nothing.
================
*/
type SealDropSink interface {
	NoteSealDrop(characterID int64, degree, tier int, forced bool, at time.Time)
}

/*
================
SealBoardFromEnv

Reads EnvSealBoard ("on", "1", "true"). Off is the native default.
================
*/
func SealBoardFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSealBoard))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
countSealDrops

Counts the sealed equipment in one committed monster-loot settlement for
its loot-winning representative (a pet or COS kill counts for the owner
the settlement chose). A nil sink, winner or item source counts nothing.
================
*/
func (rt *Runtime) countSealDrops(winner *enterworld.Character, drops []grounditem.Item, nowMs int64) {
	if rt == nil || rt.SealDrops == nil || winner == nil || winner.ID <= 0 {
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
		if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
			rt.SealDrops.NoteSealDrop(winner.ID, enterworld.EquipmentDegree(ref), tier, drop.SealPityForced, at)
		}
	}
}
