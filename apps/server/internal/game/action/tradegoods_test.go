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
	"opensro.online/server/internal/game/enterworld"
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
	// The current holder and public-drop ownership do not change the cargo
	// origin. A thief with a transport may take another's goods;
	// they land in its cargo.
	c.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 1, Alias: "DifferentHolder"}
	c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: 8, RefObjID: 200, Codename: "SUIT",
		TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobThief), StackCount: 1})
	deps := rt.deps.(*enterworld.Deps)
	deps.Items = testCosSource(deps.Items.(staticItemSource))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &domain.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 4}}
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
	cargo := restored.ActiveCOS.Container.Rows
	if len(cargo) != 1 || cargo[0].StackCount != 10 || cargo[0].TradeOwner != "OriginalTrader" {
		t.Fatalf("restored cargo: %+v", cargo)
	}
}

/*
================
TestCargoPurchaseAssignsOwnerBeforeChoosingMergeTarget
================
*/
func TestCargoPurchaseAssignsOwnerBeforeChoosingMergeTarget(t *testing.T) {
	rt, c := traderFixture(t, false)
	c.Job.Alias = "Buyer"
	ref, _ := rt.deps.ItemReferences().ItemRefByCodename(c.MissionInventory[1].Codename)
	rt.Commerce.Tabs[1] = []commerce.Offer{{Slot: 2, Ref: ref, Price: 60, Stack: 40, Contents: []commerce.Content{{Ref: ref, Stack: 40}}}}
	// Goods load into the transport only (buysIntoTransport): a foreign
	// stack already in its cargo must stay apart from the buyer's.
	foreign := c.MissionInventory[1]
	foreign.Slot, foreign.TradeOwner, foreign.StackCount = 0, "Foreign", 10
	c.MissionInventory = c.MissionInventory[:1]
	c.ActiveCOS.Container.Rows = []domain.InventoryRow{foreign}
	buy := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 3, CosGID: c.ActiveCOS.GID}
	trade(t, rt, c, buy)
	cargo := c.ActiveCOS.Container.Rows
	if len(cargo) != 2 || cargo[0].TradeOwner != "Foreign" || cargo[0].StackCount != 10 || cargo[1].TradeOwner != "Buyer" || cargo[1].StackCount != 3 {
		t.Fatalf("cargo origin or merge changed: %+v", cargo)
	}
	if rt.Commerce.Tabs[1][0].Contents[0].TradeOwner != "" {
		t.Fatal("purchase mutated shared merchandise template")
	}
	buy.Quantity = 2
	trade(t, rt, c, buy)
	cargo = c.ActiveCOS.Container.Rows
	if len(cargo) != 2 || cargo[1].StackCount != 5 || goldOf(c) != 4700 {
		t.Fatalf("same-owner purchase failed: %+v", cargo)
	}
}
