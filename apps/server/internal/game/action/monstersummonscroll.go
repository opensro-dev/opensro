/*
===========================================================================

monstersummonscroll.go - the monster summon scrolls: one monster beside
the user (#3785, #4530)

The v1.188 server's expendable-item use handler serves every scroll that
names a monster in its first parameter on one path, so the port does too:

  - the Rigid L2 unique scrolls (later-native content, on with the later
    game, owner 2026-11-24): down-scaled "Rigid" copies of the field
    uniques (MOB_<X>_L2), staged by the build (scripts/rigid_import.py);
  - the anti-bot stone ITEM_ETC_AUTOMOB (native v1.150 data, 3/3/3/2):
    it calls MOB_AUTOMOB, which every viewer's client draws as the last
    COS it targeted and removes without a fade (the client half lives in
    the entity journal). No v1.150 shop, package or drop hands it out,
    so it reaches a bag only through GM or event distribution.

The native rules, from that handler (traced 2026-11-23 and 2026-11-26):

  - The monster is the item's first parameter description. The handler
    copies it, rejects the "xxx" empty sentinel, resolves it as a monster
    codename and spawns that monster at the user through the world spawn
    factory, on a ring of 0 to 70 units, with no lifetime: the monster
    stays until it is killed. The table below mirrors the shipped rows,
    as the mall scrolls carry their counts in code.
  - In a dungeon region only ITEM_ETC_AUTOMOB may be used; every other
    scroll answers 0x1879, whose low byte is the client's
    UIIT_MSG_COS_CREATE_DUNGEON. The port asks the plane owner
    (simulation.FeaturesAt(region).MonsterSummonScrolls) instead of
    testing the region bit. INFERENCE: the handler's dungeon compare is
    also guarded by a per-user pointer this trace did not name; the port
    reads the guard as "the user stands in a dungeon", the only case the
    region test names.
  - The handler's own gate is a per-user counter: refused with 0x185B
    while it is nonzero, else armed to 60, and counted down by the
    per-user timer step. INFERENCE: that step runs once a second, as the
    other per-user state timers it walks do, so the gate is 60 s.
  - INFERENCE: a cave floor has no form of the outdoor ring sample (the
    plane owner refuses it, PlaneFeatures.QuestMonsterSpawns), so the
    stone used in a dungeon lands its monster on the user's own point,
    grounded as a cave nest's respawn is. The native ring is 0 to 70
    units, so the point is one the native sample can draw.
  - The Rigid rows also carry a COOLTIME parameter of 600000 ms (category
    word 0xF1). Where that keyword becomes a timer does not live in the
    game-server binary, so the port mirrors it as the Rigid family's
    gate; the stone's row has no COOLTIME and keeps the handler's 60 s.
  - A rarity-3 kind announces itself through the ordinary unique
    appearance notice; MOB_AUTOMOB is no unique and announces nothing.

Refusals spend nothing: the summon event's town and arena refusals
(summonEventRefusal, shared with the party scroll), the dungeon refusal,
and the gate answering with the native reuse delay. The gate is shared
by every scroll and kept on the Runtime, session-scoped like the native
counter.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// The native spawn ring at the user: 0 to 70 units.
const (
	monsterScrollRadiusMin  = 0
	monsterScrollRadiusSpan = 70
)

// monsterScrollGateMs is the handler's own per-user gate: the counter
// armed to 60, one step a second.
const monsterScrollGateMs = 60_000

// rigidSummonCooldownMs is the Rigid family's cooltime: 600 s, those
// item rows' COOLTIME parameter value, shared by all eight scrolls.
const rigidSummonCooldownMs = 600_000

// AutoMobStoneItem is the anti-bot stone; AutoMobMonster is what it calls.
const (
	AutoMobStoneItem = "ITEM_ETC_AUTOMOB"
	AutoMobMonster   = "MOB_AUTOMOB"
)

/*
================
monsterSummonScroll

One scroll's row: the monster its first parameter names, the gate its
use arms, whether a unique appearance is announced, and whether the
scroll may be used in a dungeon region.
================
*/
type monsterSummonScroll struct {
	monster    string
	gateMs     int64
	announce   bool
	inDungeons bool
}

// monsterSummonScrolls mirrors the shipped rows' first parameter
// descriptions (scripts/data/rigid-scroll-rows.json for the Rigid rows,
// itemdata row 3664 for the stone).
var monsterSummonScrolls = map[string]monsterSummonScroll{
	"ITEM_RIGID_TIGERWOMAN_L2_SUMMON_SCROLL": rigidScroll("MOB_CH_TIGERWOMAN_L2"),
	"ITEM_RIGID_KERBERUS_L2_SUMMON_SCROLL":   rigidScroll("MOB_EU_KERBEROS_L2"),
	"ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL":     rigidScroll("MOB_OA_URUCHI_L2"),
	"ITEM_RIGID_IVY_L2_SUMMON_SCROLL":        rigidScroll("MOB_AM_IVY_L2"),
	"ITEM_RIGID_ISYUTARU_L2_SUMMON_SCROLL":   rigidScroll("MOB_KK_ISYUTARU_L2"),
	"ITEM_RIGID_BONELORD_L2_SUMMON_SCROLL":   rigidScroll("MOB_TK_BONELORD_L2"),
	"ITEM_RIGID_TAHOMET_L2_SUMMON_SCROLL":    rigidScroll("MOB_RM_TAHOMET_L2"),
	"ITEM_RIGID_BLACKSNAKE_L2_SUMMON_SCROLL": rigidScroll("MOB_TQ_BLACKSNAKE_L2"),
	AutoMobStoneItem:                         {monster: AutoMobMonster, gateMs: monsterScrollGateMs, inDungeons: true},
}

/*
================
rigidScroll

A Rigid unique's row: the family's 600 s cooltime, announced, refused
in a dungeon.
================
*/
func rigidScroll(monster string) monsterSummonScroll {
	return monsterSummonScroll{monster: monster, gateMs: rigidSummonCooldownMs, announce: true}
}

/*
================
MonsterSummonScrollMonster

The monster the named scroll summons; false for any other item.
================
*/
func MonsterSummonScrollMonster(codename string) (string, bool) {
	scroll, ok := monsterSummonScrolls[codename]
	return scroll.monster, ok
}

/*
================
Runtime.useMonsterSummonScroll

One monster beside the user, persistent until killed, and the scroll
spent; the gate then holds every scroll for the user for the used row's
time. Native refusals (town, arena, dungeon) and the gate spend nothing.
================
*/
func (rt *Runtime) useMonsterSummonScroll(use persistentSummonUse, result *OpResult) bool {
	c := use.character
	scroll, ok := monsterSummonScrolls[use.ref.Codename]
	if !ok {
		result.DiagnosticRefusal = "item-use: not a monster summon scroll"
		return false
	}
	if rt.Monsters == nil || !enterworld.CharacterAlive(c) || c.Level == nil {
		result.DiagnosticRefusal = "item-use: monster summon scroll owner unavailable"
		return false
	}
	at, ok := rt.summonEventRefusal(use, monsterSummonGate, result)
	if !ok {
		return false
	}
	plane := simulation.FeaturesAt(at.RegionID)
	if !plane.MonsterSummonScrolls && !scroll.inDungeons {
		*result = itemUseFailure(wire.ErrCodeSummonInDungeon)
		return false
	}
	if rt.monsterScrollReady == nil {
		rt.monsterScrollReady = make(map[string]int64)
	}
	if rt.monsterScrollReady[c.Name] > use.nowMs {
		*result = itemUseFailure(wire.ErrCodeItemReuseDelay)
		return false
	}
	spawn := simulation.QuestMonsterSpawn{
		Codename: scroll.monster, RadiusMin: monsterScrollRadiusMin, RadiusSpan: monsterScrollRadiusSpan,
		AnnounceUnique: scroll.announce,
	}
	if !plane.QuestMonsterSpawns {
		// A cave floor: the user's own point, no ring (file header).
		spawn.Position, spawn.FixedPosition, spawn.RadiusMin, spawn.RadiusSpan = at, true, 0, 0
	}
	landed := rt.SpawnQuestMonster(use.division, c, spawn)
	remaining := rt.consumeItemUseRow(c, use.row)
	rt.monsterScrollReady[c.Name] = use.nowMs + scroll.gateMs
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	if !landed {
		result.Frames = append(result.Frames, exchangeBanner(partySummonFailedLine))
	}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
