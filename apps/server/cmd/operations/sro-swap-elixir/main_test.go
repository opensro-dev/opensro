/*
===========================================================================

main_test.go - the plain-to-Intensifying elixir mapping on the shipped v1.150 data

Each of the four plain elixirs becomes the Intensifying one of its kind,
keeping slot, stack, trade owner and record id; the B elixirs and every
other row are left alone. A row's stack is kept as it is: on the shipped
data both rows hold the same per slot.

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestPlainElixirsBecomeIntensifying
================
*/
func TestPlainElixirsBecomeIntensifying(t *testing.T) {
	tier.Long(t, "loads the shipped item data")
	w := &swapper{items: enterworld.NewTextdataItems(gamedatatest.TextdataDir(t)), counts: map[string]int64{}}
	for _, kind := range []string{"WEAPON", "SHIELD", "ARMOR", "ACCESSARY"} {
		plain, strong := elixirPrefix+kind+plainSuffix, elixirPrefix+kind+strongSuffix
		ref, ok := w.items.ItemRefByCodename(plain)
		if !ok {
			t.Fatalf("no %s in the shipped data", plain)
		}
		row := domain.InventoryRow{Slot: 20, RecordID: 77, RefObjID: ref.RefObjID, Codename: plain, TypeFlags: ref.TypeFlags(),
			StackCount: 3, TradeOwner: "Seller"}
		next, changed, err := w.swap(row)
		strongRef, _ := w.items.ItemRefByCodename(strong)
		if err != nil || !changed || next.Codename != strong || next.RefObjID != strongRef.RefObjID {
			t.Fatalf("%s: %v %v %+v", plain, err, changed, next)
		}
		if next.Slot != 20 || next.RecordID != 77 || next.StackCount != 3 || next.TradeOwner != "Seller" {
			t.Fatalf("%s: kept fields changed %+v", plain, next)
		}
		if w.counts[strong] != 3 {
			t.Fatalf("%s: counted %d", strong, w.counts[strong])
		}
	}
	for _, other := range []string{elixirPrefix + "WEAPON_B", "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_01", "ITEM_ETC_HP_POTION_01"} {
		if _, changed, err := w.swap(domain.InventoryRow{Codename: other, StackCount: 1}); changed || err != nil {
			t.Fatalf("%s: changed=%v err=%v", other, changed, err)
		}
	}
}
