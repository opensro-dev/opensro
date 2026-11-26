/*
===========================================================================

serveractor_upkeep.go - AI caravan actors keep fit to fight (port-only)

What a player looks after between fights: an abnormal status a universal
pill cures, a weapon imbue kept running, gear repaired in town. The
runtime asks; the actor then uses the native item, skill or repair path.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

// universalPillCures is the mask of the statuses a universal pill
// (ITEM_ETC_CURE_ALL_*) cures: freeze, frostbite, burn, shock, poison, zombie.
var universalPillCures = abnormal.Freeze.Bit() | abnormal.Frostbite.Bit() | abnormal.Burn.Bit() |
	abnormal.ElectricShock.Bit() | abnormal.Poison.Bit() | abnormal.Zombie.Bit()

/*
================
ServerActorNeedsCure

Whether the actor carries a status a universal pill cures.
================
*/
func (rt *Runtime) ServerActorNeedsCure(division, name string) bool {
	block := rt.playerAbnormal(division, name)
	return block != nil && block.Mask&universalPillCures != 0
}

/*
================
ServerActorImbued

Whether the actor has a weapon imbue running.
================
*/
func (rt *Runtime) ServerActorImbued(division, name string, nowMs int64) bool {
	imbue, _ := rt.activeWeaponImbue(division, name, nowMs)
	return imbue.Pinned
}

/*
================
ServerActorRepair

Restores the actor's worn equipment to full durability, as a town
blacksmith would, by the native repairable rules (no accessory or job
suit); agents carry no gold for it, so none is charged. The result's
Broadcast holds what observers see (a broken item made whole).
================
*/
func (rt *Runtime) ServerActorRepair(division, name string) OpResult {
	c := rt.findCharacter(division, name)
	refs := rt.deps.ItemReferences()
	if c == nil || !c.RuntimeOnly || refs == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	var repaired wearFrames
	rt.deps.Update(c, "caravan repair", func() bool {
		for _, row := range append([]enterworld.InventoryRow(nil), c.MissionInventory...) {
			if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
				continue
			}
			ref, ok := refs.ItemRefByCodename(row.Codename)
			if !ok || ref == nil || !rt.itemRepairable(ref, row) {
				continue
			}
			if maximum := int64(rt.equipmentMaxDurability(&row)); row.Durability < maximum {
				taken := rt.offsetItemDurability(division, c, uint8(row.Slot), int32(maximum-row.Durability))
				repaired.actor = append(repaired.actor, taken.actor...)
				repaired.public = append(repaired.public, taken.public...)
			}
		}
		return len(repaired.actor) > 0
	})
	return OpResult{Frames: repaired.public, Broadcast: repaired.public}
}
