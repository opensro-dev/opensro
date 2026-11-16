/*
===========================================================================

partysummonscroll.go - the party Monster Summon Scroll: strong monsters
to hunt, one per party member

Port-only, not native to v1.150: the item ships in the v1.150 data
(ITEM_ETC_E060526_SUMMON_PARTY_SCROLL_A, "Monster Summon Scroll (party
use)", 3/3/15/2) but no v1.150 server serves it. The rules are the
v1.188 server's (its summon-scroll quest event, traced 2026-11-16):

  - Refusals, in order, none spending the scroll: a town region
    ("Summon scrolls cannot be used in towns.", SN_TALK_QEV_CH_EVENT_0818_01),
    a user below level 30 (UIIT_MSG_PANDORA_USE_ERROR), the battle arena
    (UIIT_MSG_ARENA_ERR_PANDORA_USE). No party is required and the
    handler has no cooldown of its own.
  - One monster per party member (one alone), all of the same kind:
    the strong-monster table at min(user level, 110), the user's own
    level only. Each lands 20 + 0..80 units from the user, at grade 7,
    with no owner: anyone may hunt it. Its hostility is its default
    tactics', chosen with grade 0: most of the table's kinds
    attack on sight, a few (the Yeti) do not; a kind the port's tactics
    data does not cover attacks on sight (inference: the event monster is
    there to be fought).
  - Grade 7 is x30 HP, x30 EXP and SP, x1.8 skill damage
    (world/monster EffectiveMaxHP, reward.go, combat MonsterDamageScale).
  - A grade-7 monster outside the arena leaves after 300 s; a timeout
    gives nothing. Its kill pays its ordinary loot and experience (the
    event's special kill drop is switched off for this event).

The table names MOB_EVE_STRONG_<X>, a row that is MOB_<X> with grade 7
and nothing else (level and HP equal on every row). v1.150 has no
MOB_EVE_STRONG row, so the port spawns MOB_<X> at grade 7: the same
monster. Levels 81 to 110 name Qin-Shi tomb and Egypt kinds a v1.150
shard may not hold; a missing kind takes the nearest lower level whose
kind the shard holds (inference: the scroll should never answer
nothing to a high-level user).

The scroll reaches players only through So-Ok's Christmas Gift
(progression/sook_christmas.go), so it is served while that event's mode
is selected (SRO_CHRISTMAS_EVENT_MODE=so_ok_2006), window or not; in any
other mode it is dead stock, as the native v1.150 server leaves it.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// PartySummonScrollItem is the scroll the port serves.
const PartySummonScrollItem = "ITEM_ETC_E060526_SUMMON_PARTY_SCROLL_A"

const (
	// partySummonMinLevel is the native level floor.
	partySummonMinLevel = 30
	// partySummonMaxLevel is the table's top key.
	partySummonMaxLevel = 110
	// partySummonGrade is the grade the scroll's monsters spawn at.
	partySummonGrade = 7
	// partySummonLifetimeMs is a grade-7 monster's native 300 s.
	partySummonLifetimeMs = 300000
	// The spawn ring: 20 + 0..80 units from the user.
	partySummonRadiusMin  = 20
	partySummonRadiusSpan = 80
)

// The refusal and success lines. The v1.150 text carries only the town
// line; the others are the v1.188 client's English rows, sent as
// notices. Port-only.
const (
	partySummonTownLine   = "Summon scrolls cannot be used in towns."
	partySummonLevelLine  = "Pandora Box and Monster summon scroll can be used if your character is over Lv. 30."
	partySummonArenaLine  = "Unable to use item in the Battle Arena."
	partySummonSoloLine   = "A summoned monster has appeared. It will disappear in 5 minutes."
	partySummonPartyLine  = "Summoned monsters have appeared, one for each party member. They will disappear in 5 minutes."
	partySummonFailedLine = "No monster could be summoned here."
)

// partySummonKinds is the v1.188 server's table from level 30 to 110,
// as the v1.150 MOB_<X> each MOB_EVE_STRONG_<X> row copies.
var partySummonKinds = [partySummonMaxLevel - partySummonMinLevel + 1]string{
	"MOB_WC_HYEONGCHEON", "MOB_CA_PERYTON_CLON", "MOB_OA_SCORPION", "MOB_CA_ONG_CLON", "MOB_CA_ONG",
	"MOB_OA_YEOWA_CLON", "MOB_OA_BLACKROBBERARCHER", "MOB_CA_MONKEY_CLON", "MOB_OA_BLACKROBBER", "MOB_CA_GOLEM_CLON",
	"MOB_OA_REDSCORPION", "MOB_OA_REDEYEGHOST", "MOB_OA_REDEYEGHOST_CLON", "MOB_KT_ONG_CLON2", "MOB_KT_BUNWANG_CLON",
	"MOB_KT_BUNWANG", "MOB_KT_DESERTTRUNKZ_CLON", "MOB_KT_WHITESPIDER_CLON", "MOB_KT_WHITESPIDER", "MOB_KT_MUJIGI",
	"MOB_KT_MUJIGI_CLON", "MOB_KK_ISHADE", "MOB_KK_SPIDER_CLON", "MOB_KK_SPIDER", "MOB_KK_PENON_F",
	"MOB_KK_PENON_W", "MOB_KK_PLANAR", "MOB_KK_PLANAR_CLON", "MOB_KK_GWHITESPIDER", "MOB_KK_YETI",
	"MOB_KK_YETI_CLON", "MOB_DH_BUGGHOST_CLON", "MOB_DH_WORKEARTHGHOST_CLON", "MOB_DH_BUGGHOST", "MOB_DH_WORKEARTHGHOST",
	"MOB_DH_SOLDIEREARTHGHOST_CLON", "MOB_DH_SOLDIEREARTHGHOST", "MOB_DH_BLINDMON_CLON", "MOB_DH_BLINDMON", "MOB_DH_YACHA_CLON",
	"MOB_RM_FEATHERMASK_CLON", "MOB_TK_BONESOLDIER_CLON", "MOB_RM_ROCKY_CLON", "MOB_RM_FEATHERMASK", "MOB_TK_BONEHUNTER_CLON",
	"MOB_RM_GOAT_CLON", "MOB_TK_BONEHUNTER", "MOB_TK_BONEMAGE_CLON", "MOB_RM_WINGTRIBE_CLON", "MOB_TK_BONEGENERAL_CLON",
	"MOB_RM_WINGTRIBE", "MOB_TQ_TOMBSOLDIER_CLON2", "MOB_RM_ANTELOPE_CLON", "MOB_TQ_QINSHITOMBSTONE_CLON", "MOB_TQ_TOMBARCHER_CLON2",
	"MOB_RM_WINGTRIBESPEAR_CLON", "MOB_TQ_TOMBSTONEGHOST_CLON2", "MOB_RM_WINGTRIBESPEAR", "MOB_TQ_ROYALSOLDIER_CLON2", "MOB_TQ_ROYALSOLDIER_CLON",
	"MOB_TQ_TOMBBUGGHOST_CLON2", "MOB_TQ_SNAKESLAVE_CLON2", "MOB_TQ_SNAKEWOMAN_CLON2", "MOB_TQ_SNAKESLAVE_CLON", "MOB_TQ_SNAKEDEMON_CLON2",
	"MOB_TQ_SNAKESLAVE", "MOB_TQ_SNAKEDEMON_CLON", "MOB_TQ_SNAKEMAN_CLON2", "MOB_TQ_SNAKEMAN_CLON", "MOB_TQ_SNAKEMAN",
	"MOB_TQ_MANGNYANG", "MOB_SD_WENEG", "MOB_SD_KEISAS", "MOB_SD_SAND_HYENA", "MOB_SD_RERET",
	"MOB_SD_MUMMY_RAGE", "MOB_SD_TELBASTA", "MOB_SD_EVILBUG", "MOB_SD_BLOODKHEPRI", "MOB_SD_AKERU",
	"MOB_SD_SOPEDU",
}

/*
================
partySummonKind

The kind the scroll calls for a user of level: the table's row at
min(level, 110), or the nearest lower row the shard holds (has answers
whether it holds a kind); false below level 30 or when no row from
there down is held.
================
*/
func partySummonKind(level int64, has func(string) bool) (string, bool) {
	if level < partySummonMinLevel {
		return "", false
	}
	key := min(level, partySummonMaxLevel)
	for at := key - partySummonMinLevel; at >= 0; at-- {
		if has(partySummonKinds[at]) {
			return partySummonKinds[at], true
		}
	}
	return "", false
}

/*
================
Runtime.usePartySummonScroll

The native refusals answer first and spend nothing; then one monster per
party member lands around the user and the scroll is spent, whatever the
ground let land (the native handler spends it on success either way).
================
*/
func (rt *Runtime) usePartySummonScroll(use persistentSummonUse, result *OpResult) bool {
	c := use.character
	if !rt.SoOkChristmas.Selected() {
		result.DiagnosticRefusal = "item-use: the party summon scroll is served only in So-Ok's Christmas Gift's mode"
		return false
	}
	if rt.Monsters == nil || !enterworld.CharacterAlive(c) || c.Level == nil {
		result.DiagnosticRefusal = "item-use: party summon scroll owner unavailable"
		return false
	}
	at := rt.liveSpawn(simulation.WorldKey(use.division, c.Name), c, use.nowMs)
	if battlefield, known := world.RegionPlayerCombat(at.RegionID); !known || !battlefield {
		result.Frames = append(result.Frames, exchangeBanner(partySummonTownLine))
		return false
	}
	if *c.Level < partySummonMinLevel {
		result.Frames = append(result.Frames, exchangeBanner(partySummonLevelLine))
		return false
	}
	if rt.sookRun(use.division, c) != nil {
		result.Frames = append(result.Frames, exchangeBanner(partySummonArenaLine))
		return false
	}
	kind, found := partySummonKind(*c.Level, func(codename string) bool {
		_, held := rt.Monsters.ReferenceByCodename(codename)
		return held
	})
	if !found {
		result.Frames = append(result.Frames, exchangeBanner(partySummonFailedLine))
		return false
	}
	request := simulation.QuestMonsterSpawn{
		Codename: kind, RadiusMin: partySummonRadiusMin, RadiusSpan: partySummonRadiusSpan,
		LifetimeMs: partySummonLifetimeMs, Rarity: partySummonGrade, HasRarity: true, Aggressive: true,
	}
	members := max(1, len(rt.auraParty(use.division, c)))
	landed := 0
	for range members {
		if rt.SpawnQuestMonster(use.division, c, request) {
			landed++
		}
	}
	line := partySummonSoloLine
	if members > 1 {
		line = partySummonPartyLine
	}
	if landed == 0 {
		line = partySummonFailedLine
	}
	remaining := rt.consumeItemUseRow(c, use.row)
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	result.Frames = append(result.Frames, exchangeBanner(line))
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
