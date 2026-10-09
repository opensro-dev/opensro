package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

func TestMerchantSaleQuoteMatchesTaxedCommitAndRefreshesPartialStack(t *testing.T) {
	rt, c := merchantFixture(t)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 4})
	bindMerchantTax(t, rt, 100, 20, 0)
	read := func() shopProjection {
		var p shopProjection
		if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := read()
	if len(p.SaleQuotes) != 1 || p.SaleQuotes[0].Price != "12" || p.SaleQuotes[0].Quantity != 4 {
		t.Fatalf("quote: %+v", p.SaleQuotes)
	}
	slot := p.SaleQuotes[0].Slot
	before := goldOf(c)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: slot, Quantity: 3})
	if goldOf(c) != before+36 {
		t.Fatal("quote and payout diverged")
	}
	p = read()
	if len(p.SaleQuotes) != 1 || p.SaleQuotes[0].Quantity != 1 || len(p.Buyback) != 1 || p.Buyback[0].Price != "45" {
		t.Fatalf("partial sale/retention: %+v", p)
	}
	rt.Selected.Clear(testDivision, c.Name)
	p = read()
	if len(p.SaleQuotes) != 0 || p.Error == "" {
		t.Fatal("unselected merchant leaked actionable quotes")
	}
}

func TestMerchantSellsPermittedItemsWithoutShopMembership(t *testing.T) {
	rt, c := merchantFixture(t)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 4})
	// The source of an owned item is irrelevant: after this point there is no
	// offer for it anywhere, exactly as for ordinary non-stocked monster loot.
	clear(rt.Commerce.Tabs)
	var quote shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
		t.Fatal(err)
	}
	if len(quote.SaleQuotes) != 1 {
		t.Fatal("non-stocked item has no sale quote")
	}
	before := goldOf(c)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: quote.SaleQuotes[0].Slot, Quantity: 2})
	if goldOf(c) != before+30 || len(c.Buyback) != 1 {
		t.Fatal("non-stocked item could not be sold and retained")
	}
}

func TestMerchantSalePermissionRevocationInvalidatesQuoteAndCommit(t *testing.T) {
	rt, c := merchantFixture(t)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 4})
	var quote shopProjection
	json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote)
	slot := quote.SaleQuotes[0].Slot
	ref := rt.Commerce.Tabs[1][0].Ref
	for _, permission := range []float64{0, -1, 256, 1.5} {
		ref.NativeFields = ref.NativeFields.With("canSell", permission)
		before, _ := json.Marshal(c)
		var denied shopProjection
		json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &denied)
		if len(denied.SaleQuotes) != 0 {
			t.Fatal("permission-free quote")
		}
		trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: slot, Quantity: 1})
		after, _ := json.Marshal(c)
		if string(before) != string(after) {
			t.Fatal("revoked sale changed authority")
		}
	}
}

func TestNativeNoBuybackReferenceClasses(t *testing.T) {
	for _, row := range []struct {
		flags uint16
		code  string
		want  bool
	}{
		{0x8ec, "ITEM_ETC_HP_POTION_01", false}, {0x12c, "ITEM_CH_M_HEAVY_01_HA_A", false},
		{0x46c, "ITEM_ETC_SPECIAL", true}, {0x4c, "ITEM_COS", true}, {0x6ac, "ITEM_MALL_AVATAR", true},
		{0x8ec, "item_qno_test", true}, {0x8ec, "ITEM_ETC_E0601", true},
	} {
		if got := commerceNoBuyback(row.flags, row.code); got != row.want {
			t.Errorf("%x %s: %v", row.flags, row.code, got)
		}
	}
}

func TestNoBuybackSaleCreditsWithoutRetaining(t *testing.T) {
	rt, c := merchantFixture(t)
	items := rt.deps.ItemReferences().(staticItemSource)
	ref := items["ITEM_ETC_HP_POTION_01"]
	delete(items, ref.Codename)
	ref.Codename = "ITEM_QNO_COMMERCE_TEST"
	items[ref.Codename] = ref
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 4})
	var p shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.SaleQuotes) != 1 || !p.SaleQuotes[0].NoBuyback {
		t.Fatalf("missing warning: %+v", p.SaleQuotes)
	}
	before := goldOf(c)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: p.SaleQuotes[0].Slot, Quantity: 4})
	if goldOf(c) != before+60 || len(c.Buyback) != 0 {
		t.Fatalf("sale/ledger mismatch: gold=%d ledger=%+v", goldOf(c), c.Buyback)
	}
}
