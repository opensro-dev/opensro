/*
===========================================================================

mallp2wscrolls_test.go - the pay-to-win scrolls leave the mall; a pack names its count

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestWithholdItemsNamedRemovesSinglesAndPacks

A listed scroll's single package and its pack of eleven both leave; a
booster beside them keeps its address, and a listed codename the tree
lacks (a plain build without the later items) names nothing.
================
*/
func TestWithholdItemsNamedRemovesSinglesAndPacks(t *testing.T) {
	const scroll, booster = 4201, 4202
	refs := stubItemRefs{
		"ITEM_MALL_DAMAGE_INC_10P_SCROLL": {RefObjID: scroll, Codename: "ITEM_MALL_DAMAGE_INC_10P_SCROLL"},
		"ITEM_MALL_100_EXP_SCROLL_1TIME":  {RefObjID: booster, Codename: "ITEM_MALL_100_EXP_SCROLL_1TIME"},
	}
	catalog := &MallCatalog{Offers: []MallPackage{
		{Code: "PACKAGE_ITEM_MALL_DAMAGE_INC_10P_SCROLL", ItemIDs: []uint32{scroll}, MallAddress: MallAddress{Shop: 1, Tab: 3, Slot: 0}},
		{Code: "PACKAGE_ITEM_MALL_100_EXP_SCROLL_1TIME", ItemIDs: []uint32{booster}, MallAddress: MallAddress{Shop: 1, Tab: 3, Slot: 1}},
		{Code: "PACKAGE_ITEM_MALL_DAMAGE_INC_10P_SCROLL_11", ItemIDs: []uint32{scroll}, MallAddress: MallAddress{Shop: 1, Tab: 3, Slot: 2}},
	}}
	if removed := catalog.WithholdItemsNamed(refs, P2WScrollItems); removed != 2 {
		t.Fatalf("removed %d offers, want 2", removed)
	}
	if len(catalog.Offers) != 1 || catalog.Offers[0].ItemIDs[0] != booster || catalog.Offers[0].Slot != 1 {
		t.Fatalf("kept %+v, want only the booster at slot 1", catalog.Offers)
	}
	if removed := (&MallCatalog{Offers: catalog.Offers}).WithholdItemsNamed(stubItemRefs{}, P2WScrollItems); removed != 0 {
		t.Fatalf("a tree without the scrolls removed %d, want 0", removed)
	}
}

/*
================
TestMallP2WScrollsOffSwitch

Unset is the shipped mall; on withholds; a typo refuses the boot.
================
*/
func TestMallP2WScrollsOffSwitch(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    bool
		refused bool
	}{{"", false, false}, {"off", false, false}, {"on", true, false}, {"onn", false, true}} {
		t.Setenv(EnvMallP2WScrollsOff, tc.value)
		got, err := MallP2WScrollsOffFromEnv()
		if (err != nil) != tc.refused || got != tc.want {
			t.Fatalf("%q: got %t, err %v; want %t, refused %t", tc.value, got, err, tc.want, tc.refused)
		}
	}
}

/*
================
TestPackageCountNamesThePack

A pack of eleven scrolls counts eleven; a single, a non-stackable item and
a bundle of two items count nothing, so only the pack's title changes.
================
*/
func TestPackageCountNamesThePack(t *testing.T) {
	scroll := &enterworld.ItemRef{RefObjID: 4201, TypeIDs: [4]int64{3, 3, 13, 1}}
	sword := &enterworld.ItemRef{RefObjID: 4300, TypeIDs: [4]int64{3, 1, 6, 2}}
	for _, tc := range []struct {
		name     string
		contents []Content
		want     uint16
	}{
		{"pack", []Content{{Ref: scroll, Stack: 50, Data: 11}}, 11},
		{"single", []Content{{Ref: scroll, Stack: 50, Data: 1}}, 0},
		{"buy by stack", []Content{{Ref: scroll, Stack: 50}}, 0},
		{"equipment", []Content{{Ref: sword, Stack: 1, Data: 40}}, 0},
		{"bundle", []Content{{Ref: scroll, Stack: 50, Data: 11}, {Ref: scroll, Stack: 50, Data: 11}}, 0},
	} {
		if got := packageCount(tc.contents); got != tc.want {
			t.Fatalf("%s: count %d, want %d", tc.name, got, tc.want)
		}
	}
}
