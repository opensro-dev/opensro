/*
===========================================================================

trade.go - C6 trade.json: what each trader pays for trade goods

A trade good sold to a foreign trader is priced by that trader's quotation
(_ItemQuotation, commerce.TradeQuotationPrice, the native quotation) over the
good's reference price. The sale path admits only a fixed-rate quotation
(lower bound equal to upper), so only those rows are listed. A thief
buyer pays three quarters of the reference price for any good
(ThiefGoodsValue), which the file states once. Prices are before
the fortress tax, which depends on the fortress's owner at the time.

===========================================================================
*/
package catalogue

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
TradeQuote
================
*/
type TradeQuote struct {
	NpcID     uint32 `json:"npcId"`
	ItemID    uint32 `json:"itemId"`
	BasePrice uint32 `json:"basePrice" doc:"The good's own reference price (the quotation formula's input), not the price at its origin shop (that is the good's price in the origin trader's tab in npcs.json)."`
	SellPrice uint32 `json:"sellPrice" doc:"What this trader pays for one, before the fortress tax."`
	Profit    int64  `json:"profit" doc:"sellPrice minus basePrice."`
	// RouteBonus is the shard's route profit percent (action/tradetiers.go).
	RouteBonus int64 `json:"routeBonus,omitempty" doc:"Port-only (SRO_TRADE_TIERS): the percent this route adds to the profit, Jangan to or from Donwhang 50, Hotan 100, Samarkand 200, Constantinople 300. sellPrice and profit already include it."`
}

/*
================
TradeFile
================
*/
type TradeFile struct {
	Header
	// ThiefRate is the share of the reference price a thief buyer pays.
	ThiefRate float64      `json:"thiefRate"`
	Quotes    []TradeQuote `json:"quotes"`
}

/*
================
BuildTrade
================
*/
func BuildTrade(quotations map[[2]uint32]commerce.TradeQuotation, item func(id uint32) *enterworld.ItemRef, trader func(id uint32) bool) []TradeQuote {
	quotes := []TradeQuote{}
	for key, quote := range quotations {
		// The imported quotation table also prices traders of later towns
		// that v1.150 has no reference for; only a placed v1.150 NPC trades.
		if quote.Lower != quote.Upper || trader != nil && !trader(key[0]) {
			continue
		}
		ref := item(key[1])
		if ref == nil {
			continue
		}
		base, ok := ref.NativeFields.Lookup("price")
		if !ok || base < 1 {
			continue
		}
		price, err := commerce.TradeQuotationPrice(uint32(base), quote)
		if err != nil {
			continue
		}
		quotes = append(quotes, TradeQuote{NpcID: key[0], ItemID: key[1], BasePrice: uint32(base), SellPrice: price,
			Profit: int64(price) - int64(uint32(base))})
	}
	sort.Slice(quotes, func(i, j int) bool {
		if quotes[i].NpcID != quotes[j].NpcID {
			return quotes[i].NpcID < quotes[j].NpcID
		}
		return quotes[i].ItemID < quotes[j].ItemID
	})
	return quotes
}

/*
================
ApplyTradeRoutes

Raises each quote by its route's profit percent (SRO_TRADE_TIERS); raise
is the GameWorld's own arithmetic, so the site shows what the shard pays.
================
*/
func ApplyTradeRoutes(quotes []TradeQuote, percent func(npcID, itemID uint32) int64, raise func(credit, profit, percent int64) (int64, int64)) {
	for i := range quotes {
		bonus := percent(quotes[i].NpcID, quotes[i].ItemID)
		if bonus <= 0 {
			continue
		}
		credit, profit := raise(int64(quotes[i].SellPrice), quotes[i].Profit, bonus)
		quotes[i].SellPrice, quotes[i].Profit, quotes[i].RouteBonus = uint32(min(credit, int64(^uint32(0)))), profit, bonus
	}
}

/*
================
ThiefRate

ThiefGoodsValue's credit for one unit of a reference price of 100.
================
*/
func ThiefRate() float64 {
	credit, _ := commerce.ThiefGoodsValue(100, 1)
	return float64(credit) / 100
}
