/*
===========================================================================

coslease.go - renew the retained pickup companion owned by a summoner item

Calendar expiry survives dismissal, storage and downtime. Renewal changes
that same record; it cannot manufacture a companion or summon it implicitly.

===========================================================================
*/
package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const (
	companionLeaseSecondsPerMinute       = 60
	companionLeaseWrongTarget      uint8 = 0xb8
)

/*
================
companionLeaseUse
================
*/
type companionLeaseUse struct {
	character *enterworld.Character
	ref       *enterworld.ItemRef
	row       int
	request   wire.ItemUseRequest
	tail      []byte
	nowUnix   int64
}

/*
================
extendCompanionLease

49D8F0 reads one owned inventory slot and accepts only an existing pickup
record. 49DA27..49DA57 adds Param1 minutes to max(expiry, now), and 49DAC1
restores the alive bit without creating a world actor.
================
*/
func (rt *Runtime) extendCompanionLease(use companionLeaseUse, result *OpResult) bool {
	*result = itemUseFailure(companionLeaseWrongTarget)
	if len(use.tail) != 1 || !inventory.IsBagSlot(use.tail[0]) {
		return false
	}
	var selected *enterworld.InventoryRow
	for i := range use.character.MissionInventory {
		row := &use.character.MissionInventory[i]
		if row.Slot == int64(use.tail[0]) {
			if selected != nil {
				return false
			}
			selected = row
		}
	}
	if selected == nil || selected.StackCount != 1 || !wire.IsCosSummoner(selected.TypeFlags) || selected.Summon == nil {
		return false
	}
	ref, valid := rt.deps.ItemReferences().ItemRefByCodename(selected.Codename)
	if !valid || ref == nil || ref.RefObjID != selected.RefObjID || ref.TypeFlags() != selected.TypeFlags || ref.TypeIDs[3] != 2 {
		return false
	}
	pet := selected.Summon
	cosRef, valid := rt.cosReference(pet)
	if !valid || cosRef.TidWord>>11 != 4 {
		return false
	}
	minutes, present := use.ref.NativeFields.Lookup("itemParam1_29c")
	if !present || math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes <= 0 || minutes > math.MaxInt32/companionLeaseSecondsPerMinute || math.Trunc(minutes) != minutes {
		return false
	}
	seconds := int64(minutes) * companionLeaseSecondsPerMinute
	base := max(pet.RentalExpiresAtUnix, use.nowUnix)
	if base < 0 || base > math.MaxInt64-seconds {
		return false
	}
	pet.RentalExpiresAtUnix = base + seconds
	pet.StateFlags |= 1
	pet.RefreshRentalTimes(use.nowUnix)
	remaining := rt.consumeItemUseRow(use.character, use.row)
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	result.Frames = append(result.Frames, companionItemStateFrames(use.character, pet)...)
	result.Frames = append(result.Frames, rt.updateQuestInventory(use.character)...)
	return true
}
