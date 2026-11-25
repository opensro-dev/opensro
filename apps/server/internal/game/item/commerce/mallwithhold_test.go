/*
===========================================================================

mallwithhold_test.go - a mall good a shard rule keeps off the shelves

===========================================================================
*/
package commerce

import "testing"

/*
================
TestWithholdItemRemovesEveryPackageHoldingIt

The card's own package and a bundle that holds it leave the mall; every
other good stays at its address, so no purchase target shifts.
================
*/
func TestWithholdItemRemovesEveryPackageHoldingIt(t *testing.T) {
	const card = 19962
	catalog := &MallCatalog{Offers: []MallPackage{
		{Code: "PACKAGE_ITEM_MALL_GACHA_CARD", ItemIDs: []uint32{card}, MallAddress: MallAddress{Shop: 1, Tab: 2, Slot: 0}},
		{Code: "PACKAGE_POTION", ItemIDs: []uint32{7}, MallAddress: MallAddress{Shop: 1, Tab: 2, Slot: 1}},
		{Code: "PACKAGE_BUNDLE", ItemIDs: []uint32{7, card}, MallAddress: MallAddress{Shop: 1, Tab: 2, Slot: 2}},
	}}
	if removed := catalog.WithholdItem(card); removed != 2 {
		t.Fatalf("removed %d offers, want 2", removed)
	}
	if len(catalog.Offers) != 1 || catalog.Offers[0].Code != "PACKAGE_POTION" || catalog.Offers[0].Slot != 1 {
		t.Fatalf("kept %+v, want only the potion at slot 1", catalog.Offers)
	}
	if removed := catalog.WithholdItem(card); removed != 0 {
		t.Fatalf("second pass removed %d, want 0", removed)
	}
}
