package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// 594980: avatar records occupy independent storage slots; their TID4 selects
// the visible socket. Occupied costume sockets refuse replacement.
func (rt *Runtime) applyAvatarTransfer(c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	result := failureResult(wire.ErrCodeInvalidRequest)
	rt.deps.Update(c, "avatar-transfer", func() bool {
		if c.DeletePending {
			return false
		}
		rows := []enterworld.InventoryRow(nil)
		if c.AvatarInventory != nil {
			rows = c.AvatarInventory.Rows
		}
		avatars, fault := inventory.NewContainer(invItemsFromRowsWithin(rows, 4), 4)
		if fault != nil {
			return false
		}
		player := inventory.New(invItemsFromRows(c.MissionInventory))
		source, dest := player, avatars
		equip := q.MovementType == wire.MoveTypePlayerToAvatar
		if !equip {
			source, dest = avatars, player
		}
		item, ok := source.At(q.SourceSlot)
		if !ok || item.TypeFlags&0x7fe != 0x6ac || item.TypeFlags>>11 < 1 || item.TypeFlags>>11 > 4 {
			return false
		}
		if equip {
			if _, occupied := dest.At(q.DestSlot); occupied {
				return false
			}
		} else {
			// Retail 525AC0/525B42 owns destination selection, ignoring the
			// requested bag cell. Reserve it before finding an attachment cell.
			if q.DestSlot < inventory.EquipmentSlotEnd || q.DestSlot >= inventory.BagSlotEnd {
				return false
			}
			var free bool
			q.DestSlot, free = player.FirstFreeBagSlot()
			if !free {
				result = failureResult(wire.ErrCodeStorageFull)
				return false
			}
		}
		if equip {
			dress := false
			for _, worn := range avatars.Items() {
				if worn.TypeFlags>>11 == item.TypeFlags>>11 {
					return false
				}
				if worn.TypeFlags>>11 == 2 {
					dress = true
				}
			}
			if item.TypeFlags>>11 == 3 && !dress {
				return false
			}
			requirements := equipRequirements(rt.deps.ItemReferences(), c, rt.FortressGuildRole)
			if requirements != nil {
				if requirements.PreExclusivity(item) != nil || requirements.PostExclusivity(item) != nil {
					return false
				}
			}
		}
		// All mutations below are on private containers. Never publish a dress
		// removal unless its dependent attachment has somewhere to go too.
		if fault := source.TransferWholeTo(dest, q.SourceSlot, q.DestSlot, 1); fault != nil {
			return false
		}
		var attachment *inventory.Item
		var attachmentDestination uint8
		if !equip && item.TypeFlags>>11 == 2 {
			for _, worn := range avatars.Items() {
				if worn.TypeFlags>>11 != 3 {
					continue
				}
				free, ok := player.FirstFreeBagSlot()
				if !ok {
					result = failureResult(wire.ErrCodeStorageFull)
					return false
				}
				if fault := avatars.TransferWholeTo(player, worn.Slot, free, 1); fault != nil {
					return false
				}
				attachment, attachmentDestination = &worn, free
				break
			}
		}

		c.MissionInventory = rowsFromInvItems(player.Items())
		c.AvatarInventory = &domain.AvatarInventory{Capacity: 4, Rows: rowsFromInvItems(avatars.Items())}
		visual := wire.UnequipVisualFrame(wire.UnequipVisual{Gid: enterworld.ObjectIDForCharacter(c), Slot: q.SourceSlot, RefObjID: item.RefObjID})
		if equip {
			visual = wire.EquipVisualFrame(wire.EquipVisual{Gid: enterworld.ObjectIDForCharacter(c), RefObjID: item.RefObjID, TypeFlags: item.TypeFlags, OptLevel: item.Plus})
		}
		count := uint8(0)
		if attachment != nil {
			count = 1
		}
		payload := wire.NewWriter(12).U8(1).U8(q.MovementType).U8(q.SourceSlot).U8(q.DestSlot).U16(1).U8(count)
		if attachment != nil {
			payload.U8(wire.MoveTypeAvatarToPlayer).U8(attachment.Slot).U8(attachmentDestination).U16(1)
		}
		result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: payload.Payload()}}}
		// 525C50 removes the attachment before the main dress continuation.
		if attachment != nil {
			result.Frames = append(result.Frames, wire.UnequipVisualFrame(wire.UnequipVisual{Gid: enterworld.ObjectIDForCharacter(c), Slot: attachment.Slot, RefObjID: attachment.RefObjID}))
		}
		result.Frames = append(result.Frames, visual)
		// Viewers see the dress change as the owner does (applyInventoryMove).
		result.Broadcast = result.Frames[1:]
		return true
	})
	return result
}
