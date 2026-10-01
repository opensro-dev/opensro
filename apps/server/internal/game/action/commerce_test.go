package action

import (
	"encoding/json"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"reflect"
	"testing"
)

func TestShopCatalogPublishesAuthoredTabIdentity(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].NpcTalkStoreGroups[0].Tabs[0].LabelSymbol = "SN_TAB_WEAPON"
	var projection shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &projection); err != nil {
		t.Fatal(err)
	}
	if len(projection.Tabs) != 1 || projection.Tabs[0].Index != 0 || projection.Tabs[0].LabelSymbol != "SN_TAB_WEAPON" {
		t.Fatalf("lost authored tab: %+v", projection)
	}
	if len(projection.Offers) != 1 || projection.Offers[0].Tab != projection.Tabs[0].Index {
		t.Fatalf("tab identity differs from transaction: %+v", projection)
	}
}

func merchantFixture(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	ref := items["ITEM_ETC_HP_POTION_01"]
	ref.NativeFields = ref.NativeFields.With("sellPrice", 15)
	ref.NativeFields = ref.NativeFields.With("canSell", 1)
	rt, _ := newTestRuntime(c, items)
	spawn := simulation.SeedWorldState(c).Spawn
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
	rt.Selected.Set(testDivision, c.Name, 17)
	// An in-range shop request opened the merchant (npcrange.go).
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{1: {{Slot: 2, Ref: ref, Price: 60, Stack: 50}}}}
	return rt, c
}
func trade(t *testing.T, rt *Runtime, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	t.Helper()
	p, e := q.Encode()
	if e != nil {
		t.Fatal(e)
	}
	return rt.HandleItemMove(testDivision, c, p)
}
func TestShopBuySellAtomic(t *testing.T) {
	rt, c := merchantFixture(t)
	q := wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 70}
	r := trade(t, rt, c, q)
	if len(r.Frames) != 4 || r.Frames[0].Opcode != 14 || r.Frames[1].Opcode != 12 || goldOf(c) != 800 || len(c.MissionInventory) != 3 {
		t.Fatalf("purchase: %+v gold %d rows %+v", r, goldOf(c), c.MissionInventory)
	}
	q = wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 20}
	r = trade(t, rt, c, q)
	if len(r.Frames) != 3 || r.Frames[2].Opcode != opShopBuyback || goldOf(c) != 1100 || c.MissionInventory[1].StackCount != 30 {
		t.Fatalf("sale: %+v %+v", r, c.MissionInventory)
	}
}
func TestShopRefusalsNeverMutate(t *testing.T) {
	for _, kind := range []string{"funds", "slot", "merchant", "distance", "equipment", "quantity", "full"} {
		t.Run(kind, func(t *testing.T) {
			rt, c := merchantFixture(t)
			q := wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1}
			switch kind {
			case "funds":
				q.Quantity = 100
			case "slot":
				q.ShopSlot = 3
			case "merchant":
				q.NpcGID = 18
			case "distance":
				rt.NpcRoster[0].Spawn.X += 1000
			case "equipment":
				q.MovementType = 9
				q.SourceSlot = 20
			case "quantity":
				q.MovementType = 9
				q.SourceSlot = 99
			case "full":
				for slot := int64(13); slot < 109; slot++ {
					if slot == 20 {
						continue
					}
					c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1})
				}
			}
			before := c.Snapshot()
			r := trade(t, rt, c, q)
			if len(r.Frames) != 1 || r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(c.MissionInventory, before.MissionInventory) || goldOf(c) != goldOf(before) {
				t.Fatalf("refusal mutated: %+v", r)
			}
		})
	}
}

func TestPackagePurchaseLimitPreservesAuthoredData(t *testing.T) {
	rt, c := merchantFixture(t)
	read := func() uint16 {
		var p shopProjection
		if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p.Offers[0].PurchaseLimit
	}
	if got := read(); got != 50 {
		t.Fatalf("ordinary stack: %d", got)
	}
	o := rt.Commerce.Tabs[1][0]
	rt.Commerce.Tabs[1][0].Contents = []commerce.Content{{Ref: o.Ref, Stack: o.Stack, Data: 1}}
	if got := read(); got != 5 {
		t.Fatalf("preset bundle Data=1: %d", got)
	}
	rt.Commerce.Tabs[1][0].Contents = append(rt.Commerce.Tabs[1][0].Contents, commerce.Content{Ref: o.Ref, Stack: o.Stack})
	if got := read(); got != 5 {
		t.Fatalf("multi-item package: %d", got)
	}
}

/*
================
TestShopPurchaseRefusalsNameTheirCause

A refused purchase answers the native code the client turns into a notice:
0x0F without the gold, 0xD4 for an honor package (no Training Camp honor).
================
*/
func TestShopPurchaseRefusalsNameTheirCause(t *testing.T) {
	rt, c := merchantFixture(t)
	setGold(c, 0)
	rows := len(c.MissionInventory)
	q := wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1}
	r := trade(t, rt, c, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeNotEnoughGold)) {
		t.Fatalf("gold refusal: %+v", r)
	}
	offer := rt.Commerce.Tabs[1][0]
	offer.Currency = commerce.PaymentHonor
	rt.Commerce.Tabs[1] = []commerce.Offer{offer}
	r = trade(t, rt, c, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeNotEnoughHonor)) {
		t.Fatalf("honor refusal: %+v", r)
	}
	if goldOf(c) != 0 || len(c.MissionInventory) != rows {
		t.Fatalf("a refused purchase changed the character: gold %d rows %d", goldOf(c), len(c.MissionInventory))
	}
}
