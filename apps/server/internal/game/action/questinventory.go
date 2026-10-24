/*
===========================================================================

questinventory.go - atomic quest and voucher inventory consumption and grants

PlanQuestInventory and planInventoryItemsGrant apply removals and stack grants
against a detached bag copy before any character row or wire frame commits.

===========================================================================
*/

package action

import (
	"fmt"
	"reflect"
	"sort"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
PlanQuestInventory

PlanQuestInventory runs under the caller's character transaction. It never
mutates the character: all removals and grants must fit before rewards commit.
================
*/
func (rt *Runtime) PlanQuestInventory(c *enterworld.Character, consume, grant []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
	if c == nil || c.DeletePending {
		return nil, nil, fmt.Errorf("quest inventory: unavailable character")
	}
	before := invItemsFromBag(c)
	inv := inventory.New(before, inventory.BagEnd(c))
	for _, request := range consume {
		if request.Count == 0 {
			return nil, nil, fmt.Errorf("zero quest consumption")
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
				return nil, nil, fmt.Errorf("quest consumption: %v", fault)
			}
			remaining -= n
			if remaining == 0 {
				break
			}
		}
		if remaining != 0 {
			return nil, nil, fmt.Errorf("missing quest item %s", request.Codename)
		}
	}
	return rt.planInventoryGrant(c, before, inv, grant, nil)
}

/*
================
planInventoryGrant

planInventoryGrant resolves codename/count pairs into default inventory items
and delegates the stack placement and frame planning to planInventoryItemsGrant.
================
*/
func (rt *Runtime) planInventoryGrant(
	c *enterworld.Character,
	before []inventory.Item,
	inv *inventory.Inventory,
	grant []inventory.ItemAmount,
	silent map[uint8]bool,
) ([]enterworld.InventoryRow, []wire.Frame, error) {
	items := make([]inventory.Item, 0, len(grant))
	for _, request := range grant {
		if request.Count == 0 || request.Count > 65535 || rt.deps.ItemReferences() == nil {
			return nil, nil, fmt.Errorf("invalid quest grant %s", request.Codename)
		}
		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(request.Codename)
		if !ok || ref == nil {
			return nil, nil, fmt.Errorf("unresolved quest reward %s", request.Codename)
		}
		// A gold heap (ITEM_ETC_GOLD_*) only lies on the ground: pickup
		// credits the balance and never writes a row (#367). The shop tokens
		// that share its group are ordinary stackable items (wire.IsGoldHeap).
		if wire.IsGoldHeap(ref.TypeFlags()) {
			return nil, nil, fmt.Errorf("ground-only item %s cannot enter an inventory", request.Codename)
		}
		item := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: uint16(request.Count)}
		if ref.TypeIDs[1] == 1 {
			item.Durability = 100
			if ref.VarianceIntMin1c0 != nil {
				item.Durability = uint32(clampInt64(*ref.VarianceIntMin1c0, 1, 0xffffffff))
			}
		}
		items = append(items, item)
	}
	return rt.planInventoryItemsGrant(c, before, inv, items, silent)
}

/*
================
planInventoryItemsGrant

planInventoryItemsGrant adds prepared items to inv, already holding the
caller's removals from before, and plans the rows and frames for the whole
change. A slot in silent gets no delete frame: its caller announces the
removal itself, as the Gacha exchange does with a 0x3645 quantity of zero.
================
*/
func (rt *Runtime) planInventoryItemsGrant(
	c *enterworld.Character,
	before []inventory.Item,
	inv *inventory.Inventory,
	grant []inventory.Item,
	silent map[uint8]bool,
) ([]enterworld.InventoryRow, []wire.Frame, error) {
	for _, item := range grant {
		if item.Quantity == 0 || rt.deps.ItemReferences() == nil {
			return nil, nil, fmt.Errorf("invalid quest grant %s", item.Codename)
		}
		if wire.IsGoldHeap(item.TypeFlags) {
			return nil, nil, fmt.Errorf("ground-only item %s cannot enter an inventory", item.Codename)
		}
		for item.Quantity > 0 {
			result, fault := inv.GrantStack(item, rt.maxStackFor(item.TypeFlags, item.Codename))
			if fault != nil {
				if fault.Code == wire.ErrCodeStorageFull {
					// B-11: log the bag state behind a full-bag grant
					// refusal so a repeat separates "genuinely full"
					// from a client that lost earlier grant answers.
					log.WithField("character", c.Name).
						WithField("occupied", inv.Len()).
						WithField("capacity", inventory.BagEnd(c)).
						Warn("quest inventory: grant refused, bag full")
				}
				return nil, nil, fmt.Errorf("quest reward inventory: %w", fault)
			}
			item.Quantity = result.GroundRemainder
		}
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
