/*
===========================================================================

monsterunique.go - the native unique-monster reward prepass

The reference server runs this producer before assigned and ordinary rewards.
Each generated instance goes through the common property and ground-item door.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/enterworld"
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

The native special equipment constructor refuses accessories. The first unique equipment batch retries those
choices; other constructor failures consume the attempt.
================
*/
func (rt *Runtime) prepareUniqueEquipment(ctx uniqueDropContext, nonRepair bool) (grounditem.Item, bool, bool) {
	country, ok := rt.resolveMonsterDropCountry(ctx.mob.Ref.Country)
	if !ok {
		return grounditem.Item{}, false, false
	}
	level := rt.uniqueDropTableLevel(ctx.mob)
	group, ok := loot.SpecialEquipmentGroup(level)
	if !ok {
		return grounditem.Item{}, false, false
	}
	chosen, ok := loot.SelectEquipment(country, group, false, level, rt.DropRoll)
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
		Codename: chosen.Codename, Count: 1, Plus: plus, Special: true, NonRepair: nonRepair, FromUnique: true,
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
pharaohGearRate

Port-only, not native. A Pharaoh Tomb elite (grade 6) or unique (grade 8)
rolls its equipment this many times as often: the owner found the tomb
bosses' gear too rare to test. The rate rides the monster's reference
(SRO_PHARAOH_TOMB_GEAR_RATE, read when the tomb loads). Every other monster,
and a tomb member of any other grade, keeps the native rule (1).
================
*/
func (rt *Runtime) pharaohGearRate(mob monster.Instance) int {
	rate := int(mob.Ref.PharaohGearRate)
	if rate <= 1 {
		return 1
	}
	switch mob.Rarity() {
	case 6, 8:
		return rate
	}
	return 1
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
	count *= rt.pharaohGearRate(ctx.mob)
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

//============================================================================
// Guaranteed seals (port-only, not native; owner 2026-10-25)
//============================================================================

// uniqueSealAttempts bounds the re-draws of one guaranteed seal: a seal
// bucket's absolute admission or a reference lookup can refuse a pick, but a
// guaranteed reward must not go missing on an ordinary draw.
const uniqueSealAttempts = 32

/*
================
uniqueSealRewards

The seal tiers one unique kill rewards, in drop order, with the draw each is
guaranteed on: 0 means every kill. Owner decision 2026-10-26 (the round-1
shape, held through round 2): a Star every kill and a Moon one kill in ten,
on top of whatever the ordinary rare rolls bring. There is no guaranteed
Sun: it stays on the ordinary rare roll for uniques as for everyone else.
================
*/
var uniqueSealRewards = []struct {
	tier  int
	oneIn uint32
}{
	{enterworld.SealStar, 0},
	{enterworld.SealMoon, 10},
}

/*
================
prepareUniqueSeals

One unique kill's guaranteed sealed equipment: the highest degree the drop
level offers (loot.SealGroupAt; degree 9 at the native cap, degree 11 once
the cap-110 catalog moves it), one named seal each. The Star leaves every
kill, the Moon one in ten; no Sun is guaranteed. The seal rates and the
Sun class widening do not thin these: they are the reward, not a rare roll.
================
*/
func (rt *Runtime) prepareUniqueSeals(ctx uniqueDropContext) []grounditem.Item {
	if !RollsUniqueDrops(ctx.mob.Rarity()) || rt.DropRoll == nil {
		return nil
	}
	country, ok := rt.resolveMonsterDropCountry(ctx.mob.Ref.Country)
	if !ok {
		return nil
	}
	level := rt.uniqueDropTableLevel(ctx.mob)
	var drops []grounditem.Item
	for _, reward := range uniqueSealRewards {
		if reward.oneIn > 1 {
			draw, err := rt.DropRoll()
			if err != nil {
				return drops
			}
			if draw%reward.oneIn != 0 {
				continue
			}
		}
		group, ok := loot.SealGroupAt(level, reward.tier)
		if !ok {
			continue
		}
		if item, ok := rt.prepareSealEquipment(country, group, level, ctx); ok {
			drops = append(drops, item)
		}
	}
	return drops
}

/*
================
prepareSealEquipment

One guaranteed seal pick: the sealed class of the named tier, its native
equipment plus roll, and the common door (variance, blues, ground item).
Rare equipment always enters the magic-option constructor, so a
seal carries options; the retry covers absolute admission, an unavailable
reference and a mixed bucket's item of another seal tier. The native Moon
bucket includes Star body armour: only this port-only reward rejects it;
ordinary native equipment selection keeps the original mixed assignment.
================
*/
func (rt *Runtime) prepareSealEquipment(country uint8, group int, level uint8, ctx uniqueDropContext) (grounditem.Item, bool) {
	items := rt.deps.ItemReferences()
	if items == nil {
		return grounditem.Item{}, false
	}
	for attempt := 0; attempt < uniqueSealAttempts; attempt++ {
		chosen, ok := loot.SelectEquipment(country, group, true, level, rt.DropRoll)
		if !ok {
			continue
		}
		ref, found := items.ItemRefByCodename(chosen.Codename)
		seal, sealed := enterworld.EquipmentSeal(ref)
		if !found || !sealed || seal != group%enterworld.SealTiers {
			continue
		}
		plus, ok := rt.rollDroppedEquipmentPlus()
		if !ok {
			return grounditem.Item{}, false
		}
		item, ok := rt.prepareSelectedDrop(loot.DropItem{
			Codename: chosen.Codename, Count: 1, Plus: plus, FromUnique: true,
		}, ctx.at, ctx.owner, ctx.now)
		if ok {
			return item, true
		}
	}
	return grounditem.Item{}, false
}
