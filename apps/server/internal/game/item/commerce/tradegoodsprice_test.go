/*
===========================================================================

tradegoodsprice_test.go - tests for tradegoodsprice.go

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestShopsPriceTradeGoodsWithTheFactor

On the shipped shops: every specialty offer of trade goods costs the factor
times its v1.150 price, and other offers are untouched.
================
*/
func TestShopsPriceTradeGoodsWithTheFactor(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	native, err := Load(dir, items)
	if err != nil {
		t.Fatal(err)
	}
	scaled, err := Load(dir, items)
	if err != nil {
		t.Fatal(err)
	}
	if n := scaled.ScaleTradeGoodsPrices(enterworld.TradeGoodsPriceFactor); n == 0 {
		t.Fatal("no trade goods offer scaled")
	}
	goods, others := 0, 0
	for tab, offers := range native.Tabs {
		for i, offer := range offers {
			after := scaled.Tabs[tab][i]
			if offerSellsTradeGoods(offer) {
				goods++
				if after.Price != offer.Price*enterworld.TradeGoodsPriceFactor {
					t.Fatalf("tab %d slot %d: %d, want %d", tab, offer.Slot, after.Price, offer.Price*enterworld.TradeGoodsPriceFactor)
				}
			} else if after.Price != offer.Price {
				t.Fatalf("tab %d slot %d: a non-goods offer changed", tab, offer.Slot)
			}
			others++
		}
	}
	if goods < 30 || others <= goods {
		t.Fatalf("goods %d of %d offers", goods, others)
	}
}
