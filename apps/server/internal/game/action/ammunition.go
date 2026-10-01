/*
===========================================================================

ammunition.go - Package action.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
ammunitionTid4ForWeapon

ammunitionTid4ForWeapon is the native equipment-family join. Item TID4 6
is a Chinese bow and consumes arrow TID 3.3.4.1; weapon TID4 12 is a
European crossbow and consumes bolt TID 3.3.4.2. Both live in equipment
socket 7, the same socket the client refreshes through opcode 0x3752.
================
*/
func ammunitionTid4ForWeapon(weaponKind uint8) (int64, bool) {
	switch weaponKind {
	case 6:
		return 1, true
	case 12:
		return 2, true
	default:
		return 0, false
	}
}

/*
================
ammunitionSpent

ammunitionSpent is SkillAction_Projectile's debit (585AF0 / 585FB6): a
skill spends its cnsm count once per mc impact; a basic shot spends one.
planEquippedAmmunition floors the stack at 0, so a last arrow still pays
for a shot that asks for more.
================
*/
func ammunitionSpent(skill enterworld.SkillRow, advanced bool) int64 {
	if !advanced {
		return 1
	}
	return int64(skill.Ammunition.Count) * int64(max(1, skill.Attack.ImpactCount))
}

/*
================
ammunitionDebit
================
*/
type ammunitionDebit struct {
	index     int
	remaining int64
}

/*
================
planEquippedAmmunition

planEquippedAmmunition validates the live equipment row without changing
it. Damage is admitted after this plan is built; applying the plan is then
the final, non-refusing step of the combined character/monster transition.
This ordering matters because enterworld.Update deliberately does not roll
a callback back when it returns false.
================
*/
func (rt *Runtime) planEquippedAmmunition(character *enterworld.Character, weaponKind uint8, spent int64) (ammunitionDebit, bool) {
	wantTid4, required := ammunitionTid4ForWeapon(weaponKind)
	if !required {
		return ammunitionDebit{index: -1}, true
	}
	items := rt.deps.ItemReferences()
	if character == nil || items == nil {
		return ammunitionDebit{}, false
	}
	for index := range character.MissionInventory {
		row := character.MissionInventory[index]
		if row.Slot != int64(inventory.SocketShield) || row.StackCount < 1 || row.StackCount > 0xffff {
			continue
		}
		ref, ok := items.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || ref.RefObjID != row.RefObjID ||
			ref.TypeIDs != [4]int64{3, 3, 4, wantTid4} ||
			(row.TypeFlags != 0 && row.TypeFlags != ref.TypeFlags()) {
			return ammunitionDebit{}, false
		}
		return ammunitionDebit{index: index, remaining: max(0, row.StackCount-max(1, spent))}, true
	}
	return ammunitionDebit{}, false
}

/*
================
ammunitionResult

What a shot leaves in equipment socket 7: the absolute stack count opcode
0x3752 carries, and the bag stack AutoReloadMagazine moved in, if any.
================
*/
type ammunitionResult struct {
	count  uint16
	reload *ammunitionReload
}

/*
================
ammunitionReload

One AutoReloadMagazine move: a whole bag stack into socket 7.
================
*/
type ammunitionReload struct {
	from     uint8
	quantity uint16
}

/*
================
applyAmmunitionDebit

The already-validated, non-refusing tail of the attack commit. When the
equipped stack runs out, CGObjPC_ConsumeAmmo (4EC290) calls
CGObjPC_AutoReloadMagazine (4EC340): CGStorage_FindItemHasSameTID searches the
bag from slot 13 for a stack of the same type id and runs an ordinary
inventory move of the whole stack into socket 7. Without one, socket 7 stays
empty and later shots are refused (0x300E).
================
*/
func applyAmmunitionDebit(character *enterworld.Character, debit ammunitionDebit) ammunitionResult {
	if debit.remaining != 0 {
		character.MissionInventory[debit.index].StackCount = debit.remaining
		return ammunitionResult{count: uint16(debit.remaining)}
	}
	spent := character.MissionInventory[debit.index]
	character.MissionInventory = append(
		character.MissionInventory[:debit.index:debit.index],
		character.MissionInventory[debit.index+1:]...,
	)
	reload := reloadMagazine(character, spent.TypeFlags)
	if reload == nil {
		return ammunitionResult{}
	}
	return ammunitionResult{count: reload.quantity, reload: reload}
}

/*
================
reloadMagazine

Moves the lowest bag stack with the spent ammunition's type id (the packed
TID, not the exact item) into socket 7, or returns nil when none exists.
================
*/
func reloadMagazine(character *enterworld.Character, typeFlags uint16) *ammunitionReload {
	best := -1
	for index, row := range character.MissionInventory {
		if row.Slot < int64(inventory.EquipmentSlotEnd) || row.Slot >= int64(inventory.BagSlotEnd) ||
			row.TypeFlags != typeFlags || row.StackCount < 1 || row.StackCount > 0xffff {
			continue
		}
		if best < 0 || row.Slot < character.MissionInventory[best].Slot {
			best = index
		}
	}
	if best < 0 {
		return nil
	}
	row := &character.MissionInventory[best]
	reload := &ammunitionReload{from: uint8(row.Slot), quantity: uint16(row.StackCount)}
	row.Slot = int64(inventory.SocketShield)
	return reload
}

/*
================
ammunitionFrames

The actor's frames for a shot's ammunition: the reload's inventory move
first, then 0x3752 with the socket's new absolute count. A zero count clears
the socket on the client (596510), so it must follow the move, not precede it.
================
*/
func ammunitionFrames(result ammunitionResult) []wire.Frame {
	frames := make([]wire.Frame, 0, 2)
	if result.reload != nil {
		frames = append(frames, wire.Frame{
			Opcode:  wire.OpItemMoveResponse,
			Payload: wire.EncodeInventoryMoveResult(result.reload.from, uint8(inventory.SocketShield), result.reload.quantity, nil),
		})
	}
	return append(frames, wire.AvatarInventorySlot7StackCountFrame(result.count))
}
