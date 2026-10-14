/*
===========================================================================

withhold_test.go - tests for withhold.go

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestWithholdGoodsKeepsTheOtherSlots

An offer is withheld when its item or a packaged item is excluded; every
other offer keeps its tab and slot.
================
*/
func TestWithholdGoodsKeepsTheOtherSlots(t *testing.T) {
	sun, moon := &enterworld.ItemRef{Codename: "SUN"}, &enterworld.ItemRef{Codename: "MOON"}
	c := &Catalog{Tabs: map[int32][]Offer{
		1: {{Slot: 0, Ref: moon}, {Slot: 1, Ref: sun}, {Slot: 2, Ref: moon}},
		2: {{Slot: 0, Contents: []Content{{Ref: sun}}}, {Slot: 1, Contents: []Content{{Ref: moon}}}},
	}}
	if removed := c.WithholdGoods(func(ref *enterworld.ItemRef) bool { return ref == sun }); removed != 2 {
		t.Fatalf("removed %d, want 2", removed)
	}
	if len(c.Tabs[1]) != 2 || c.Tabs[1][0].Slot != 0 || c.Tabs[1][1].Slot != 2 {
		t.Fatalf("tab 1 %+v", c.Tabs[1])
	}
	if len(c.Tabs[2]) != 1 || c.Tabs[2][0].Slot != 1 {
		t.Fatalf("tab 2 %+v", c.Tabs[2])
	}
}
