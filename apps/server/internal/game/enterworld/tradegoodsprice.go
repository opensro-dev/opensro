/*
===========================================================================

tradegoodsprice.go - trade goods worth twenty times their v1.150 price

Port-only, not native (owner decision 2026-10-14, SRO_TRADE_TIERS, for the
balance advisor). Every v1.150 trade good (TID 3/3/8) has reference price
348, so a full cargo is worth 1.4M-2.5M and the advisor's star marks
(5M/10M/20M/35M/50M) were out of reach. With the rule on, each good's
reference price and sell price are scaled once at load, before any owner
reads them, and the shops' policy prices with them (commerce): purchase,
sale quotation, thief valuation, origin buyback, cargo value and stars,
the client's tooltips (the published references) and the website all agree.
Profit, and the job EXP, hunter share and weekly pool that derive from it,
scale by the same factor.

===========================================================================
*/
package enterworld

import "fmt"

// TradeGoodsPriceFactor multiplies a trade good's prices under the rule.
const TradeGoodsPriceFactor = 20

/*
================
IsTradeGoods

A trade good's type IDs: 3/3/8 (ITEM_ETC_TRADE_*).
================
*/
func IsTradeGoods(typeIDs [4]int64) bool {
	return typeIDs[0] == 3 && typeIDs[1] == 3 && typeIDs[2] == 8
}

/*
================
ApplyTradeGoodsPriceFactor

Scales every trade good's reference and sell price before any owner reads
them; the count is the goods changed.
================
*/
func (t *TextdataItems) ApplyTradeGoodsPriceFactor(factor float64) (int, error) {
	t.once.Do(t.load)
	if t.archive != nil {
		return 0, fmt.Errorf("trade goods prices: item references are already archived")
	}
	scaled := 0
	for _, ref := range t.byCodename {
		if ref == nil || !IsTradeGoods(ref.TypeIDs) {
			continue
		}
		for _, field := range []string{"price", "sellPrice"} {
			if value, present := ref.NativeFields.Lookup(field); present {
				ref.NativeFields = ref.NativeFields.With(field, value*factor)
			}
		}
		scaled++
	}
	return scaled, nil
}
