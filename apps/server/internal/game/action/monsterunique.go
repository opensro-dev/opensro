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

/*
================
prepareUniqueDrops

The native prepass may exceed the ordinary capacity, notably for Roc. That
capacity stops later assigned/ordinary additions; it does not truncate this list.
================
*/
func (rt *Runtime) prepareUniqueDrops(ctx uniqueDropContext) []grounditem.Item {
	rarity := ctx.mob.Rarity()
	if rarity&15 != 3 && rarity&15 != 8 {
		return nil
	}
	code := ctx.mob.Ref.Codename
	count, ok := rt.rollUniqueCount(5, 8)
	if !ok {
		return nil
	}
	if rarity == 8 {
		count, ok = rt.rollUniqueCount(1, 2)
	} else if code == "MOB_RM_ROC" {
		count, ok = rt.rollUniqueCount(50, 80)
	}
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
	count, ok = rt.rollUniqueCount(0, 2)
	if !ok {
		return nil
	}
	switch code {
	case "MOB_TK_BONELORD":
		count, ok = rt.rollUniqueCount(0, 1)
	case "MOB_TQ_WHITESNAKE":
		count, ok = rt.rollUniqueCount(1, 3)
	case "MOB_RM_ROC":
		count, ok = rt.rollUniqueCount(10, 20)
	default:
		if rarity == 8 {
			count = 0
		}
	}
	if !ok {
		return nil
	}
	for i := 0; i < count; i++ {
		if item, accepted, _ := rt.prepareUniqueEquipment(ctx, false); accepted {
			drops = append(drops, item)
		}
	}
	count, ok = rt.rollUniqueCount(0, 2)
	if !ok {
		return nil
	}
	switch code {
	case "MOB_TQ_WHITESNAKE":
		count, ok = rt.rollUniqueCount(1, 3)
	case "MOB_RM_ROC":
		count, ok = rt.rollUniqueCount(50, 100)
	default:
		if rarity == 8 {
			count, ok = rt.rollUniqueCount(1, 2)
		}
	}
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
	count, ok = rt.rollUniqueCount(10, 20)
	if !ok {
		return nil
	}
	if rarity == 8 {
		count = 0
	} else if code == "MOB_RM_ROC" {
		count, ok = rt.rollUniqueCount(100, 150)
	}
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
