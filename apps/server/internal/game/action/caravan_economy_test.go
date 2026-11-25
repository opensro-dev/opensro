/*
===========================================================================

caravan_economy_test.go - the AI caravan gold doors and cargo drop

Slice 5 of #3704: an escort reward credits the Hunter through the
caravan-escort-reward door, a hire fee debits the trader through
caravan-hire-fee and refuses whole when the purse is short, and a
parley tribute lands as real trade-goods ground items.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCaravanGoldDoors

The reward credits, the fee debits (to exactly zero is fine), a short
purse refuses the fee whole with no error and no change, and an
unknown character is an error on both doors.
================
*/
func TestCaravanGoldDoors(t *testing.T) {
	c := testCharacter() // 5000 gold
	rt, _ := newTestRuntime(c, testItems())

	done, err := rt.CaravanEscortReward(testDivision, c.Name, 60_000)
	if err != nil || done.Before != 5000 || done.After != 65_000 || *c.Gold != 65_000 {
		t.Fatalf("escort reward %+v, %v, gold %d", done, err, *c.Gold)
	}
	paid, err := rt.CaravanHireFee(testDivision, c.Name, 15_000)
	if !paid || err != nil || *c.Gold != 50_000 {
		t.Fatalf("hire fee paid=%v err=%v gold %d", paid, err, *c.Gold)
	}
	paid, err = rt.CaravanHireFee(testDivision, c.Name, 50_001)
	if paid || err != nil || *c.Gold != 50_000 {
		t.Fatalf("short hire fee paid=%v err=%v gold %d, want a whole refusal", paid, err, *c.Gold)
	}
	if _, err := rt.CaravanEscortReward(testDivision, "Nobody", 1); err == nil {
		t.Fatal("a reward landed on a missing character")
	}
	if _, err := rt.CaravanHireFee(testDivision, "Nobody", 1); err == nil {
		t.Fatal("a hire fee was charged to a missing character")
	}
	if _, err := rt.CaravanEscortReward(testDivision, c.Name, 0); err == nil {
		t.Fatal("a zero reward was applied")
	}
}

/*
================
TestDropCaravanCargoLandsRealGoods

The tribute is stacks units of the named trade goods on the ground at
the pose, each its own item; an unknown codename conjures nothing.
================
*/
func TestDropCaravanCargoLandsRealGoods(t *testing.T) {
	c := testCharacter()
	items := testItems()
	items["ITEM_ETC_TRADE_WC_01"] = &enterworld.ItemRef{
		RefObjID: 2151, Codename: "ITEM_ETC_TRADE_WC_01", TypeIDs: [4]int64{3, 3, 8, 1},
	}
	rt, _ := newTestRuntime(c, items)
	at := simulation.Spawn{RegionID: 26265, X: 100, Y: 2, Z: 200}

	if dropped := rt.DropCaravanCargo(testDivision, "ITEM_ETC_TRADE_WC_01", 3, at, "[AI]Trader_Mei"); dropped != 3 {
		t.Fatalf("dropped %d goods, want 3", dropped)
	}
	ground := rt.Ground.All(testDivision)
	if len(ground) != 3 {
		t.Fatalf("ground holds %d items, want 3", len(ground))
	}
	for _, item := range ground {
		if item.Codename != "ITEM_ETC_TRADE_WC_01" || item.RefObjID != 2151 || item.StackCount != 1 ||
			item.DroppedBy != "[AI]Trader_Mei" || item.Position.RegionID != 26265 {
			t.Fatalf("unexpected ground goods: %+v", item)
		}
	}
	if dropped := rt.DropCaravanCargo(testDivision, "ITEM_ETC_TRADE_NOPE_99", 2, at, "[AI]Trader_Mei"); dropped != 0 {
		t.Fatalf("an unknown codename dropped %d goods", dropped)
	}
	if dropped := rt.DropCaravanCargo(testDivision, "ITEM_ETC_TRADE_WC_01", 0, at, "[AI]Trader_Mei"); dropped != 0 {
		t.Fatalf("zero stacks dropped %d goods", dropped)
	}
}
