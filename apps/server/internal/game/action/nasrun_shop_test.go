/*
===========================================================================

nasrun_shop_test.go - the Devil's Spirit token shop's purchase

Port-only, not native (#3456 step 3b). A token offer is paid from the
buyer's own balance inside the purchase, refused silently when short,
never touches gold, and the shop is sent again with the new balance.

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
tokenShopBalance

The nasrunTokens field of the last shop catalogue in frames, and
whether one was sent.
================
*/
func tokenShopBalance(t *testing.T, frames []wire.Frame) (*int64, bool) {
	t.Helper()
	var balance *int64
	sent := false
	for _, frame := range frames {
		if frame.Opcode != opShopCatalog {
			continue
		}
		var p shopProjection
		if err := json.Unmarshal(frame.Payload, &p); err != nil {
			t.Fatal(err)
		}
		balance, sent = p.NasrunTokens, true
	}
	return balance, sent
}

/*
================
TestTokenOfferSpendsTheBalanceNotGold
================
*/
func TestTokenOfferSpendsTheBalanceNotGold(t *testing.T) {
	rt, c := merchantFixture(t)
	gold := int64(1000)
	c.Gold = &gold
	c.MissionInventory = nil
	offer := rt.Commerce.Tabs[1][0]
	offer.Price, offer.Currency, offer.HonorPrice, offer.GuildPointPrice = 60, commerce.PaymentNasrunToken, 0, 0
	rt.Commerce.Tabs[1][0] = offer
	buy := wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopTab: 0, ShopSlot: 2, Quantity: 1}

	// Short (50 < 60): the native silent refusal, and nothing moves.
	c.NasrunTokens = 50
	res := trade(t, rt, c, buy)
	if len(res.Frames) != 1 || len(res.Frames[0].Payload) != 2 || res.Frames[0].Payload[0] != 2 || res.Frames[0].Payload[1] != wire.ErrCodeInvalidRequest {
		t.Fatalf("short token response = %+v, want the silent refusal", res.Frames)
	}
	if goldOf(c) != 1000 || c.NasrunTokens != 50 || len(c.MissionInventory) != 0 {
		t.Fatalf("short tokens mutated state: gold=%d tokens=%d inv=%d", goldOf(c), c.NasrunTokens, len(c.MissionInventory))
	}

	// Enough (130 >= 60): the tokens pay, gold is untouched (no town tax
	// on tokens), and the shop comes back with the new balance.
	c.NasrunTokens = 130
	res = trade(t, rt, c, buy)
	if goldOf(c) != 1000 || c.NasrunTokens != 70 || len(c.MissionInventory) != 1 {
		t.Fatalf("settled state: gold=%d tokens=%d inv=%d, want 1000/70/1", goldOf(c), c.NasrunTokens, len(c.MissionInventory))
	}
	balance, sent := tokenShopBalance(t, res.Frames)
	if !sent || balance == nil || *balance != 70 {
		t.Fatalf("shop after the buy: sent=%v balance=%v, want 70", sent, balance)
	}
}

/*
================
TestGoldPurchaseSendsNoTokenShop

A gold offer neither spends tokens nor resends the shop, and a shop with
no token offer carries no balance.
================
*/
func TestGoldPurchaseSendsNoTokenShop(t *testing.T) {
	rt, c := merchantFixture(t)
	gold := int64(1000)
	c.Gold = &gold
	c.MissionInventory = nil
	c.NasrunTokens = 130
	offer := rt.Commerce.Tabs[1][0]
	offer.Price, offer.Currency, offer.HonorPrice, offer.GuildPointPrice = 400, commerce.PaymentGold, 0, 0
	rt.Commerce.Tabs[1][0] = offer
	res := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopTab: 0, ShopSlot: 2, Quantity: 1})
	if c.NasrunTokens != 130 || len(c.MissionInventory) != 1 {
		t.Fatalf("gold buy: tokens=%d inv=%d, want 130/1", c.NasrunTokens, len(c.MissionInventory))
	}
	if _, sent := tokenShopBalance(t, res.Frames); sent {
		t.Fatal("a gold buy resent the shop")
	}
	if balance, _ := tokenShopBalance(t, []wire.Frame{rt.shopCatalog(testDivision, c, 17)}); balance != nil {
		t.Fatalf("a shop with no token offer sent a balance %d", *balance)
	}
}

/*
================
TestTokenGoodsTravelApartFromTheStock

A client built before the token shop refuses an unknown payment in
offers and loses its session on it, so token goods go in tokenOffers,
which that client never reads; the native stock stays in offers.
================
*/
func TestTokenGoodsTravelApartFromTheStock(t *testing.T) {
	rt, c := merchantFixture(t)
	c.NasrunTokens = 42
	gold := rt.Commerce.Tabs[1][0]
	token := gold
	token.Slot, token.Price, token.Currency = 3, 60, commerce.PaymentNasrunToken
	rt.Commerce.Tabs[1] = append(rt.Commerce.Tabs[1], token)
	var p shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Offers) != 1 || p.Offers[0].Currency == commerce.PaymentNasrunToken {
		t.Fatalf("offers = %+v, want the gold offer alone", p.Offers)
	}
	if len(p.TokenOffers) != 1 || p.TokenOffers[0].Slot != 3 || p.TokenOffers[0].Currency != commerce.PaymentNasrunToken {
		t.Fatalf("tokenOffers = %+v, want the token offer alone", p.TokenOffers)
	}
	if p.NasrunTokens == nil || *p.NasrunTokens != 42 {
		t.Fatalf("balance = %v, want 42", p.NasrunTokens)
	}
}

/*
================
TestNoMerchantBuysASpirit

A Spirit is bought with tokens; selling it for gold would turn tokens
into gold, so the sale is refused even when its row says canSell.
================
*/
func TestNoMerchantBuysASpirit(t *testing.T) {
	rt, c := merchantFixture(t)
	gold := int64(1000)
	c.Gold = &gold
	c.MissionInventory = nil
	offer := rt.Commerce.Tabs[1][0]
	// The item source's own row is the offer's, so the bag row the buy
	// makes and the sale's lookup agree that it is a Spirit (3/1/14).
	offer.Ref.TypeIDs = [4]int64{3, 1, 14, 1}
	offer.Price, offer.Stack = 100, 1
	rt.Commerce.Tabs[1][0] = offer
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	if len(c.MissionInventory) != 1 || goldOf(c) != 900 {
		t.Fatalf("setup buy: inv=%d gold=%d", len(c.MissionInventory), goldOf(c))
	}
	slot := c.MissionInventory[0].Slot
	res := trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: uint8(slot), Quantity: 1})
	if len(c.MissionInventory) != 1 || goldOf(c) != 900 {
		t.Fatalf("a Spirit sold: inv=%d gold=%d frames=%+v", len(c.MissionInventory), goldOf(c), res.Frames)
	}
	if len(res.Frames) != 1 || res.Frames[0].Payload[len(res.Frames[0].Payload)-1] != wire.ErrCodeCannotDealAtShop {
		t.Fatalf("sale refusal = %+v, want cannot deal", res.Frames)
	}
}
