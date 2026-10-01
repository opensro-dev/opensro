/*
===========================================================================

costransfer.go - owns costransfer behavior and its checked data boundaries

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// Both inventories belong to the same character authority transaction. Caller
// holds the division action lock; failed validation changes neither inventory.
/*
================
applyCosTransfer
================
*/
func (rt *Runtime) applyCosTransfer(c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	var questFrames []wire.Frame
	committed := rt.deps.Update(c, "player-cos-transfer", func() bool {
		bag, cos, ok := rt.ownedCOSContainer(c, q.CosGID)
		if !ok {
			return false
		}
		player := inventory.New(invItemsFromRows(c.MissionInventory))
		source, dest := player, cos
		if q.MovementType == wire.MoveTypeCosToPlayer {
			source, dest = cos, player
		}
		item, present := source.At(q.SourceSlot)
		if !present {
			return false
		}
		if fault := source.TransferWholeTo(dest, q.SourceSlot, q.DestSlot, rt.maxStackFor(item.TypeFlags, item.Codename)); fault != nil {
			return false
		}
		// Check publication representability before committing the stronger COS
		// record contract. This excludes bodies the durable row cannot preserve.
		candidate := *c.CompanionByGID(q.CosGID)
		candidate.Container = &domain.COSContainer{Capacity: bag.Capacity, Rows: rowsFromInvItems(cos.Items())}
		refs := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
		ref, found := refs.CharacterRefByCodename(candidate.Codename)
		if !found {
			return false
		}
		if _, err := enterworld.BuildCOSRecord(&candidate, ref, rt.deps.ItemReferences()); err != nil {
			return false
		}
		// Rebuilding the player inventory detaches its summoner records. Commit
		// the bag through that new canonical pet, not the old row's pointer.
		next := *c
		next.MissionInventory = rowsFromInvItems(player.Items())
		retained := next.CompanionByGID(q.CosGID)
		if retained == nil {
			return false
		}
		retained.Container = candidate.Container
		c.MissionInventory = next.MissionInventory
		questFrames = rt.updateQuestInventory(c)
		return true
	})
	if !committed {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	return OpResult{Frames: append([]wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.NewWriter(8).U8(1).U8(q.MovementType).U32(q.CosGID).U8(q.SourceSlot).U8(q.DestSlot).Payload()}}, questFrames...)}
}
