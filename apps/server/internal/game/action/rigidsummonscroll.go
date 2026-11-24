/*
===========================================================================

rigidsummonscroll.go - the Rigid L2 unique summon scrolls (#3785)

Later-native content, on with the later game: the later clients
widened the unique ladder with down-scaled "Rigid" copies of the
field uniques (MOB_<X>_L2) and a summon scroll per copy. The v1.150
client and server have neither, but the v1.188 server's use path
serves the scrolls, so the port does too, with no switch (owner
2026-11-24). The scroll items and the four kinds the v1.150 tables
lack are staged by the build (scripts/rigid_import.py).

The native rules, from the v1.188 server's expendable-item use
handler (traced 2026-11-23):

  - The monster is the item's first parameter description: the
    handler copies it, rejects the "xxx" empty sentinel, resolves
    it as a monster codename and spawns that monster at the user
    through the world spawn factory, on a ring of 0 to 70 units.
    The port's rows carry the codename in the same place; the table
    below mirrors the eight staged rows (rigid_import.py's
    SCROLLS), as the mall scrolls carry their counts in code.
  - The family cooltime rides the item rows' COOLTIME parameter:
    the descriptions carry the category word 0xF1 (241) and the
    paired parameter value is the delay - 600000 ms on every scroll
    row. Where the keyword is parsed into the timer does not live
    in the game-server binary (a census of it finds no such string;
    its home is more likely the DB/tooling side of the row
    pipeline), so the port mirrors the constant, as the mall
    scrolls mirror theirs. What the use handler consults is the
    per-user timer itself; the port keeps it on the Runtime,
    session-scoped like the native timer.
  - The monster stays until it is killed: the handler asks the
    factory for no lifetime. A rarity-3 kind announces itself
    through the ordinary unique appearance notice.

Refusals spend nothing: the summon event's town and arena refusals
(summonEventRefusal, shared with the party scroll), and the family
cooltime answering with the native reuse delay. The items are
unstocked in the port (their ISRO-R Battle Arena store has no
v1.150 counterpart), so a scroll reaches a bag only through GM or
event distribution.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// rigidSummonCooldownMs is the family cooltime: 600 s, the item
// rows' COOLTIME parameter value, shared by all eight scrolls.
const rigidSummonCooldownMs = 600_000

// The native spawn ring at the user: 0 to 70 units.
const (
	rigidSummonRadiusMin  = 0
	rigidSummonRadiusSpan = 70
)

// rigidSummonScrollMonsters names the monster each scroll summons:
// the staged item rows' first parameter descriptions, mirrored
// (scripts/data/rigid-scroll-rows.json).
var rigidSummonScrollMonsters = map[string]string{
	"ITEM_RIGID_TIGERWOMAN_L2_SUMMON_SCROLL": "MOB_CH_TIGERWOMAN_L2",
	"ITEM_RIGID_KERBERUS_L2_SUMMON_SCROLL":   "MOB_EU_KERBEROS_L2",
	"ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL":     "MOB_OA_URUCHI_L2",
	"ITEM_RIGID_IVY_L2_SUMMON_SCROLL":        "MOB_AM_IVY_L2",
	"ITEM_RIGID_ISYUTARU_L2_SUMMON_SCROLL":   "MOB_KK_ISYUTARU_L2",
	"ITEM_RIGID_BONELORD_L2_SUMMON_SCROLL":   "MOB_TK_BONELORD_L2",
	"ITEM_RIGID_TAHOMET_L2_SUMMON_SCROLL":    "MOB_RM_TAHOMET_L2",
	"ITEM_RIGID_BLACKSNAKE_L2_SUMMON_SCROLL": "MOB_TQ_BLACKSNAKE_L2",
}

/*
================
RigidSummonScrollMonster

The monster the named scroll summons; false for any other item.
================
*/
func RigidSummonScrollMonster(codename string) (string, bool) {
	monster, ok := rigidSummonScrollMonsters[codename]
	return monster, ok
}

/*
================
Runtime.useRigidSummonScroll

One rigid unique beside the user, persistent until killed, and the
scroll spent; the family cooltime then holds every scroll for the
user for 600 s. Native refusals (town, arena) and the cooltime
spend nothing.
================
*/
func (rt *Runtime) useRigidSummonScroll(use persistentSummonUse, result *OpResult) bool {
	c := use.character
	monster, ok := RigidSummonScrollMonster(use.ref.Codename)
	if !ok {
		result.DiagnosticRefusal = "item-use: not a rigid summon scroll"
		return false
	}
	if rt.Monsters == nil || !enterworld.CharacterAlive(c) || c.Level == nil {
		result.DiagnosticRefusal = "item-use: rigid summon scroll owner unavailable"
		return false
	}
	if _, ok := rt.summonEventRefusal(use, 0, "", result); !ok {
		return false
	}
	if rt.rigidScrollReady == nil {
		rt.rigidScrollReady = make(map[string]int64)
	}
	if rt.rigidScrollReady[c.Name] > use.nowMs {
		*result = itemUseFailure(wire.ErrCodeItemReuseDelay)
		return false
	}
	landed := rt.SpawnQuestMonster(use.division, c, simulation.QuestMonsterSpawn{
		Codename: monster, RadiusMin: rigidSummonRadiusMin, RadiusSpan: rigidSummonRadiusSpan,
		AnnounceUnique: true,
	})
	remaining := rt.consumeItemUseRow(c, use.row)
	rt.rigidScrollReady[c.Name] = use.nowMs + rigidSummonCooldownMs
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	if !landed {
		result.Frames = append(result.Frames, exchangeBanner(partySummonFailedLine))
	}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
