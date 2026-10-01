/*
===========================================================================

coscontainer.go - validates the selected companion and owns its inventory transactions

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// Caller holds the division operation lock. Both COS identity and its bag
// are checked again inside the character authority door.
/*
================
applyCosContainerMove
================
*/
func (rt *Runtime) applyCosContainerMove(c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	result := failureResult(wire.ErrCodeInvalidRequest)
	committed := rt.deps.Update(c, "cos-container-move", func() bool {
		container, inv, valid := rt.ownedCOSContainer(c, q.CosGID)
		if !valid {
			return false
		}
		source, present := inv.At(q.SourceSlot)
		if !present {
			return false
		}
		if _, fault := inv.Transfer(q.SourceSlot, q.DestSlot, q.Quantity, rt.maxStackFor(source.TypeFlags, source.Codename)); fault != nil {
			return false
		}
		container.Rows = rowsFromInvItems(inv.Items())
		return true
	})
	if committed {
		// 697E80 case 10: unlike player type 0, no submove-count byte.
		result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.NewWriter(10).U8(1).U8(0x10).U32(q.CosGID).U8(q.SourceSlot).U8(q.DestSlot).U16(q.Quantity).Payload()}}}
	}
	return result
}

// Must run inside the character authority door. Used by storage and commerce.
/*
================
ownedCOSContainer
================
*/
func (rt *Runtime) ownedCOSContainer(c *enterworld.Character, gid uint32) (*domain.COSContainer, *inventory.Inventory, bool) {
	cos := c.CompanionByGID(gid)

	if c.DeletePending || cos == nil || !cos.Summoned || cos.CurrentHP == 0 || cos.Container == nil {
		return nil, nil, false
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil, nil, false
	}
	ref, found := refs.CharacterRefByCodename(cos.Codename)
	if !found || ref == nil {
		return nil, nil, false
	}
	if _, err := enterworld.BuildCOSRecord(cos, ref, rt.deps.ItemReferences()); err != nil {
		return nil, nil, false
	}
	inv, fault := inventory.NewContainer(invItemsFromRowsWithin(cos.Container.Rows, int64(cos.Container.Capacity)), cos.Container.Capacity)
	return cos.Container, inv, fault == nil
}
