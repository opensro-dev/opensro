package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// 594980: avatar records occupy independent storage slots; their TID4 selects
// the visible socket. Occupied costume sockets refuse replacement.
func (rt *Runtime) applyAvatarTransfer(divisionID string, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
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
		player := bagOf(c)
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
			if q.DestSlot < inventory.EquipmentSlotEnd || q.DestSlot >= inventory.BagEnd(c) {
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

		nextBag := rowsFromInvItems(player.Items())
		nextAvatars := &domain.AvatarInventory{Capacity: 4, Rows: rowsFromInvItems(avatars.Items())}
		// 50F1F0 installs or removes the avatar's contributions (4E3760 /
		// 4E3860) and the actor's stats refresh, as for equipment: the
		// blessed options count only while the avatar is worn. Derived on
		// the next snapshot so a broken record refuses before anything moves.
		next := c.Snapshot()
		next.MissionInventory, next.AvatarInventory = nextBag, nextAvatars
		display, err := rt.PlayerBaseStats(divisionID, next)
		if err != nil {
			log.Warnf("action: avatar move %d->%d refused - %v", q.SourceSlot, q.DestSlot, err)
			return false
		}
		statFrame := wire.Frame{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(next, display)}
		c.MissionInventory = nextBag
		c.AvatarInventory = nextAvatars
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
		// Viewers see the dress change as the owner does (applyInventoryMove),
		// after the reference for an avatar they may never have been sent.
		// A copy: the owner's stats frame is appended to Frames below.
		result.Broadcast = append([]wire.Frame(nil), result.Frames[1:]...)
		if equip {
			references := rt.itemReferenceFrames([]inventory.Item{item})
			result.Broadcast = append(references, result.Broadcast...)
		}
		// The stats are the owner's alone, after the move and visuals.
		hp, mp := rt.clampStoredGaugeToKeeper(divisionID, c)
		result.Frames = append(result.Frames, statFrame)
		result.Frames = append(result.Frames, rt.gaugeDropFrames(divisionID, c, hp, mp, false)...)
		return true
	})
	return result
}
