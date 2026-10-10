/*
===========================================================================

monsterunique.go - the native unique-monster reward prepass

SR_GameServer 726020 runs this producer before assigned and ordinary rewards.
Each generated instance goes through the common property and ground-item door.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
uniqueDropContext
================
*/
type uniqueDropContext struct {
	mob   monster.Instance
	at    simulation.Spawn
	owner string
	now   time.Time
}

/*
================
rollUniqueCount
================
*/
func (rt *Runtime) rollUniqueCount(minimum, maximum int) (int, bool) {
	if rt.DropRoll == nil {
		return 0, false
	}
	n, err := rt.DropRoll()
	if err != nil || n > 32767 {
		return 0, false
	}
	return minimum + int(n%uint32(maximum-minimum+1)), true
}

/*
================
prepareUniqueEquipment

725600 refuses accessories. The first unique equipment batch retries those
choices; other constructor failures consume the attempt (726020).
================
*/
func (rt *Runtime) prepareUniqueEquipment(ctx uniqueDropContext, nonRepair bool) (grounditem.Item, bool, bool) {
	country, ok := rt.resolveMonsterDropCountry(ctx.mob.Ref.Country)
	if !ok {
		return grounditem.Item{}, false, false
	}
	group, ok := loot.SpecialEquipmentGroup(ctx.mob.Ref.Level)
	if !ok {
		return grounditem.Item{}, false, false
	}
	chosen, ok := loot.SelectEquipment(country, group, false, ctx.mob.Ref.Level, rt.DropRoll)
	if !ok {
		return grounditem.Item{}, false, false
	}
	items := rt.deps.ItemReferences()
	if items == nil {
		return grounditem.Item{}, false, false
	}
	ref, ok := items.ItemRefByCodename(chosen.Codename)
	if !ok || ref == nil {
		return grounditem.Item{}, false, false
	}
	if ref.TypeIDs[2] == 5 || ref.TypeIDs[2] == 12 {
		return grounditem.Item{}, false, true
	}
	plus := uint8(5)
	for i := 0; i < 2; i++ {
		value, ok := rt.rollUniqueCount(1, 5)
		if !ok {
			return grounditem.Item{}, false, false
		}
		plus = min(plus, uint8(value))
	}
	item, ok := rt.prepareSelectedDrop(loot.DropItem{
		Codename: chosen.Codename, Count: 1, Plus: plus, Special: true, NonRepair: nonRepair,
	}, ctx.at, ctx.owner, ctx.now)
	return item, ok, false
}

// The four count stages of a unique's prepass, in roll order: non-repair
// equipment, ordinary special equipment, Elixir recipes, return scrolls.
const (
	uniqueStageEquipment = iota
	uniqueStageRepairable
	uniqueStageRecipes
	uniqueStageScrolls
	uniqueStages
)

/*
================
UniqueCount

One stage's count range. Native always draws the stage's default range
first; Redraw replaces that draw with a second one over Min..Max, and a
rule that differs from the default without Redraw is the fixed count Min.
================
*/
type UniqueCount struct {
	Min, Max int
	Redraw   bool
}

// uniqueCountDefaults are the stages' first draws.
var uniqueCountDefaults = [uniqueStages]UniqueCount{{5, 8, false}, {0, 2, false}, {0, 2, false}, {10, 20, false}}

/*
================
RollsUniqueDrops

Whether a kill of this spawn grade runs the unique prepass: grades 3 and 8
(the low nibble; a party nibble does not change it).
================
*/
func RollsUniqueDrops(rarity uint8) bool {
	return rarity&15 == 3 || rarity&15 == 8
}

/*
================
UniqueDropCounts

The count rule of each prepass stage for a monster: a grade-8 monster, the
Roc, the Bone Lord and the White Snake override the defaults. rarity is
the full spawn byte, as the native compares it. The website's catalogue
reads the ranges.
================
*/
func UniqueDropCounts(rarity uint8, code string) [uniqueStages]UniqueCount {
	counts := uniqueCountDefaults
	if rarity == 8 {
		counts[uniqueStageEquipment] = UniqueCount{1, 2, true}
	} else if code == "MOB_RM_ROC" {
		counts[uniqueStageEquipment] = UniqueCount{50, 80, true}
	}
	switch code {
	case "MOB_TK_BONELORD":
		counts[uniqueStageRepairable] = UniqueCount{0, 1, true}
	case "MOB_TQ_WHITESNAKE":
		counts[uniqueStageRepairable] = UniqueCount{1, 3, true}
		counts[uniqueStageRecipes] = UniqueCount{1, 3, true}
	case "MOB_RM_ROC":
		counts[uniqueStageRepairable] = UniqueCount{10, 20, true}
		counts[uniqueStageRecipes] = UniqueCount{50, 100, true}
	default:
		if rarity == 8 {
			counts[uniqueStageRepairable] = UniqueCount{0, 0, false}
			counts[uniqueStageRecipes] = UniqueCount{1, 2, true}
		}
	}
	if rarity == 8 {
		counts[uniqueStageScrolls] = UniqueCount{0, 0, false}
	} else if code == "MOB_RM_ROC" {
		counts[uniqueStageScrolls] = UniqueCount{100, 150, true}
	}
	return counts
}

/*
================
rollUniqueStage

The stage's default draw, then its override: a second draw or a fixed
count.
================
*/
func (rt *Runtime) rollUniqueStage(stage int, counts [uniqueStages]UniqueCount) (int, bool) {
	first := uniqueCountDefaults[stage]
	count, ok := rt.rollUniqueCount(first.Min, first.Max)
	if !ok {
		return 0, false
	}
	rule := counts[stage]
	if rule.Redraw {
		return rt.rollUniqueCount(rule.Min, rule.Max)
	}
	if rule != first {
		return rule.Min, true
	}
	return count, true
}

/*
================
prepareUniqueDrops

The native prepass may exceed the ordinary capacity, notably for Roc. That
capacity stops later assigned/ordinary additions; it does not truncate this list.
================
*/
func (rt *Runtime) prepareUniqueDrops(ctx uniqueDropContext) []grounditem.Item {
	rarity := ctx.mob.Rarity()
	if !RollsUniqueDrops(rarity) {
		return nil
	}
	counts := UniqueDropCounts(rarity, ctx.mob.Ref.Codename)
	count, ok := rt.rollUniqueStage(uniqueStageEquipment, counts)
	if !ok {
		return nil
	}
	var drops []grounditem.Item
	for i := 0; i < count; i++ {
		item, accepted, accessory := rt.prepareUniqueEquipment(ctx, true)
		if accessory {
			i--
			continue
		}
		if accepted {
			drops = append(drops, item)
		}
	}
	count, ok = rt.rollUniqueStage(uniqueStageRepairable, counts)
	if !ok {
		return nil
	}
	for i := 0; i < count; i++ {
		if item, accepted, _ := rt.prepareUniqueEquipment(ctx, false); accepted {
			drops = append(drops, item)
		}
	}
	count, ok = rt.rollUniqueStage(uniqueStageRecipes, counts)
	if !ok {
		return nil
	}
	for i := 0; i < count; i++ {
		choice, ok := rt.rollUniqueCount(0, 3)
		if !ok {
			return nil
		}
		kind := [...]string{"WEAPON", "SHIELD", "ARMOR", "ACCESSARY"}[choice]
		chosen := loot.DropItem{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_" + kind + "_B", Count: 1}
		if item, accepted := rt.prepareSelectedDrop(chosen, ctx.at, ctx.owner, ctx.now); accepted {
			drops = append(drops, item)
		}
	}
	count, ok = rt.rollUniqueStage(uniqueStageScrolls, counts)
	if !ok {
		return nil
	}
	for i := 0; i < count; i++ {
		chosen := loot.DropItem{Codename: "ITEM_ETC_SCROLL_RETURN_02", Count: 1}
		if item, accepted := rt.prepareSelectedDrop(chosen, ctx.at, ctx.owner, ctx.now); accepted {
			drops = append(drops, item)
		}
	}
	return drops
}
