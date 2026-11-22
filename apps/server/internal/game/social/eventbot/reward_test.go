/*
===========================================================================

reward_test.go - paying a win into a bag

===========================================================================
*/
package eventbot

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// scrollRef is a stackable ETC scroll: an ETC-band type word
// (PackTypeFlags(3, 3, 1, 0)) and the itemdata stack cap 250.
func scrollRef() *enterworld.ItemRef {
	return &enterworld.ItemRef{
		RefObjID:       61,
		Codename:       "ITEM_ETC_SILK_SCROLL",
		TypeIDs:        [4]int64{3, 3, 1, 0},
		NativeMaxStack: 250,
	}
}

/*
================
TestAddRewardStacksThenTakesASlot

An existing stack fills to its cap first; the rest takes a new slot.
================
*/
func TestAddRewardStacksThenTakesASlot(t *testing.T) {
	ref := scrollRef()
	character := &enterworld.Character{Name: "Win", MissionInventory: []enterworld.InventoryRow{
		{Slot: 13, RefObjID: 61, TypeFlags: wire.PackTypeFlags(3, 3, 1, 0), StackCount: 245},
	}}
	if !AddReward(character, ref, 10) {
		t.Fatal("reward refused")
	}
	rows := character.MissionInventory
	if len(rows) != 2 || rows[0].StackCount != 250 || rows[1].StackCount != 5 {
		t.Fatalf("rows = %+v, want 250 + 5", rows)
	}
	if rows[1].Codename != "ITEM_ETC_SILK_SCROLL" || rows[1].RefObjID != 61 {
		t.Fatalf("new row = %+v", rows[1])
	}
}

/*
================
TestAddRewardFullBagPaysNothing

No room for all of it: nothing is added, so a win never half-pays.
================
*/
func TestAddRewardFullBagPaysNothing(t *testing.T) {
	ref := scrollRef()
	character := &enterworld.Character{Name: "Full"}
	for slot := int64(13); slot < 13+200; slot++ {
		character.MissionInventory = append(character.MissionInventory,
			enterworld.InventoryRow{Slot: slot, RefObjID: 9999, StackCount: 1})
	}
	before := len(character.MissionInventory)
	if AddReward(character, ref, 10) {
		t.Fatal("reward paid into a full bag")
	}
	if len(character.MissionInventory) != before {
		t.Fatal("a refused reward still changed the bag")
	}
}

/*
================
TestAddRewardNeverMintsGold

The reward only ever adds its own item; no gold field moves.
================
*/
func TestAddRewardNeverMintsGold(t *testing.T) {
	ref := scrollRef()
	gold := int64(500)
	character := &enterworld.Character{Name: "Win", Gold: &gold}
	if !AddReward(character, ref, 10) {
		t.Fatal("reward refused")
	}
	if character.Gold == nil || *character.Gold != 500 {
		t.Fatalf("gold moved on an item reward: %v", character.Gold)
	}
}
