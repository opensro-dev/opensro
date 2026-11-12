/*
===========================================================================

itemsale_test.go - a merchant sale notes its item in the ledger

Port-only, not native (#3307): the economy ledger's gold side reads
the balance at the door, which cannot name what was sold, so the
sale door notes the item itself. An ordinary shop sale lands in the
seller's per-item hourly bucket with its count and credited gold;
the note rides the real authority store, wired as production wires
it (wiring_authority.go).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMerchantSaleNotesItsItem

Selling potions in two lots and a scroll at the merchant fills the
seller's per-item hourly buckets: the potion lots accumulate into
one bucket (5 for 75), the scroll keeps its own (1 for 40).
================
*/
func TestMerchantSaleNotesItsItem(t *testing.T) {
	authority, rt, c := freeGrantShopFixture(t)
	// The production wiring (wiring_authority.go): the authority
	// store keeps the note.
	rt.deps.(*enterworld.Deps).ItemSaleNote = authority.NoteItemSale
	for _, sale := range []wire.ItemMoveRequest{
		{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 2},
		{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 3},
		{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 14, Quantity: 1},
	} {
		result := trade(t, rt, c, sale)
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("the sale of slot %d did not succeed: %+v", sale.SourceSlot, result.Frames)
		}
	}
	hours, err := authority.EconomyCharacterItemHours(c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int64{
		"ITEM_ETC_HP_POTION_01":     {5, 75},
		"ITEM_ETC_SCROLL_RETURN_01": {1, 40},
	}
	if len(hours) != len(want) {
		t.Fatalf("item hours %+v, want %v", hours, want)
	}
	for _, hour := range hours {
		if got := want[hour.Item]; got != [2]int64{hour.Count, hour.Gold} {
			t.Fatalf("item hours %+v, want %v", hours, want)
		}
	}
}
