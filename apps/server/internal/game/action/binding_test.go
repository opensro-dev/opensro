/*
===========================================================================

binding_test.go - persisted beginner reward ownership at transaction boundaries

===========================================================================
*/
package action

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestBoundRewardGrantPersistsAndDoesNotBindOrdinaryStock
================
*/
func TestBoundRewardGrantPersistsAndDoesNotBindOrdinaryStock(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 20})
	rt, _ := newTestRuntime(c, testItems())
	before := c.Snapshot()
	rows, _, err := rt.PlanQuestInventory(c, nil, []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 78, CharacterBound: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
		t.Fatal("grant planner mutated character")
	}
	c.MissionInventory = rows
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored enterworld.Character
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	bound := uint16(0)
	ordinary := uint16(0)
	for _, item := range invItemsFromBag(&restored) {
		if item.Codename != "ITEM_ETC_HP_POTION_01" {
			continue
		}
		if item.CharacterBound {
			bound += item.Quantity
		} else {
			ordinary += item.Quantity
		}
	}
	if bound != 78 || ordinary != 20 {
		t.Fatalf("bound %d, ordinary %d", bound, ordinary)
	}
	if !reflect.DeepEqual(rows, rowsFromInvItems(invItemsFromBag(&restored))) {
		t.Fatal("persistence bridge lost binding")
	}
}

/*
================
TestBoundWeaponCannotBeOfferedOrStored
================
*/
func TestBoundWeaponCannotBeOfferedOrStored(t *testing.T) {
	rt, c, _, _ := exchangeFixture(t)
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == 20 {
			c.MissionInventory[i].CharacterBound = true
		}
	}
	if _, code := rt.exchangeOfferable(c, 20); code != wire.ExchangeErrCannotTrade {
		t.Fatalf("bound trade offered: %d", code)
	}
	item, ok := bagOf(c).At(20)
	if !ok || !item.CharacterBound {
		t.Fatal("binding not armed")
	}
	if rt.storableItem(item) {
		t.Fatal("bound item admitted to shared storage")
	}
}

/*
================
TestBoundPotionSaleRefusesWithoutChangingGoldOrInventory
================
*/
func TestBoundPotionSaleRefusesWithoutChangingGoldOrInventory(t *testing.T) {
	rt, c := merchantFixture(t)
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Codename == "ITEM_ETC_HP_POTION_01" {
			c.MissionInventory[i].CharacterBound = true
		}
	}
	before := c.Snapshot()
	result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 1})
	if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("bound sale accepted: %+v", result)
	}
	if !reflect.DeepEqual(before.MissionInventory, c.MissionInventory) || goldOf(before) != goldOf(c) {
		t.Fatal("bound sale mutated authority")
	}
	if quotes := rt.shopSaleQuotes(testDivision, c, rt.NpcRoster[0]); len(quotes) != 0 {
		t.Fatalf("bound sale quoted: %+v", quotes)
	}
}

/*
================
TestBoundPotionUseConsumesOnlyTheUsedUnit
================
*/
func TestBoundPotionUseConsumesOnlyTheUsedUnit(t *testing.T) {
	c := testCharacter()
	hp, mp := int64(40), int64(80)
	c.CurrentHP, c.CurrentMP = &hp, &mp
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{CharacterBound: true, Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 2})
	rt, _ := newTestRuntime(c, testItems())
	result := rt.HandleItemUse(testDivision, c, []byte{21, 0xEC, 0x08})
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 || *c.CurrentHP != 65 {
		t.Fatalf("bound potion refused: %+v", result)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 21 {
			if !row.CharacterBound || row.StackCount != 1 {
				t.Fatalf("consumption lost binding: %+v", row)
			}
			return
		}
	}
	t.Fatal("remaining bound potion disappeared")
}

/*
================
TestBoundRewardsSurviveAuthorityRestartWithFeatureOff
================
*/
func TestBoundRewardsSurviveAuthorityRestartWithFeatureOff(t *testing.T) {
	t.Setenv("SRO_BEGINNER_QUESTS", "off")
	dir := t.TempDir()
	first := openDoorRuntime(t, dir, testCharacter())
	rows, _, err := first.rt.PlanQuestInventory(first.character, nil, []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 25, CharacterBound: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !first.rt.deps.Update(first.character, "bound-reward-test", func() bool {
		first.character.MissionInventory = rows
		return true
	}) {
		t.Fatal("reward was not committed")
	}
	first.authority.Close()
	second := openDoorRuntime(t, dir, nil)
	for _, item := range invItemsFromBag(second.character) {
		if item.Codename != "ITEM_ETC_HP_POTION_01" {
			continue
		}
		if !item.CharacterBound || item.Quantity != 25 || second.rt.storableItem(item) {
			t.Fatalf("binding lost after restart: %+v", item)
		}
		if _, fault := bagOf(second.character).Drop(item.Slot); fault == nil {
			t.Fatal("feature-off restart allowed a bound drop")
		}
		return
	}
	t.Fatal("reward disappeared after restart")
}

/*
================
TestMixedBindingMoveRefusesBeforeTheClientCanMerge
================
*/
func TestMixedBindingMoveRefusesBeforeTheClientCanMerge(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: 13, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 20, VarianceBits: "0", CharacterBound: true},
		{Slot: 14, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 10, VarianceBits: "0"},
	}
	rt, _ := newTestRuntime(c, testItems())
	before := c.Snapshot()
	result := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 13, DestSlot: 14, Quantity: 0}))
	if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpItemMoveResponse || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("mixed stack move was not refused on wire: %+v", result)
	}
	if !reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
		t.Fatal("mixed stack move mutated authority")
	}
}
