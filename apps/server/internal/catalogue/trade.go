/*
===========================================================================

trade.go - C6 trade.json: what each trader pays for trade goods

A trade good sold to a foreign trader is priced by that trader's quotation
(_ItemQuotation, commerce.TradeQuotationPrice, native 4C8810) over the
good's reference price. The sale path admits only a fixed-rate quotation
(lower bound equal to upper), so only those rows are listed. A thief
buyer pays three quarters of the reference price for any good
(ThiefGoodsValue, 4C81D0), which the file states once. Prices are before
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
	NpcID  uint32 `json:"npcId"`
	ItemID uint32 `json:"itemId"`
	// BasePrice is the good's reference price; SellPrice what the trader
	// pays for one; Profit their difference.
	BasePrice uint32 `json:"basePrice"`
	SellPrice uint32 `json:"sellPrice"`
	Profit    int64  `json:"profit"`
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
func BuildTrade(quotations map[[2]uint32]commerce.TradeQuotation, item func(id uint32) *enterworld.ItemRef) []TradeQuote {
	quotes := []TradeQuote{}
	for key, quote := range quotations {
		if quote.Lower != quote.Upper {
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
ThiefRate

ThiefGoodsValue's credit for one unit of a reference price of 100.
================
*/
func ThiefRate() float64 {
	credit, _ := commerce.ThiefGoodsValue(100, 1)
	return float64(credit) / 100
}
