package action

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
)

/*
================
bindMerchantTax

Binds the roster NPCs of reference ref to fortress 1 and sets its ratio;
a non-zero holder occupies the fortress.
================
*/
func bindMerchantTax(t *testing.T, rt *Runtime, ref uint32, rate int16, holder int64) {
	t.Helper()
	if _, ok := rt.Fortresses.Get(testDivision, 1); !ok {
		rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	}
	for i := range rt.NpcRoster {
		if rt.NpcRoster[i].RefObjID == ref {
			rt.NpcRoster[i].FortressID = 1
		}
	}
	if holder != 0 && !rt.Fortresses.Occupy(testDivision, 1, holder) {
		t.Fatal("occupy")
	}
	if record, _ := rt.Fortresses.Get(testDivision, 1); record.TaxRate != rate && !rt.Fortresses.SetTaxRate(testDivision, 1, rate) {
		t.Fatal("tax rate", rate)
	}
}

func TestTaxedCommerceCreditsAndRetentionDiffer(t *testing.T) {
	rt, c := merchantFixture(t)
	bindMerchantTax(t, rt, 100, 20, 0)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 2})
	if goldOf(c) != 4856 {
		t.Fatal("taxed package purchase", goldOf(c))
	}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 2})
	if goldOf(c) != 4880 || len(c.Buyback) != 1 || c.Buyback[0].Price != 30 {
		t.Fatal("sale tax was reused as restoration discount", goldOf(c), c.Buyback)
	}
	// Later tax changes cannot reprice the retained object.
	bindMerchantTax(t, rt, 100, -20, 0)
	buyback(t, rt, c, 17, c.Buyback[0].ID)
	if goldOf(c) != 4850 || len(c.Buyback) != 0 {
		t.Fatal("retained price changed")
	}
}

func TestTaxExemptionAndCatalogUseSameAuthority(t *testing.T) {
	rt, c := merchantFixture(t)
	guild := int64(77)
	c.GuildID = &guild
	bindMerchantTax(t, rt, 100, 20, guild)
	var catalog shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Offers[0].Price != "60" {
		t.Fatal("exempt quote", catalog)
	}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	if goldOf(c) != 4940 {
		t.Fatal("exempt charge disagrees with quote")
	}
	bindMerchantTax(t, rt, 100, -20, 0)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 1})
	if goldOf(c) != 4958 || c.Buyback[0].Price != 18 {
		t.Fatal("exemption erased negative adjustment")
	}
}

func TestBuybackLivesWithLogicalSessionAndNotPersistedCharacter(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.BeginCommerceSession(testDivision, c, 101)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 1})
	before := c.Snapshot()
	rt.BeginCommerceSession(testDivision, c, 101)
	if !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("duplicate entry/resume cleared retained items")
	}
	rt.EndCommerceSession(testDivision, c, 100)
	if !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("stale close cleared another lifetime")
	}
	data, _ := json.Marshal(c)
	var saved map[string]json.RawMessage
	json.Unmarshal(data, &saved)
	if _, exists := saved["buyback"]; exists {
		t.Fatal("ledger persisted")
	}
	rt.BeginCommerceSession(testDivision, c, 102)
	if len(c.Buyback) != 0 || c.BuybackNext != before.BuybackNext {
		t.Fatal("new lifetime retained sold objects or reused IDs")
	}
	c.Buyback = before.Buyback
	rt.EndCommerceSession(testDivision, c, 101)
	if len(c.Buyback) != 1 {
		t.Fatal("old teardown erased new owner")
	}
	rt.EndCommerceSession(testDivision, c, 102)
	if len(c.Buyback) != 0 || c.BuybackSession != 0 {
		t.Fatal("final teardown retained sold objects")
	}
}

/*
================
TestPurchaseTaxFillsTheTreasury

6186D0 credits the bound fortress with the taxed price minus the untaxed
one; an ordinary item sale collects nothing (no sale path calls 486330).
================
*/
func TestPurchaseTaxFillsTheTreasury(t *testing.T) {
	rt, c := merchantFixture(t)
	bindMerchantTax(t, rt, 100, 20, 0)
	treasury := func() int64 {
		record, _ := rt.Fortresses.Get(testDivision, 1)
		return record.TaxGold
	}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 2})
	if goldOf(c) != 4856 || treasury() != 24 {
		t.Fatalf("purchase: gold %d treasury %d", goldOf(c), treasury())
	}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 2})
	if treasury() != 24 {
		t.Fatalf("an item sale reached the treasury: %d", treasury())
	}
	// A negative ratio is a discount: nothing is collected.
	bindMerchantTax(t, rt, 100, -20, 0)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	if treasury() != 24 {
		t.Fatalf("a discount reached the treasury: %d", treasury())
	}
}
