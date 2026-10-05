/*
===========================================================================

tradegoods_test.go - cargo identity across the live drop and pickup handlers

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestTradeCargoOwnerSurvivesDropPickupAndCharacterRestore
================
*/
func TestTradeCargoOwnerSurvivesDropPickupAndCharacterRestore(t *testing.T) {
	c := testCharacter()
	refs := testItems()
	ref := *refs["ITEM_ETC_HP_POTION_01"]
	ref.TypeIDs = [4]int64{3, 3, 8, 1}
	refs[ref.Codename] = &ref
	c.MissionInventory = []domain.InventoryRow{{Slot: 13, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 10, TradeOwner: "OriginalTrader"}}
	rt, _ := newTestRuntime(c, refs)
	rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 13}))
	drops := rt.Ground.All(testDivision)
	if len(drops) != 1 || drops[0].TradeOwner != "OriginalTrader" || drops[0].StackCount != 10 {
		t.Fatalf("ground identity: %+v", drops)
	}
	// The current holder and public-drop ownership do not change the cargo origin.
	c.Job.Alias = "DifferentHolder"
	result := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drops[0].Gid}.Encode())
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatalf("pickup failed: %+v", result)
	}
	data, err := json.Marshal(c.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var restored domain.Character
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.MissionInventory) != 1 || restored.MissionInventory[0].StackCount != 10 || restored.MissionInventory[0].TradeOwner != "OriginalTrader" {
		t.Fatalf("restored cargo: %+v", restored.MissionInventory)
	}
}

/*
================
TestCargoPurchaseAssignsOwnerBeforeChoosingMergeTarget
================
*/
func TestCargoPurchaseAssignsOwnerBeforeChoosingMergeTarget(t *testing.T) {
	rt, c := merchantFixture(t)
	c.Job.Alias = "Buyer"
	offer := &rt.Commerce.Tabs[1][0]
	offer.Ref.TypeIDs = [4]int64{3, 3, 8, 1}
	offer.Contents = []commerce.Content{{Ref: offer.Ref, Stack: 50}}
	c.MissionInventory = []domain.InventoryRow{{Slot: 13, RefObjID: offer.Ref.RefObjID, Codename: offer.Ref.Codename, TypeFlags: offer.Ref.TypeFlags(), StackCount: 10, TradeOwner: "Foreign"}}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 3})
	if len(c.MissionInventory) != 2 || c.MissionInventory[0].TradeOwner != "Foreign" || c.MissionInventory[0].StackCount != 10 || c.MissionInventory[1].TradeOwner != "Buyer" || c.MissionInventory[1].StackCount != 3 {
		t.Fatalf("cargo origin or merge changed: %+v", c.MissionInventory)
	}
	if offer.Contents[0].TradeOwner != "" {
		t.Fatal("purchase mutated shared merchandise template")
	}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 2})
	if len(c.MissionInventory) != 2 || c.MissionInventory[1].StackCount != 5 || goldOf(c) != 4700 {
		t.Fatalf("same-owner purchase failed: %+v", c.MissionInventory)
	}
}
