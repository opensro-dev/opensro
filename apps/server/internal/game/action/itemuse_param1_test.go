/*
===========================================================================

itemuse_param1_test.go - the 3/3/13/13 scrolls split on Param1

v1.150 itemdata ships the stat recall (Param1 2), the Hwan (1), the auto
potion ticket (3) and the friend return scroll (4) under one type. Only
Param1 2 may reach the stat recall; the v1.188 server handles none of the
type (its special-consumable switch refuses it outright;
research/findings/item-use-13-13-param1.md), so
3, 4 and any unknown Param1 are refused with the scroll kept.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
param1ScrollRef

A 3/3/13/13 row carrying only the given Param1. Country 3 is the native
"every country" row and canUse is set, so no gate before the Param1 split
refuses this fixture.
================
*/
func param1ScrollRef(codename string, param1 float64) *enterworld.ItemRef {
	return &enterworld.ItemRef{RefObjID: 9260, Codename: codename, TypeIDs: [4]int64{3, 3, 13, 13},
		Country:      3,
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": param1})}
}

/*
================
param1ScrollBody

The use packet for a scroll in bag slot 21.
================
*/
func param1ScrollBody(ref *enterworld.ItemRef) []byte {
	body := []byte{21, 0, 0}
	binary.LittleEndian.PutUint16(body[1:], ref.TypeFlags())
	return body
}

/*
================
TestParam1ScrollFamilies
================
*/
func TestParam1ScrollFamilies(t *testing.T) {
	for _, tc := range []struct {
		codename string
		param1   float64
		want     itemUseFamily
	}{
		{"ITEM_MALL_HWAN", 1, itemUseBerserk},
		{"ITEM_MALL_STATS_POINT_RECALL", 2, itemUseStatRecall},
		{"ITEM_MALL_AUTO_POTION_TICKET", 3, itemUseUnsupported},
		{"ITEM_MALL_FRIEND_RETURN_SCROLL", 4, itemUseUnsupported},
		{"ITEM_MALL_SKILL_RESET", 5, itemUseSkillRecall},
		{"ITEM_UNKNOWN_PARAM1_0", 0, itemUseUnsupported},
		{"ITEM_UNKNOWN_PARAM1_9", 9, itemUseUnsupported},
	} {
		if got := admittedItemUseFamily(param1ScrollRef(tc.codename, tc.param1)); got != tc.want {
			t.Errorf("%s (Param1 %v): family %d, want %d", tc.codename, tc.param1, got, tc.want)
		}
	}
	// A row whose itemdata lacks the column carries no Param1 at all: the
	// split reads nothing, so the type stays unsupported rather than
	// falling through to a family that would spend it.
	absent := param1ScrollRef("ITEM_UNKNOWN_PARAM1_ABSENT", 0)
	absent.NativeFields = absent.NativeFields.Without("itemParam1_29c")
	if got := admittedItemUseFamily(absent); got != itemUseUnsupported {
		t.Errorf("absent Param1 column: family %d, want %d", got, itemUseUnsupported)
	}
}

/*
================
TestParam1ScrollsOutsideTheRecallKeepStats

Using a Param1 3, 4 or unknown scroll refuses as the unsupported family
(invalid request), never calls the stat recall and keeps the scroll in the
bag.
================
*/
func TestParam1ScrollsOutsideTheRecallKeepStats(t *testing.T) {
	for _, param1 := range []float64{3, 4, 9} {
		ref := param1ScrollRef("ITEM_MALL_PARAM1_SCROLL", param1)
		c := testCharacter()
		items := testItems()
		items[ref.Codename] = ref
		c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID,
			Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1}}
		rt, _ := newTestRuntime(c, items)
		recalled := false
		rt.RecallStatPoints = func(*enterworld.Character) ([]wire.Frame, bool) { recalled = true; return nil, true }
		// The snapshot inside the helper also proves the scroll stayed.
		assertItemUseRefusedUnchanged(t, rt, c, param1ScrollBody(ref), wire.ErrCodeInvalidRequest)
		if recalled {
			t.Fatalf("Param1 %v recalled stat points", param1)
		}
	}
}

/*
================
TestParam1StatRecallScrollIsSpent

The positive control beside the refusals: Param1 2 reaches the recall
once and the scroll leaves the bag.
================
*/
func TestParam1StatRecallScrollIsSpent(t *testing.T) {
	ref := param1ScrollRef("ITEM_MALL_STATS_POINT_RECALL", 2)
	c := testCharacter()
	items := testItems()
	items[ref.Codename] = ref
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID,
		Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	recalled := 0
	rt.RecallStatPoints = func(*enterworld.Character) ([]wire.Frame, bool) {
		recalled++
		return nil, true
	}
	result := rt.HandleItemUse(testDivision, c, param1ScrollBody(ref))
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("Param1 2 was refused: %+v", result)
	}
	if recalled != 1 {
		t.Fatalf("recall called %d times, want 1", recalled)
	}
	if len(c.MissionInventory) != 0 {
		t.Fatalf("Param1 2 kept the scroll: %+v", c.MissionInventory)
	}
}

/*
================
TestShippedParam1ScrollsReachOnlyTheirFamily

The shipped v1.150 rows: the six stat recalls admit as the recall, the
Hwan as the berserk scroll, the auto potion ticket and friend return
scroll as nothing. Every shipped row of the type must admit to the family
its Param1 implies, so a later row cannot silently reach the recall.
================
*/
func TestShippedParam1ScrollsReachOnlyTheirFamily(t *testing.T) {
	items := shippedItems(t)
	for codename, want := range map[string]itemUseFamily{
		"ITEM_MALL_STATS_POINT_RECALL":      itemUseStatRecall,
		"ITEM_MALL_STATS_POINT_RECALL_50LV": itemUseStatRecall,
		"ITEM_MALL_STATS_POINT_RECALL_60LV": itemUseStatRecall,
		"ITEM_MALL_STATS_POINT_RECALL_70LV": itemUseStatRecall,
		"ITEM_MALL_STATS_POINT_RECALL_80LV": itemUseStatRecall,
		"ITEM_MALL_STATS_POINT_RECALL_81LV": itemUseStatRecall,
		"ITEM_MALL_HWAN":                    itemUseBerserk,
		"ITEM_MALL_AUTO_POTION_TICKET":      itemUseUnsupported,
		"ITEM_MALL_FRIEND_RETURN_SCROLL":    itemUseUnsupported,
	} {
		ref, ok := items.ItemRefByCodename(codename)
		if !ok {
			t.Fatalf("%s missing from the shipped itemdata", codename)
		}
		if ref.TypeIDs != [4]int64{3, 3, 13, 13} {
			t.Fatalf("%s type %v", codename, ref.TypeIDs)
		}
		if got := admittedItemUseFamily(ref); got != want {
			t.Errorf("%s: family %d, want %d", codename, got, want)
		}
	}
	// Every shipped row of the type, not only the ones named above.
	shipped := 0
	for _, row := range items.ItemCommandReferences() {
		if row.TypeFlags != wire.PackTypeFlags(3, 3, 13, 13) {
			continue
		}
		ref, ok := items.ItemRefByCodename(row.Codename)
		if !ok {
			t.Fatalf("%s listed without an item ref", row.Codename)
		}
		param1, present := ref.NativeFields.Lookup("itemParam1_29c")
		want := itemUseUnsupported
		if present {
			switch param1 {
			case 1:
				want = itemUseBerserk
			case 2:
				want = itemUseStatRecall
			case 5:
				want = itemUseSkillRecall
			}
		}
		if got := admittedItemUseFamily(ref); got != want {
			t.Errorf("%s (Param1 %v, present %t): family %d, want %d", row.Codename, param1, present, got, want)
		}
		shipped++
	}
	if shipped == 0 {
		t.Fatal("no shipped 3/3/13/13 row found")
	}
}
