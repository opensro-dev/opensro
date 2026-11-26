/*
===========================================================================

watches_test.go - grab-pet clock rows stay whole

The four shipped watch rows (type 3/3/13/15 with the whole-item bit in
the sixth authored parameter) are grab-pet clocks: two of them poured
into one stack would lose their minutes. The native merge check keeps
them one row each, whatever the stack column says (#4490, U6-14).

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// watchCodename is the fixture's shipped-clock stand-in.
const watchCodename = "ITEM_MALL_PET_WATCH_FIXTURE"

/*
================
watchRefs

The clock row itself, a same-type row without the whole-item bit, and a
different-family row that carries the bit: only the first family stays
whole.
================
*/
func watchRefs() staticItemSource {
	return staticItemSource{
		watchCodename: {
			RefObjID: 22607, Codename: watchCodename, TypeIDs: [4]int64{3, 3, 13, 15},
			NativeFields: enterworld.NewNativeFields(map[string]float64{
				"maxStack": 4320, "itemParam6_2b0": 2, "canTrade": 1}),
		},
		"WATCH_TYPE_NO_BIT": {
			RefObjID: 22670, Codename: "WATCH_TYPE_NO_BIT", TypeIDs: [4]int64{3, 3, 13, 15},
			NativeFields: enterworld.NewNativeFields(map[string]float64{
				"maxStack": 4320, "canTrade": 1}),
		},
		"WATCH_BIT_OTHER_FAMILY": {
			RefObjID: 22671, Codename: "WATCH_BIT_OTHER_FAMILY", TypeIDs: [4]int64{3, 3, 1, 1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{
				"maxStack": 300, "itemParam6_2b0": 2, "canTrade": 1}),
		},
	}
}

/*
================
TestWatchStackCapIsOne
================
*/
func TestWatchStackCapIsOne(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, watchRefs())

	watch := watchRefs()[watchCodename]
	if got := rt.maxStackFor(watch.TypeFlags(), watchCodename); got != 1 {
		t.Fatalf("watch cap: got %d, want 1", got)
	}
	noBit := watchRefs()["WATCH_TYPE_NO_BIT"]
	if got := rt.maxStackFor(noBit.TypeFlags(), "WATCH_TYPE_NO_BIT"); got != 4320 {
		t.Fatalf("same-type no-bit cap: got %d, want 4320", got)
	}
	other := watchRefs()["WATCH_BIT_OTHER_FAMILY"]
	if got := rt.maxStackFor(other.TypeFlags(), "WATCH_BIT_OTHER_FAMILY"); got != 300 {
		t.Fatalf("other-family cap: got %d, want 300", got)
	}
}

/*
================
TestWatchRowsDoNotMerge

Two clocks go through the bag-to-bag stack frame: the stack column's
4320 must not pour them; the rows swap places instead of combining.
================
*/
func TestWatchRowsDoNotMerge(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: 20, RefObjID: 22607, Codename: watchCodename, TypeFlags: wire.PackTypeFlags(3, 3, 13, 15), StackCount: 1, VarianceBits: "0"},
		{Slot: 21, RefObjID: 22607, Codename: watchCodename, TypeFlags: wire.PackTypeFlags(3, 3, 13, 15), StackCount: 1, VarianceBits: "0"},
	}
	rt, _ := newTestRuntime(c, watchRefs())

	result := rt.HandleItemMove(testDivision, c, []byte{0, 20, 21, 1, 0})
	if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("move: %+v", result)
	}
	if len(c.MissionInventory) != 2 {
		t.Fatalf("rows: %+v", c.MissionInventory)
	}
	bySlot := map[int64]enterworld.InventoryRow{}
	for _, row := range c.MissionInventory {
		bySlot[row.Slot] = row
	}
	if bySlot[21].StackCount != 1 || bySlot[20].StackCount != 1 {
		t.Fatalf("merged clock: %+v", c.MissionInventory)
	}
	if !reflect.DeepEqual(bySlot[20].Codename, watchCodename) {
		t.Fatalf("slot 20: %+v", bySlot[20])
	}
}
