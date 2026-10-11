/*
===========================================================================

characterrows.go - the bridge between persisted character rows and the
inventory engine

Arms a character's stored inventory rows as inventory.Item values and
writes them back, and reads and writes the gold balance. Moved out of
runtime.go, which keeps the Runtime state and its lifecycle.

===========================================================================
*/

package action

import (
	"math"
	"strconv"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
==================
invItemsFromBag

invItemsFromBag arms a character's persisted rows, sockets and bag up to
its capacity byte, for the inventory engine: the decimal-string variance
becomes a real u64 and a zero stack reads as one unit, both exactly like
the fixture's wire re-arming.
==================
*/
func invItemsFromBag(c *enterworld.Character) []inventory.Item {
	return invItemsFromRowsWithin(c.MissionInventory, int64(inventory.BagEnd(c)))
}

/*
==================
bagOf

The character's inventory: its rows inside a bag of its own capacity.
==================
*/
func bagOf(c *enterworld.Character) *inventory.Inventory {
	return inventory.New(invItemsFromBag(c), inventory.BagEnd(c))
}

/*
==================
invItemsFromRowsWithin
==================
*/
func invItemsFromRowsWithin(rows []enterworld.InventoryRow, slotEnd int64) []inventory.Item {
	out := make([]inventory.Item, 0, len(rows))

	for _, row := range rows {
		if row.Slot < 0 || row.Slot >= slotEnd {
			continue
		}

		variance, err := strconv.ParseUint(row.VarianceBits, 10, 64)
		if err != nil {
			variance = 0
		}

		quantity := row.StackCount
		if quantity < 1 {
			quantity = 1
		}

		if quantity > 0xFFFF {
			quantity = 0xFFFF
		}

		out = append(out, inventory.Item{
			RecordID:          row.RecordID,
			TradeOwner:        row.TradeOwner,
			Slot:              uint8(row.Slot),
			RefObjID:          row.RefObjID,
			Codename:          row.Codename,
			TypeFlags:         row.TypeFlags,
			Plus:              uint8(clampInt64(row.Plus, 0, 0xFF)),
			VarianceBits:      variance,
			Durability:        uint32(clampInt64(row.Durability, 0, 0xFFFFFFFF)),
			Quantity:          uint16(quantity),
			MagicOptions:      append([]uint64(nil), row.MagicOptions...),
			TransformRefObjID: row.TransformRefObjID, Summon: domain.CloneCOS(row.Summon),
		})
	}

	return out
}

/*
==================
rowsFromInvItems

rowsFromInvItems persists the engine rows back onto the character record
(variance back to its decimal-string form).
==================
*/
func rowsFromInvItems(items []inventory.Item) []enterworld.InventoryRow {
	out := make([]enterworld.InventoryRow, 0, len(items))

	for _, item := range items {
		out = append(out, enterworld.InventoryRow{
			RecordID:          item.RecordID,
			TradeOwner:        item.TradeOwner,
			Slot:              int64(item.Slot),
			RefObjID:          item.RefObjID,
			Codename:          item.Codename,
			TypeFlags:         item.TypeFlags,
			Plus:              int64(item.Plus),
			VarianceBits:      strconv.FormatUint(item.VarianceBits, 10),
			Durability:        int64(item.Durability),
			StackCount:        int64(item.Quantity),
			MagicOptions:      append([]uint64(nil), item.MagicOptions...),
			TransformRefObjID: item.TransformRefObjID, Summon: domain.CloneCOS(item.Summon),
		})
	}

	return out
}

/*
==================
clampInt64
==================
*/
func clampInt64(value, min, max int64) int64 {
	if value < min {
		return min
	}

	if value > max {
		return max
	}

	return value
}

/*
==================
maxStackFor

maxStackFor ports missionItemMaxStack: the itemdata MaxStack column, gated
on the stackable-ETC class. A nil item source behaves like a missing row -
cap 1, the swap leg - which is the documented dev-mode degradation until
the itemdata loader lands.
==================
*/
func (rt *Runtime) maxStackFor(typeFlags uint16, codename string) uint16 {
	if !inventory.IsEtcStackableTypeFlags(typeFlags) {
		return 1
	}

	if rt.deps.ItemReferences() == nil {
		return 1
	}

	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(codename)
	if !ok || ref == nil || len(ref.NativeFields) == 0 {
		return 1
	}

	maxStack, ok := ref.NativeFields.Lookup("maxStack")
	if !ok || math.IsNaN(maxStack) || math.IsInf(maxStack, 0) {
		return 1
	}

	return uint16(clampInt64(int64(maxStack), 1, 0xFFFF))
}

/*
==================
goldOf
==================
*/
func goldOf(character *enterworld.Character) uint64 {
	if character == nil || character.Gold == nil || *character.Gold < 0 {
		return 0
	}

	return uint64(*character.Gold)
}

/*
==================
setGold
==================
*/
func setGold(character *enterworld.Character, balance uint64) {
	value := int64(math.MaxInt64)
	if balance <= uint64(math.MaxInt64) {
		value = int64(balance)
	}

	character.Gold = &value
}
