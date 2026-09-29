/*
===========================================================================

monsterconsumables.go - consumable selection and complete dropped item instances

Every selected item passes through this property owner before ground publication.

===========================================================================
*/

package action

import (
	"math"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
prepareConsumableDrop
================
*/
func (rt *Runtime) prepareConsumableDrop(mob monster.Instance, family int, at simulation.Spawn, owner string, now time.Time) (grounditem.Item, bool) {
	_, _, attempts := loot.MonsterDropBudget(mob.Rarity(), mob.Ref.Codename)
	for attempt := 0; attempt < attempts; attempt++ {
		v, ok := rt.rollCombinedMillion()
		if !ok {
			return grounditem.Item{}, false
		}
		chosen, ok := loot.SelectConsumable(family, mob.Ref.Level, v, rt.DropRoll)
		if ok {
			return rt.prepareSelectedDrop(chosen, at, owner, now)
		}
	}
	return grounditem.Item{}, false
}

/*
================
prepareSelectedDrop
================
*/
func (rt *Runtime) prepareSelectedDrop(chosen loot.DropItem, at simulation.Spawn, owner string, now time.Time) (grounditem.Item, bool) {
	items := rt.deps.ItemReferences()
	if items == nil || chosen.Count == 0 {
		return grounditem.Item{}, false
	}
	ref, ok := items.ItemRefByCodename(chosen.Codename)
	if !ok || ref == nil {
		return grounditem.Item{}, false
	}
	row := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: chosen.Count, Plus: chosen.Plus}
	if ref.TypeIDs[1] == 1 {
		variance, durability, ok := rt.rollDroppedEquipmentVariance(ref)
		if !ok {
			return grounditem.Item{}, false
		}
		row.VarianceBits, row.Durability = variance, durability
		// 724E30 initializes variance and authored options, not random blues.
		// All version-compatible authored modifier sets are empty in this catalog.
		if !chosen.Assigned {
			magic, entered, err := loot.EquipmentMagic(chosen.Codename, rt.DropRoll)
			if err != nil || chosen.Special && !entered {
				return grounditem.Item{}, false
			}
			row.MagicOptions = magic
		}
		if chosen.NonRepair {
			if option, percent, eligible := loot.NonRepairOption(chosen.Codename, len(row.MagicOptions)); eligible {
				row.MagicOptions = append(row.MagicOptions, option)
				row.Durability = uint32(min(uint64(math.MaxUint32), uint64(durability)+uint64(durability)*uint64(percent)/100))
			}
		}
	}
	return PlanItemDrop(row, chosen.Count, at, owner, now), true
}
