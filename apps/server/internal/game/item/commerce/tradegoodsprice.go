/*
===========================================================================

tradegoodsprice.go - the shops' trade goods priced with their references

Port-only, not native (SRO_TRADE_TIERS; enterworld/tradegoodsprice.go): a
specialty shop's policy price for a trade good scales with the good's
reference price, so the purchase stays what the reference says.

===========================================================================
*/
package commerce

import "opensro.online/server/internal/game/enterworld"

/*
================
Catalog.ScaleTradeGoodsPrices

Multiplies the price of every offer that sells trade goods; the count is
the offers changed.
================
*/
func (c *Catalog) ScaleTradeGoodsPrices(factor uint64) int {
	scaled := 0
	for tab, offers := range c.Tabs {
		for i := range offers {
			if offerSellsTradeGoods(offers[i]) {
				offers[i].Price *= factor
				scaled++
			}
		}
		c.Tabs[tab] = offers
	}
	return scaled
}

/*
================
offerSellsTradeGoods
================
*/
func offerSellsTradeGoods(offer Offer) bool {
	if offer.Ref != nil && enterworld.IsTradeGoods(offer.Ref.TypeIDs) {
		return true
	}
	for _, content := range offer.Contents {
		if content.Ref != nil && enterworld.IsTradeGoods(content.Ref.TypeIDs) {
			return true
		}
	}
	return false
}
