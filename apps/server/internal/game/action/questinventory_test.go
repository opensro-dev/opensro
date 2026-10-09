package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"reflect"
	"testing"
)

func TestQuestInventoryPlansWholeRewardBeforeMutating(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	old := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
	request := inventory.ItemAmount{Codename: "ITEM_ETC_HP_POTION_01", Count: 78}
	rows, frames, err := rt.PlanQuestInventory(c, nil, []inventory.ItemAmount{request})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.MissionInventory, old) {
		t.Fatal("planner mutated authority")
	}
	if len(rows) != 3 || len(frames) != 3 || frames[0].Opcode != 14 {
		t.Fatalf("bad plan %v / %v", rows, frames)
	}
	total := int64(0)
	for _, r := range rows {
		if r.RefObjID == 3630 {
			total += r.StackCount
			if r.StackCount > 50 {
				t.Fatal("stack overflow")
			}
		}
	}
	if total != 78 {
		t.Fatalf("reward count %d", total)
	}
	c.MissionInventory = rows
	rows, _, err = rt.PlanQuestInventory(c, nil, []inventory.ItemAmount{{Codename: request.Codename, Count: 20}})
	if err != nil || len(rows) != 3 {
		t.Fatalf("existing stack did not merge: %v %v", rows, err)
	}
}

func TestQuestInventoryFullBagRefusesAllAndConsumptionCanFreeSlot(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	c.MissionInventory = nil
	for n := 13; n < 45; n++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(n), RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: testItems()["ITEM_ETC_HP_POTION_01"].TypeFlags(), StackCount: 50})
	}
	before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
	grant := []inventory.ItemAmount{{Codename: "ITEM_CH_SWORD_01_A_RARE", Count: 1}}
	if rows, frames, err := rt.PlanQuestInventory(c, nil, grant); err == nil || rows != nil || frames != nil {
		t.Fatal("full bag paid a partial reward")
	}
	if !reflect.DeepEqual(before, c.MissionInventory) {
		t.Fatal("full bag changed inventory")
	}
	rows, frames, err := rt.PlanQuestInventory(c, []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 50}}, grant)
	if err != nil || len(rows) != 32 {
		t.Fatalf("consumed slot was not reusable: %v", err)
	}
	if len(frames) != 3 || frames[1].Payload[1] != 15 || frames[2].Payload[1] != 14 || frames[1].Payload[2] != frames[2].Payload[2] {
		t.Fatalf("replacement must remove before grant: %v", frames)
	}
	if !reflect.DeepEqual(before, c.MissionInventory) {
		t.Fatal("successful plan mutated inventory")
	}
}

func TestQuestInventoryMissingConsumptionAndMissingRewardRefuse(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	for _, req := range []struct{ consume, grant []inventory.ItemAmount }{
		{consume: []inventory.ItemAmount{{Codename: "MISSING", Count: 1}}},
		{grant: []inventory.ItemAmount{{Codename: "MISSING", Count: 1}}},
		{grant: []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 0}}},
		// A gold heap is ground-only: an operator grant or a reward that
		// names one is refused whole (#367).
		{grant: []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 1}, {Codename: "ITEM_ETC_GOLD_02", Count: 1}}},
	} {
		if _, _, err := rt.PlanQuestInventory(c, req.consume, req.grant); err == nil {
			t.Fatal("invalid transaction accepted")
		}
	}
}

// A presented expansion is usable: with the first 32 bag slots full, a
// 55-slot inventory takes the grant into wire slot 45; a 45-slot one refuses.
func TestQuestInventoryUsesTheCharactersCapacity(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	c.MissionInventory = nil
	for n := 13; n < 45; n++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(n), RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: testItems()["ITEM_ETC_HP_POTION_01"].TypeFlags(), StackCount: 50})
	}
	grant := []inventory.ItemAmount{{Codename: "ITEM_CH_SWORD_01_A_RARE", Count: 1}}
	c.InventoryExpansion = 10
	if _, _, err := rt.PlanQuestInventory(c, nil, grant); err == nil {
		t.Fatal("waiting slots took an item before the client knew them")
	}
	c.InventorySize, c.InventoryExpansion = 55, 0
	rows, _, err := rt.PlanQuestInventory(c, nil, grant)
	if err != nil {
		t.Fatal(err)
	}
	if last := rows[len(rows)-1]; last.Slot != 45 || last.Codename != "ITEM_CH_SWORD_01_A_RARE" {
		t.Fatalf("grant landed at %+v, want wire slot 45", last)
	}
}
