package action

import (
	"fmt"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"sort"
)

// PlanQuestInventory runs under the caller's character transaction. It never
// mutates the character: all removals and grants must fit before rewards commit.
// maxGrantPlus bounds the plus of granted equipment.
const maxGrantPlus = 12

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

// planInventoryGrant adds grant to inv, already holding the caller's removals
// from before, and plans the rows and frames for the whole change. A slot in
// silent gets no delete frame: its caller announces the removal itself, as
// the Gacha exchange does with a 0x3645 quantity of zero.
func (rt *Runtime) planInventoryGrant(
	c *enterworld.Character,
	before []inventory.Item,
	inv *inventory.Inventory,
	grant []inventory.ItemAmount,
	silent map[uint8]bool,
) ([]enterworld.InventoryRow, []wire.Frame, error) {
	for _, request := range grant {
		if request.Count == 0 || request.Count > 65535 || rt.deps.ItemReferences() == nil {
			return nil, nil, fmt.Errorf("invalid quest grant %s", request.Codename)
		}
		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(request.Codename)
		if !ok || ref == nil {
			return nil, nil, fmt.Errorf("unresolved quest reward %s", request.Codename)
		}
		// A gold heap (the gold band, ITEM_ETC_GOLD_*) only lies on the
		// ground: pickup credits the balance and never writes a row. A row
		// would carry a gold type word the inventory body cannot encode,
		// and the client fails world entry on it for good (#367).
		if wire.IsGoldBand(ref.TypeFlags()) {
			return nil, nil, fmt.Errorf("ground-only item %s cannot enter an inventory", request.Codename)
		}
		item := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: uint16(request.Count)}
		if ref.TypeIDs[1] == 1 {
			item.Durability = 100
			if ref.VarianceIntMin1c0 != nil {
				item.Durability = uint32(clampInt64(*ref.VarianceIntMin1c0, 1, 0xffffffff))
			}
			// Operator grants may carry a plus; the GM console limit is +12.
			item.Plus = min(request.Plus, maxGrantPlus)
			if request.PerfectStats {
				// Every white stat field of this item type at 31 (100%), with
				// the matching durability, as monster drops build them.
				item.VarianceBits = 0
				for field := 0; field < equipmentVarianceFieldCount(ref); field++ {
					item.VarianceBits |= uint64(31) << (field * 5)
				}
				item.Durability = combat.DurabilityFromVariance(ref, 31)
			}
			if request.PerfectMagic {
				// The strongest catalog-valid blues for the item's type and
				// degree, as monster drops source them.
				item.MagicOptions = append([]uint64(nil), loot.BestEquipmentMagic(ref.Codename)...)
			}
		}
		for item.Quantity > 0 {
			result, fault := inv.GrantStack(item, rt.maxStackFor(item.TypeFlags, item.Codename))
			if fault != nil {
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
