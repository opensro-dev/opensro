package action

import (
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

func ammoFamily(item inventory.Item) uint8 {
	if item.TypeFlags&0x7fe == 0x26c {
		return uint8(item.TypeFlags >> 11)
	}
	return 0
}
func weaponAmmo(item inventory.Item) uint8 {
	if item.TypeFlags&0x7fe != 0x32c {
		return 0
	}
	switch item.TypeFlags >> 11 {
	case 6:
		return 1
	case 12:
		return 2
	}
	return 0
}
func (rt *Runtime) twoHanded(item inventory.Item) bool {
	if rt.deps.ItemReferences() == nil {
		return false
	}
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	return ok && ref != nil && ref.NativeFields.Get("twoHanded") == 1
}

// Native 525380 rejects ammo with absent/mismatched main hand. 525237 and
// 52531D append companion moves to the same result. Work on the caller's
// temporary inventory; a failed capacity check must never commit half a swap.
func (rt *Runtime) completeEquipmentPair(inv *inventory.Inventory, source, destination uint8) ([]wire.SubMove, *inventory.Fault) {
	if source != 6 && source != 7 && destination != 6 && destination != 7 {
		return nil, nil
	}
	weapon, hasWeapon := inv.At(6)
	off, hasOff := inv.At(7)
	var moves []wire.SubMove
	move := func(from, to uint8) *inventory.Fault {
		if _, fault := inv.Transfer(from, to, 0, 1); fault != nil {
			return fault
		}
		moves = append(moves, wire.SubMove{SourceSlot: from, DestSlot: to})
		return nil
	}
	stash := func(slot uint8) *inventory.Fault {
		free, ok := inv.FirstFreeBagSlot()
		if !ok {
			return inventory.NewFault(wire.ErrCodeStorageFull, "companionEquipmentNeedsBagSlot")
		}
		return move(slot, free)
	}
	// An item newly seated in 7 (including swap-back) is an explicit request.
	if destination == 7 || source == 7 {
		if hasOff && ammoFamily(off) != 0 {
			if !hasWeapon || ammoFamily(off) != weaponAmmo(weapon) {
				return nil, inventory.NewFault(wire.ErrCodeCantEquip, "ammunitionWeaponMismatch")
			}
		} else if hasOff && hasWeapon && rt.twoHanded(weapon) {
			if fault := stash(6); fault != nil {
				return nil, fault
			}
		}
		return moves, nil
	}
	// Weapon changes keep a compatible off-hand, or return it to the bag.
	wanted := weaponAmmo(weapon)
	if wanted != 0 {
		if hasOff && ammoFamily(off) == wanted {
			return moves, nil
		}
		// Native searches bag slots in order for the matching ammunition family.
		for slot := inventory.EquipmentSlotEnd; slot < inv.BagEnd(); slot++ {
			item, ok := inv.At(slot)
			if !ok || ammoFamily(item) != wanted {
				continue
			}
			if fault := move(slot, 7); fault != nil {
				return nil, fault
			}
			return moves, nil
		}
	}
	if hasOff && (ammoFamily(off) != 0 || hasWeapon && rt.twoHanded(weapon)) {
		if fault := stash(7); fault != nil {
			return nil, fault
		}
	}
	return moves, nil
}
