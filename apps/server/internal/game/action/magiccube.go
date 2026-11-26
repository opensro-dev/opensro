/*
===========================================================================

magiccube.go - Magic Cube deposit and withdraw (0x706D 0x27 / 0x28)

Port-only, not native (#3906). v1.150 has no Magic Cube. Behind
SRO_MAGIC_CUBE (Runtime.MagicCube). Push: bag elixir → cube linked
storage. Pop: linked storage → bag. Layout pinned on the v1.696 client
jump table (client 0x7f7be0 / client 0x7f7b90); full refusal is notice 01:8A.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/magiccube"
)

/*
================
applyMagicCubeMove

Deposit (0x27) or withdraw (0x28). Off (MagicCube false) answers the
generic invalid-request notice, as if the movement type were unknown.
================
*/
func (rt *Runtime) applyMagicCubeMove(c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	if !rt.MagicCube {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	switch q.MovementType {
	case wire.MoveTypePushIntoMagicCube:
		return rt.applyMagicCubePush(c, q)
	case wire.MoveTypePopFromMagicCube:
		return rt.applyMagicCubePop(c, q)
	}
	return failureResult(wire.ErrCodeInvalidRequest)
}

/*
================
applyMagicCubePush

src = elixir bag slot, dst = cube bag slot, qty = count to deposit.
================
*/
func (rt *Runtime) applyMagicCubePush(c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	result := failureResult(wire.ErrCodeInvalidRequest)
	if q.Quantity == 0 {
		return result
	}
	committed := rt.deps.Update(c, "magic-cube-push", func() bool {
		if c.DeletePending {
			return false
		}
		inv := bagOf(c)
		elixir, ok := inv.At(q.SourceSlot)
		if !ok || elixir.Quantity < q.Quantity {
			return false
		}
		cube, ok := inv.At(q.DestSlot)
		if !ok || !magiccube.IsCube(cube.TypeFlags) {
			return false
		}
		cap, filter, ok := rt.magicCubeParams(cube.Codename)
		if !ok || !magiccube.MatchesFilter(filter, elixir.Codename) {
			return false
		}
		storage := ensureLinkedStorage(cube.LinkedStorage, cap)
		container, fault := inventory.NewContainer(
			invItemsFromRowsWithin(storage.Rows, int64(storage.Capacity)),
			uint8(storage.Capacity),
		)
		if fault != nil {
			return false
		}
		piece := elixir
		piece.Quantity = q.Quantity
		piece.LinkedStorage = nil
		piece.Summon = nil
		stackCap := rt.maxStackFor(elixir.TypeFlags, elixir.Codename)
		grant, fault := container.GrantStack(piece, stackCap)
		if fault != nil {
			result = failureResult(wire.ErrCodeMagicCubeFull)
			return false
		}
		if grant.GroundRemainder > 0 {
			// Cube capacity left a remainder; treat as full rather than
			// partial-deposit (the client sends an all-or-nothing qty).
			result = failureResult(wire.ErrCodeMagicCubeFull)
			return false
		}
		if _, fault = inv.DropQuantity(q.SourceSlot, q.Quantity); fault != nil {
			result = failureResult(fault.Code)
			return false
		}
		storage.Rows = rowsFromInvItems(container.Items())
		cube.LinkedStorage = storage
		cube.Data = linkedStoredCount(storage)
		c.MissionInventory = rowsFromInvItems(replaceBagItem(inv, cube))
		result = OpResult{Frames: []wire.Frame{{
			Opcode:  wire.OpItemMoveResponse,
			Payload: wire.EncodePushIntoMagicCubeResult(q.SourceSlot, q.DestSlot, q.Quantity),
		}}}
		return true
	})
	if !committed {
		return result
	}
	return result
}

/*
================
applyMagicCubePop

src = cube bag slot, dst = receive bag slot, RefObjID = elixir identity.
================
*/
func (rt *Runtime) applyMagicCubePop(c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	result := failureResult(wire.ErrCodeInvalidRequest)
	if q.Quantity == 0 || q.RefObjID == 0 {
		return result
	}
	committed := rt.deps.Update(c, "magic-cube-pop", func() bool {
		if c.DeletePending {
			return false
		}
		inv := bagOf(c)
		cube, ok := inv.At(q.SourceSlot)
		if !ok || !magiccube.IsCube(cube.TypeFlags) || cube.LinkedStorage == nil {
			return false
		}
		storage := ensureLinkedStorage(cube.LinkedStorage, int(cube.LinkedStorage.Capacity))
		container, fault := inventory.NewContainer(
			invItemsFromRowsWithin(storage.Rows, int64(storage.Capacity)),
			uint8(storage.Capacity),
		)
		if fault != nil {
			return false
		}
		srcSlot, found := linkedSlotByRef(container, q.RefObjID)
		if !found {
			return false
		}
		piece, fault := container.DropQuantity(srcSlot, q.Quantity)
		if fault != nil {
			return false
		}
		items := inv.Items()
		if existing, present := inv.At(q.DestSlot); present {
			stackCap := rt.maxStackFor(piece.TypeFlags, piece.Codename)
			if existing.RefObjID != piece.RefObjID || !inventory.IsEtcStackableTypeFlags(piece.TypeFlags) {
				return false
			}
			if stackCap > 0 && existing.Quantity+q.Quantity > stackCap {
				return false
			}
			existing.Quantity += q.Quantity
			items = replaceItemIn(items, existing)
		} else {
			if q.DestSlot < inventory.EquipmentSlotEnd || int(q.DestSlot) >= int(inv.BagEnd()) {
				return false
			}
			piece.Slot = q.DestSlot
			items = append(items, piece)
		}
		storage.Rows = rowsFromInvItems(container.Items())
		cube.LinkedStorage = storage
		cube.Data = linkedStoredCount(storage)
		items = replaceItemIn(items, cube)
		c.MissionInventory = rowsFromInvItems(items)
		result = OpResult{Frames: []wire.Frame{{
			Opcode:  wire.OpItemMoveResponse,
			Payload: wire.EncodePopFromMagicCubeResult(q.RefObjID, q.SourceSlot, q.DestSlot, q.Quantity),
		}}}
		return true
	})
	if !committed {
		return result
	}
	return result
}

/*
================
magicCubeParams

Capacity and filter from the cube's itemdata Param9 / Param10.
================
*/
func (rt *Runtime) magicCubeParams(codename string) (capacity, filter int, ok bool) {
	refs := rt.deps.ItemReferences()
	if refs == nil {
		return 0, 0, false
	}
	ref, found := refs.ItemRefByCodename(codename)
	if !found || ref == nil {
		return 0, 0, false
	}
	capVal, _ := ref.NativeFields.Lookup(magiccube.ParamCapacityKey)
	filVal, _ := ref.NativeFields.Lookup(magiccube.ParamFilterKey)
	capacity = magiccube.Capacity(capVal)
	filter = magiccube.Filter(filVal)
	return capacity, filter, capacity > 0 && filter > 0
}

func ensureLinkedStorage(existing *domain.COSContainer, capacity int) *domain.COSContainer {
	cap := uint8(capacity)
	if existing != nil {
		out := domain.CloneCOSContainer(existing)
		if cap > 0 && out.Capacity < cap {
			out.Capacity = cap
		}
		if out.Capacity < 1 {
			out.Capacity = cap
		}
		return out
	}
	return &domain.COSContainer{Capacity: cap}
}

func linkedStoredCount(storage *domain.COSContainer) uint32 {
	if storage == nil {
		return 0
	}
	var total uint32
	for _, row := range storage.Rows {
		if row.StackCount > 0 {
			total += uint32(row.StackCount)
		}
	}
	return total
}

func linkedSlotByRef(container *inventory.Inventory, refObjID uint32) (uint8, bool) {
	for _, row := range container.Items() {
		if row.RefObjID == refObjID && row.Quantity > 0 {
			return row.Slot, true
		}
	}
	return 0, false
}

func replaceBagItem(inv *inventory.Inventory, item inventory.Item) []inventory.Item {
	return replaceItemIn(inv.Items(), item)
}

func replaceItemIn(items []inventory.Item, item inventory.Item) []inventory.Item {
	out := make([]inventory.Item, len(items))
	copy(out, items)
	for i := range out {
		if out[i].Slot == item.Slot {
			out[i] = item
			return out
		}
	}
	return out
}
