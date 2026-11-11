/*
===========================================================================

traderepair_test.go - the port-only bag trade-goods repair (#3144)

===========================================================================
*/
package action

import (
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
tradeRepairCharacter

A character with a stuck trade stack in the personal bag (slot 28,
the report's shape), an event-food row the repair must leave alone,
and 5000 gold. The horseshoe's reference price is 100.
================
*/
func tradeRepairCharacter() *enterworld.Character {
	c := testCharacter()
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: 28, RefObjID: 5005, Codename: "ITEM_ETC_TRADE_WC_04",
			TypeFlags: wire.PackTypeFlags(3, 3, 8, 1), StackCount: 40},
		enterworld.InventoryRow{Slot: 29, RefObjID: 5006, Codename: "ITEM_ETC_E050314_CANDY_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 8, 0), StackCount: 3},
	)
	return c
}

/*
================
tradeRepairItems
================
*/
func tradeRepairItems() staticItemSource {
	items := testItems()
	items["ITEM_ETC_TRADE_WC_04"] = &enterworld.ItemRef{
		RefObjID: 5005, Codename: "ITEM_ETC_TRADE_WC_04", TypeIDs: [4]int64{3, 3, 8, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"price": 100}),
	}
	items["ITEM_ETC_E050314_CANDY_01"] = &enterworld.ItemRef{
		RefObjID: 5006, Codename: "ITEM_ETC_E050314_CANDY_01", TypeIDs: [4]int64{3, 3, 8, 0},
	}
	return items
}

/*
================
TestBagTradeGoodsRepairRefundsOnce

On load the stuck stack leaves the bag and its reference price
(40 x 100) lands in the balance; the event food stays. A second
load changes nothing: the repair has converged.
================
*/
func TestBagTradeGoodsRepairRefundsOnce(t *testing.T) {
	c := tradeRepairCharacter()
	rt, _ := newTestRuntime(c, tradeRepairItems())

	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if bagCount(c, "ITEM_ETC_TRADE_WC_04") != 0 {
		t.Fatalf("the stuck stack survived the repair: %+v", c.MissionInventory)
	}
	if bagCount(c, "ITEM_ETC_E050314_CANDY_01") != 3 {
		t.Fatalf("the repair took a row it does not own: %+v", c.MissionInventory)
	}
	if got := goldOf(c); got != 9000 {
		t.Fatalf("gold = %d, want 5000 + 40*100 refund", got)
	}

	before := c.Snapshot()
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("a second load changed the character; the repair must converge")
	}
}

/*
================
TestBagTradeGoodsRepairKeepsRowsWhenRefundOverflows

A refund that would overflow the gold balance is refused the way
the gold-credit lanes refuse it: the rows stay for the next load
and the balance does not wrap.
================
*/
func TestBagTradeGoodsRepairKeepsRowsWhenRefundOverflows(t *testing.T) {
	c := tradeRepairCharacter()
	*c.Gold = math.MaxInt64 - 100
	rt, _ := newTestRuntime(c, tradeRepairItems())

	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if bagCount(c, "ITEM_ETC_TRADE_WC_04") != 40 {
		t.Fatalf("the repair removed a stack it could not refund: %+v", c.MissionInventory)
	}
	if got := goldOf(c); got != uint64(math.MaxInt64-100) {
		t.Fatalf("gold = %d, want the balance untouched", got)
	}
}
