/*
===========================================================================

commerce_quotes.go - merchant quotations from the transaction price owner

Goods publish a bounded quantity table: all 47 v1.150 goods have stack caps
10 or 40. This preserves whole-quantity rounding, tax and party shares.

===========================================================================
*/
package action

import (
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

// A display quote is produced by the same pricing owner as the transaction.
// The browser refreshes it when opening a sale; native 706D still revalidates
// the current item and tax under the authority door. It never trusts this price.
/*
================
shopSaleQuote
================
*/
type shopSaleQuote struct {
	CosGID    uint32   `json:"cosGid,omitempty"`
	Totals    []string `json:"totals,omitempty"`
	NoBuyback bool     `json:"noBuyback"`
	Slot      uint8    `json:"slot"`
	Ref       uint32   `json:"refObjId"`
	Quantity  uint16   `json:"quantity"`
	Price     string   `json:"price"`
}

/*
================
shopSaleQuotes
================
*/
func (rt *Runtime) shopSaleQuotes(division string, c *enterworld.Character, npc simulation.NpcDef) []shopSaleQuote {
	quotes := []shopSaleQuote{}
	roster := rt.commerceRoster(division, c)
	rt.deps.Read(division, func() {
		for i := 1; i < len(roster.members); i++ {
			roster.members[i] = roster.members[i].Snapshot()
		}
	})
	type source struct {
		gid   uint32
		items []inventory.Item
	}
	sources := []source{{items: invItemsFromBag(c)}}
	if c.ActiveCOS != nil && c.ActiveCOS.Summoned && c.ActiveCOS.Container != nil {
		sources = append(sources, source{c.ActiveCOS.GID, invItemsFromRowsWithin(c.ActiveCOS.Container.Rows, int64(c.ActiveCOS.Container.Capacity))})
	}
	for _, source := range sources {
		for _, item := range source.items {
			if source.gid == 0 && item.Slot < inventory.EquipmentSlotEnd {
				continue
			}
			ref, found := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
			if !found || ref == nil || ref.RefObjID != item.RefObjID || ref.TypeFlags() != item.TypeFlags || !commerceSaleAdmitted(ref) {
				continue
			}
			if rt.tradeAdmission(division, c, npc, item) != 0 {
				continue
			}
			price, _, valid := commerce.SalePrices(ref, item.MagicOptions, rt.Commerce.Magic, rt.commerceTax(division, npc.RefObjID, c))
			quote := shopSaleQuote{CosGID: source.gid, NoBuyback: commerceNoBuyback(item.TypeFlags, item.Codename), Slot: item.Slot, Ref: item.RefObjID, Quantity: item.Quantity, Price: strconv.FormatUint(price, 10)}
			if inventory.IsTradeGoods(item.TypeFlags) {
				if item.Quantity == 0 || item.Quantity > maxTradeGoodsStack {
					continue
				}
				for amount := uint16(1); amount <= item.Quantity; amount++ {
					part := item
					part.Quantity = amount
					credit, profit, ok := rt.tradeSaleValue(division, c, npc, part)
					if !ok {
						valid = false
						break
					}
					shares := rt.tradeRewards(division, c, roster, commerce.TradeRewardInput{Credit: int64(credit), Profit: profit})
					if shares[0].Gold < 0 {
						valid = false
						break
					}
					quote.Totals = append(quote.Totals, strconv.FormatInt(shares[0].Gold, 10))
				}
				if valid {
					quote.Price = quote.Totals[0]
				}
			}
			if valid {
				quotes = append(quotes, quote)
			}
		}
	}
	return quotes
}

// Retail 5B6D22 tests RefObjData+A5, populated from itemdata column 17.
// Quote and commit share this O(1) policy: a monster drop need not occur in
// any shop catalogue. Missing/malformed authority never grants permission.
/*
================
commerceSaleAdmitted
================
*/
func commerceSaleAdmitted(ref *enterworld.ItemRef) bool {
	if ref == nil {
		return false
	}
	value, present := ref.NativeFields.Lookup("canSell")
	return present && value >= 1 && value <= 255 && value == float64(uint8(value))
}

// v1.150 80B500 fills the reference registry checked by 563F40 on sales.
/*
================
commerceNoBuyback
================
*/
func commerceNoBuyback(flags uint16, codename string) bool {
	if flags&0x7fe == 0x46c || flags&0x7e == 0x4c || flags&0x7fe == 0x6ac {
		return true
	}
	name := strings.ToUpper(codename)
	for _, prefix := range []string{"ITEM_QNO", "ITEM_QTUTORIAL", "ITEM_QSP", "ITEM_QCX", "ITEM_ETC_E", "SN_ITEM_EVENT", "SN_ITEM_ETC_TAIWAN_50000_GOLD"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
