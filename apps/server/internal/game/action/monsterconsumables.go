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

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
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
	// Port-only, not native, beta balance (owner 2026-10-13): every monster
	// drop path ends here, so with elixir drops off an elixir pick drops
	// nothing; the blacksmith sells them (progression/elixirdrops.go).
	if rt.ElixirDrops.Excludes(ref.TypeIDs) {
		return grounditem.Item{}, false
	}
	// Port-only, not native, beta balance (owner 2026-10-14): alchemy
	// materials piled up as junk on every hunting ground, so their pick
	// drops nothing (progression/materialdrops.go).
	if rt.MaterialDrops.Excludes(ref.TypeIDs) {
		return grounditem.Item{}, false
	}
	// Port-only, not native, beta balance (owner 2026-10-13): degree-9
	// alchemy goods come only from the shop, so their pick drops nothing.
	if itemClass, _ := ref.NativeFields.Lookup("itemClass"); rt.ShopOnly.Excludes(ref.TypeIDs, int(itemClass)) {
		return grounditem.Item{}, false
	}
	// Port-only, not native (owner 2026-10-14; narrowed 2026-10-18 to
	// non-unique kills): while the Sun event rule is on, degree-9 Seal of Sun
	// gear drops only from unique monsters, so an ordinary monster's Sun pick
	// drops nothing and is never re-rolled; every other chance stays native
	// (action/sunevent.go). The deployment default is off since 2026-10-20,
	// when drops became the Sun source (the rate above).
	if !chosen.FromUnique && SunExcludes(rt.SunEventOnly, ref) {
		return grounditem.Item{}, false
	}
	// Port-only, not native, beta balance (owner 2026-10-20): a degree-9
	// Sun drop is the rate's whole product; name each one in the log so
	// the supply can be counted without a database.
	if rt.SunRate.Active() {
		rarity, _ := ref.NativeFields.Lookup("rarity")
		itemClass, _ := ref.NativeFields.Lookup("itemClass")
		if progression.IsEventOnlySun(ref.TypeIDs, int(rarity), int(itemClass)) {
			log.Infof("sun drop: %s (degree-9 Seal of Sun) for %s", ref.Codename, owner)
		}
	}
	row := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: chosen.Count, Plus: chosen.Plus}
	if ref.TypeIDs[1] == 1 {
		variance, durability, ok := rt.rollDroppedEquipmentVariance(ref)
		if !ok {
			return grounditem.Item{}, false
		}
		row.VarianceBits, row.Durability = variance, durability
		// The native loot step initializes variance and authored options, not random blues.
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
