/*
===========================================================================

deathdrop.go - which inventory item a death drops

CGObjPC_ApplyDeathPenalty (4E6980, 4E6BF6..4E6CB0) rolls the drop chance
and asks the inventory for a slot
(CGStorageOP_Inventory_RandomSelectSlotForDeathPenalty 4BB460); the item
then leaves through the ordinary ground drop (move 0x17 re-typed to 7 by
CGObjPC_ItemMove_GroundDropOrDeathDrop 5264E0). Every rand() draw is the
native's, in its order.

===========================================================================
*/

package pk

import "fmt"

const (
	// equipmentSlots is the 13 equipped slots 0..12; the bag starts at 13.
	equipmentSlots = 13
	// The weapon (6) and the job suit / cape (8) never drop; slot 7
	// rerolls when it holds ammunition (CGObj_IsAmmunition, TID 3/3/4).
	weaponSlot     = 6
	secondarySlot  = 7
	jobSlot        = 8
	armourRerolls  = 6
	dropRollModulo = 101
)

/*
================
DropSlot

One inventory slot as the picker sees it. Droppable is
CGItem_CanDropOnDeathPenalty (4B7CF0).
================
*/
type DropSlot struct {
	Occupied   bool
	Droppable  bool
	Ammunition bool
}

/*
================
DropRoll

The rand() domain (0..32767).
================
*/
type DropRoll func() (uint32, error)

/*
================
RollsDrop

4E6BF6: rand() % 101 <= the penalty's chance.
================
*/
func RollsDrop(penalty uint32, roll DropRoll) (bool, error) {
	value, err := roll()
	if err != nil {
		return false, err
	}
	return int32(value%dropRollModulo) <= DropChance(penalty), nil
}

/*
================
SelectDropSlot

4BB460. A murderer (penalty > 0; a guild-war death passes 0) first rolls
an equipped slot; when that slot holds nothing droppable, or for anyone
else, the nth occupied bag slot is taken, n = rand() % occupied + 1, and
dropped only when that item may drop. False: nothing drops.
================
*/
func SelectDropSlot(slots []DropSlot, penalty uint32, roll DropRoll) (int, bool, error) {
	if len(slots) <= equipmentSlots {
		return 0, false, fmt.Errorf("pk: inventory of %d slots has no bag", len(slots))
	}
	if penalty > 0 {
		value, err := roll()
		if err != nil {
			return 0, false, err
		}
		slot := int(value % equipmentSlots)
		if slot == weaponSlot || slot == jobSlot || slot == secondarySlot && slots[secondarySlot].Occupied && slots[secondarySlot].Ammunition {
			value, err = roll()
			if err != nil {
				return 0, false, err
			}
			slot = int(value % armourRerolls)
		}
		if slots[slot].Occupied && slots[slot].Droppable {
			return slot, true, nil
		}
	}
	occupied := 0
	for _, s := range slots[equipmentSlots:] {
		if s.Occupied {
			occupied++
		}
	}
	if occupied == 0 {
		return 0, false, nil
	}
	value, err := roll()
	if err != nil {
		return 0, false, err
	}
	want := int(value%uint32(occupied)) + 1
	seen := 0
	for i := equipmentSlots; i < len(slots); i++ {
		if !slots[i].Occupied {
			continue
		}
		if seen++; seen == want {
			return i, slots[i].Droppable, nil
		}
	}
	return 0, false, nil
}
