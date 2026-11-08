/*
===========================================================================

alexandriashops_test.go - the Alexandria degree-11 shop switch (port-only)

The switch parses strictly, unset sells the goods (the native
stock), and on, the withholding removes exactly the degree-11
offers from the city's own tabs: degree-10 stock, the other
degrees and every other city's tabs are untouched, slots included.

===========================================================================
*/
package commerce

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
alexandriaRef
================
*/
func alexandriaRef(codename string, typeIDs [4]int64, itemClass float64) *enterworld.ItemRef {
	return &enterworld.ItemRef{
		Codename:     codename,
		TypeIDs:      typeIDs,
		NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": itemClass}),
	}
}

/*
================
TestAlexandriaHideD11GoodsFromEnv
================
*/
func TestAlexandriaHideD11GoodsFromEnv(t *testing.T) {
	// "" is also what an unset variable reads as.
	for _, off := range []string{"", " ", "off", "OFF", "0", "false"} {
		t.Setenv(EnvAlexandriaHideD11Goods, off)
		if hide, err := AlexandriaHideD11GoodsFromEnv(); err != nil || hide {
			t.Fatalf("%q = %v, %v; want the goods sold, no error", off, hide, err)
		}
	}
	for _, on := range []string{"on", "ON", "1", "true"} {
		t.Setenv(EnvAlexandriaHideD11Goods, on)
		if hide, err := AlexandriaHideD11GoodsFromEnv(); err != nil || !hide {
			t.Fatalf("%q = %v, %v; want the goods hidden, no error", on, hide, err)
		}
	}
	t.Setenv(EnvAlexandriaHideD11Goods, "yes")
	if _, err := AlexandriaHideD11GoodsFromEnv(); err == nil || !strings.Contains(err.Error(), EnvAlexandriaHideD11Goods) {
		t.Fatalf("a bad value must fail naming the setting, got %v", err)
	}
}

/*
================
TestWithholdAlexandriaD11
================
*/
func TestWithholdAlexandriaD11(t *testing.T) {
	gear := func(codename string, itemClass float64) *enterworld.ItemRef {
		return alexandriaRef(codename, [4]int64{3, 1, 0, 0}, itemClass)
	}
	stone := func(codename string, itemClass float64) *enterworld.ItemRef {
		return alexandriaRef(codename, [4]int64{3, 3, 0, 0}, itemClass)
	}
	// itemClass counts three classes per degree: 25-27 are degree 9,
	// 28-30 degree 10, 31-33 degree 11. A stone's class is its degree.
	d9Gear, d10Gear, d11Gear := gear("ITEM_CH_SWORD_09_A", 25), gear("ITEM_CH_SWORD_10_A", 28), gear("ITEM_CH_SWORD_11_A", 31)
	d9Stone, d11Stone := stone("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_09", 9), stone("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_11", 11)
	const (
		sdSmith     = int32(11)
		sdAccessory = int32(12)
		ktSmith     = int32(21)
	)
	c := &Catalog{
		Tabs: map[int32][]Offer{
			sdSmith: {
				{Slot: 0, Ref: d10Gear},
				{Slot: 1, Ref: d11Gear},
				// A package is its contents: a degree-11 good inside
				// withholds the whole offer.
				{Slot: 2, Ref: d10Gear, Contents: []Content{{Ref: d11Gear}}},
				{Slot: 3, Ref: d9Gear},
			},
			sdAccessory: {
				{Slot: 0, Ref: d9Stone},
				{Slot: 1, Ref: d11Stone},
			},
			ktSmith: {
				{Slot: 0, Ref: d11Gear},
			},
		},
		tabIDs: map[string]int32{
			"STORE_SD_SMITH_TAB1":     sdSmith,
			"STORE_SD_ACCESSORY_TAB2": sdAccessory,
			"STORE_KT_SMITH_TAB1":     ktSmith,
		},
	}
	if removed := c.WithholdAlexandriaD11(); removed != 3 {
		t.Fatalf("removed %d offers, want 3", removed)
	}
	want := map[int32][]string{
		sdSmith:     {"ITEM_CH_SWORD_10_A", "ITEM_CH_SWORD_09_A"},
		sdAccessory: {"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_09"},
		ktSmith:     {"ITEM_CH_SWORD_11_A"},
	}
	for tab, names := range want {
		offers := c.Tabs[tab]
		if len(offers) != len(names) {
			t.Fatalf("tab %d holds %d offers, want %d", tab, len(offers), len(names))
		}
		for i, name := range names {
			if offers[i].Ref.Codename != name {
				t.Fatalf("tab %d offer %d is %s, want %s", tab, i, offers[i].Ref.Codename, name)
			}
		}
	}
	// The kept offers keep their authored slots.
	if c.Tabs[sdSmith][0].Slot != 0 || c.Tabs[sdSmith][1].Slot != 3 {
		t.Fatalf("kept slots = %d, %d; want 0, 3", c.Tabs[sdSmith][0].Slot, c.Tabs[sdSmith][1].Slot)
	}
	// Nothing left to withhold a second time.
	if removed := c.WithholdAlexandriaD11(); removed != 0 {
		t.Fatalf("second pass removed %d offers, want 0", removed)
	}
}
