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
	if balance, _ := tokenShopBalance(t, []wire.Frame{rt.shopCatalog("", c, 17)}); balance != nil {
		t.Fatalf("a shop with no token offer sent a balance %d", *balance)
	}
}
