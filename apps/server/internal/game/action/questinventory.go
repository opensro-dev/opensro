/*
===========================================================================
questinventory.go - shared inventory plans for quests, pet gear and gacha receipts.
===========================================================================
*/
package action

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"sort"
)

/*
================
PlanQuestInventory

Plans removals and grants before the caller commits any reward.
================
*/
func (rt *Runtime) PlanQuestInventory(c *enterworld.Character, consume, grant []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
	if c == nil || c.DeletePending {
		return nil, nil, fmt.Errorf("quest inventory: unavailable character")
	}
	return rt.planBagChange(c, func(inv *inventory.Inventory) error {
		return rt.questInventoryChange(inv, consume, grant)
	})
}

/*
================
questInventoryChange

The quest plan's bag mutation: consumption first, then the rewards.
================
*/
func (rt *Runtime) questInventoryChange(inv *inventory.Inventory, consume, grant []inventory.ItemAmount) error {
	for _, request := range consume {
		if request.Count == 0 {
			return fmt.Errorf("zero quest consumption")
		}
		remaining := request.Count
		rows := inv.Items()
		sort.Slice(rows, func(i, j int) bool { return rows[i].Slot < rows[j].Slot })
		for _, row := range rows {
			if row.Slot < inventory.EquipmentSlotEnd || row.Codename != request.Codename {
				continue
			}
			n := uint32(row.Quantity)
			if n > remaining {
				n = remaining
			}
			if n == 0 {
				continue
			}
			if _, fault := inv.DropQuantity(row.Slot, uint16(n)); fault != nil {
				return fmt.Errorf("quest consumption: %v", fault)
			}
			remaining -= n
			if remaining == 0 {
				break
			}
		}
		if remaining != 0 {
			return fmt.Errorf("missing quest item %s", request.Codename)
		}
	}
	for _, request := range grant {
		if request.Count == 0 || request.Count > 65535 || rt.deps.ItemReferences() == nil {
			return fmt.Errorf("invalid quest grant %s", request.Codename)
		}
		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(request.Codename)
		if !ok || ref == nil {
			return fmt.Errorf("unresolved quest reward %s", request.Codename)
		}
		// A gold heap (the gold band, ITEM_ETC_GOLD_*) only lies on the
		// ground: pickup credits the balance and never writes a row. A row
		// would carry a gold type word the inventory body cannot encode,
		// and the client fails world entry on it for good (#367).
		if wire.IsGoldBand(ref.TypeFlags()) {
			return fmt.Errorf("ground-only item %s cannot enter an inventory", request.Codename)
		}
		item := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: uint16(request.Count)}
		if ref.TypeIDs[1] == 1 {
			item.Durability = 100
			if ref.VarianceIntMin1c0 != nil {
				item.Durability = uint32(clampInt64(*ref.VarianceIntMin1c0, 1, 0xffffffff))
			}
		}
		for item.Quantity > 0 {
			result, fault := inv.GrantStack(item, rt.maxStackFor(item.TypeFlags, item.Codename))
			if fault != nil {
				return fmt.Errorf("quest reward inventory: %w", fault)
			}
			item.Quantity = result.GroundRemainder
		}
	}
	return nil
}

/*
================
planBagChange

Applies change to a copy of the bag and plans the resulting rows and the
client's receipts (references, then per-slot delete and grant frames). The
caller owns the transaction and commits the rows; a failed change plans
nothing.
================
*/
func (rt *Runtime) planBagChange(c *enterworld.Character, change func(inv *inventory.Inventory) error) ([]enterworld.InventoryRow, []wire.Frame, error) {
	if c == nil || c.DeletePending {
		return nil, nil, fmt.Errorf("bag change: unavailable character")
	}
	before := invItemsFromBag(c)
	inv := inventory.New(before, inventory.BagEnd(c))
	if err := change(inv); err != nil {
		return nil, nil, err
	}
	return rt.planInventoryGrant(c, before, inv, nil, nil)
}

/*
================
planInventoryGrant

Gacha suppresses its own removal receipt; pet/quest changes keep ordinary receipts.
================
*/
func (rt *Runtime) planInventoryGrant(c *enterworld.Character, before []inventory.Item, inv *inventory.Inventory, grant []inventory.ItemAmount, silent map[uint8]bool) ([]enterworld.InventoryRow, []wire.Frame, error) {
	if err := rt.questInventoryChange(inv, nil, grant); err != nil {
		return nil, nil, err
	}
	after := inv.Items()
	frames := []wire.Frame{rt.commerceReferences(after, before)}
	previous := map[uint8]inventory.Item{}
	next := map[uint8]inventory.Item{}
	for _, row := range before {
		previous[row.Slot] = row
	}
	for _, row := range after {
		next[row.Slot] = row
	}
	// Stable slot order; replacement is delete then grant when identity changes.
	for slot := inventory.EquipmentSlotEnd; slot < inventory.BagEnd(c); slot++ {
		old, had := previous[slot]
		row, has := next[slot]
		if had && !silent[slot] && (!has || old.RefObjID != row.RefObjID || (!inventory.IsEtcStackableTypeFlags(row.TypeFlags) && !reflect.DeepEqual(old, row))) {
			frames = append(frames, wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: []byte{1, 15, slot, 0}})
		}
		if has && (!had || !reflect.DeepEqual(old, row)) {
			payload := wire.NewWriter(4 + row.Body().EncodedSize()).U8(1).U8(14).U8(slot).U8(0).Bytes(row.Body().Encode()).Payload()
			frames = append(frames, wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: payload})
		}
	}
	return alchemyRows(c.MissionInventory, before, after), frames, nil
}
