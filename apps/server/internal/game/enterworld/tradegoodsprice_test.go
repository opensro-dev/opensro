/*
===========================================================================

tradegoodsprice_test.go - tests for tradegoodsprice.go

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestTradeGoodsPriceFactorScalesOnlyTradeGoods

On the shipped data: every trade good's price and sell price scale by the
factor, and nothing else changes.
================
*/
func TestTradeGoodsPriceFactorScalesOnlyTradeGoods(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	native, scaled := NewTextdataItems(dir), NewTextdataItems(dir)
	count, err := scaled.ApplyTradeGoodsPriceFactor(TradeGoodsPriceFactor)
	if err != nil || count < 40 {
		t.Fatalf("scaled %d goods (%v)", count, err)
	}
	for _, codename := range []string{"ITEM_ETC_TRADE_CH_01", "ITEM_ETC_TRADE_EU_04", "ITEM_ETC_HP_POTION_01", "ITEM_CH_SWORD_01_A_DEF"} {
		before, _ := native.ItemRefByCodename(codename)
		after, _ := scaled.ItemRefByCodename(codename)
		want := before.NativeFields.Get("price")
		if IsTradeGoods(before.TypeIDs) {
			want *= TradeGoodsPriceFactor
		}
		if after.NativeFields.Get("price") != want {
			t.Fatalf("%s: price %v, want %v", codename, after.NativeFields.Get("price"), want)
		}
	}
	if ref, _ := scaled.ItemRefByCodename("ITEM_ETC_TRADE_CH_01"); ref.NativeFields.Get("price") != 348*TradeGoodsPriceFactor {
		t.Fatalf("CH_01 price %v", ref.NativeFields.Get("price"))
	}
}

/*
================
TestPublishedTradeGoodsCarryTheScaledPrice

The browser reads a trade good's price from the published reference row
(the worker's refItemSnapshot -> item tooltip fields, which the sale
window's purchase sum and profit line read), so scaling the authority's
reference is the client's price too: no separate client factor.
================
*/
func TestPublishedTradeGoodsCarryTheScaledPrice(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	for factor, want := range map[float64]float64{1: 348, TradeGoodsPriceFactor: 348 * TradeGoodsPriceFactor} {
		items := NewTextdataItems(dir)
		if factor != 1 {
			if _, err := items.ApplyTradeGoodsPriceFactor(factor); err != nil {
				t.Fatal(err)
			}
		}
		collector := newRefItemCollector(&Deps{Items: items})
		collector.add("ITEM_ETC_TRADE_CH_01", nil)
		if len(collector.rows) != 1 || collector.rows[0].NativeFields.Get("price") != want {
			t.Fatalf("x%v: published rows %+v, want price %v", factor, collector.rows, want)
		}
	}
}
