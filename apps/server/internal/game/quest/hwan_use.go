/*
===========================================================================

hwan_use.go - the quest tools and the trap of the Blue Berserk chains

Port-only, not native; the rules are the port's reading of the 1.188-era game data, with its inferences marked:

	Soul-ringing Bell   used within 100 of the cemetery southeast of Jangan it
	                    summons the Old Tombstone Ghost 90 to 100 from the
	                    player; the bell is spent either way, and a player who
	                    already holds the Soul Shell cannot use it
	Mountain Essence    used on the selected Yeoha (or Decayed Yeoha) within
	                    300: one Branch of Life; any other target is refused
	Radiant Crystal     used within 300 of Cleopatra's Gate: a ten second dig,
	                    then the Relic of the Sun God with the odds of
	                    SRO_HWAN_SUN_RELIC (original: 11 of 101)
	Trap for Spirit     closing on a Niya General rolls Normal Spirit, Flutter
	                    Spirit or nothing (SRO_HWAN_NIYA_CAPTURE_RATES; original
	                    2, 28 and 71 of 101)

The tools go through the same admission as the older quest tools (gather.go).

===========================================================================
*/
package quest

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	hwanGhost          = "MOB_QT_CH_TOMBSTONE_01"
	hwanGhostMin       = 90
	hwanGhostSpan      = 10
	hwanBellRegion     = 23980
	hwanBellRadius     = 100
	hwanEssenceRange   = 300
	hwanDigRegion      = 25954
	hwanDigRadius      = 300
	hwanDigSeconds     = 10
	hwanNiyaGeneral    = "MOB_TK_BONEGENERAL"
	hwanNiyaSkill      = "SKILL_QNO_CH_HWAN_1_4_04_01"
	noSelectedTarget   = "UIIT_SKILL_USE_FAIL_NONESELECTETARGET"
	cannotUseQuestTool = "UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM"
)

// hwanYeoha lists the monsters the Mountain Essence works on.
var hwanYeoha = []string{"MOB_CH_YEOHA", "MOB_CH_YEOHA_CLON"}

func hwanBellPoint() simulation.Spawn {
	return simulation.Spawn{RegionID: hwanBellRegion, X: 1218, Y: 230, Z: 706}
}

func hwanDigPoint() simulation.Spawn {
	return simulation.Spawn{RegionID: hwanDigRegion, X: 1672, Y: 180, Z: 232}
}

/*
================
hwanActive

The character's active quest of the given chain and step, whichever race it is.
================
*/
func (rt *Runtime) hwanActive(c *enterworld.Character, chain int, step string) *Definition {
	for _, race := range hwanRaces {
		if def, ok := rt.Defs.ByCodename(hwanQuest(race, chain, step)); ok && activeQuestIndex(c, def.RefID) >= 0 {
			return def
		}
	}
	return nil
}

/*
================
hwanDeclined

The refusal every Blue Berserk tool gives when its quest is not running.
================
*/
func hwanDeclined() ([]wire.Frame, bool) {
	return []wire.Frame{questNotification(cannotUseQuestTool)}, false
}

/*
================
useHwanBell

The Soul-ringing Bell. The caller spends the bell when the ghost is summoned.
================
*/
func (rt *Runtime) useHwanBell(c *enterworld.Character, at simulation.Spawn) ([]wire.Frame, bool) {
	def := rt.hwanActive(c, 1, "3_1")
	if def == nil || captureItemCount(c, hwanShell) > 0 {
		return hwanDeclined()
	}
	if !withinQuestArea(at, hwanBellPoint(), hwanBellRadius) {
		return []wire.Frame{questNotification(hwanTalk(def.Codename) + "10")}, false
	}
	ghost := simulation.QuestMonsterSpawn{Codename: hwanGhost, RadiusMin: hwanGhostMin, RadiusSpan: hwanGhostSpan}
	if rt.SpawnQuestMonster == nil || !rt.SpawnQuestMonster(c, ghost) {
		return nil, false
	}
	return nil, true
}

/*
================
useHwanEssence

The Mountain Essence on the selected monster.
================
*/
func (rt *Runtime) useHwanEssence(c *enterworld.Character, at simulation.Spawn) ([]wire.Frame, bool) {
	def := rt.hwanActive(c, 2, "3")
	if def == nil || captureItemCount(c, hwanBranchOfLife) > 0 {
		return hwanDeclined()
	}
	if rt.SelectedMonster == nil {
		return hwanDeclined()
	}
	target, where, selected := rt.SelectedMonster(c)
	if !selected {
		return []wire.Frame{questNotification(noSelectedTarget)}, false
	}
	if distance := simulation.WorldDistance2D(at, where); !(distance <= hwanEssenceRange) {
		return hwanDeclined()
	}
	if target != hwanYeoha[0] && target != hwanYeoha[1] {
		return []wire.Frame{questNotification(hwanTalk(def.Codename) + "09")}, false
	}
	// Inference: a full bag only skips the grant (nothing is lost); the essence is kept then.
	rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: hwanBranchOfLife, Count: 1}})
	if err != nil {
		return nil, false
	}
	c.MissionInventory = rows
	objectives, _ := rt.applyInventoryChange(c)
	return append(updates, objectives...), true
}

/*
================
useHwanCrystal

The Radiant Crystal: the dig is a ten second gather job whose roll is the
Sun God relic knob. The crystal is spent when the dig starts.
================
*/
func (rt *Runtime) useHwanCrystal(c *enterworld.Character, at simulation.Spawn, nowMs int64) ([]wire.Frame, bool) {
	def := rt.hwanActive(c, 2, "4")
	if def == nil || captureItemCount(c, hwanSunRelic) > 0 {
		return hwanDeclined()
	}
	talk := hwanTalk(def.Codename)
	if !withinQuestArea(at, hwanDigPoint(), hwanDigRadius) {
		return []wire.Frame{questNotification(talk + "09")}, false
	}
	if !emptyQuestBagSlot(c) {
		return hwanDeclined()
	}
	return rt.startGatherJob(c, gatherJob{quest: def.RefID, remaining: hwanDigSeconds, nextMs: nowMs + questSecondMs,
		award: hwanSunRelic, failSymbol: talk + "10", success: talk + "08", roll: gatherRelic})
}

/*
================
canPlaceNiyaTrap

A Trap for Spirit is placed only while the trap quest runs.
================
*/
func (rt *Runtime) canPlaceNiyaTrap(c *enterworld.Character) ([]wire.Frame, bool) {
	if rt.hwanActive(c, 1, "4_1") == nil {
		return hwanDeclined()
	}
	return nil, true
}

/*
================
captureNiya

A closed trap on a Niya General. The bag must have room for the spirit first;
then one roll picks nothing, a Flutter Spirit or the Normal Spirit. The monster
is taken only when a spirit is.
================
*/
func (rt *Runtime) captureNiya(c *enterworld.Character, retire func() bool) ([]wire.Frame, bool) {
	def := rt.hwanActive(c, 1, "4_1")
	if def == nil {
		return nil, false
	}
	talk := hwanTalk(def.Codename)
	if !emptyQuestBagSlot(c) {
		return []wire.Frame{questNotification(talk + "11")}, false
	}
	roll := rt.CaptureRoll
	if roll == nil {
		roll = combat.SecureRoll32767
	}
	value, err := roll()
	if err != nil || value > captureMaximumRoll {
		return nil, false
	}
	item, notice := "", talk+"09"
	switch rt.Hwan.NiyaCapture(value) {
	case NiyaNormalSpirit:
		item, notice = hwanNormalSpirit, talk+"08"
	case NiyaFlutterSpirit:
		item, notice = hwanFlutterSpirit, talk+"14"
	default:
		return []wire.Frame{questNotification(notice)}, false
	}
	rows, frames, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: item, Count: 1}})
	if err != nil {
		return []wire.Frame{questNotification(talk + "11")}, false
	}
	if !retire() {
		return nil, false
	}
	c.MissionInventory = rows
	updates, _ := rt.applyInventoryChange(c)
	frames = append(frames, updates...)
	return append(frames, questNotification(notice)), true
}

// isHwanDigQuest names the quests whose crystal dig is a gather job that
// abandoning must cancel.
func isHwanDigQuest(codename string) bool {
	for _, race := range hwanRaces {
		if codename == hwanQuest(race, 2, "4") {
			return true
		}
	}
	return false
}
