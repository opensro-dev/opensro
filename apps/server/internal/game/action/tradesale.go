/*
===========================================================================

tradesale.go - special and thief merchant admission and cargo valuation

617F70 admits the sale; 4C8D10 values the complete quantity before tax.
Quotation calls are read-only. Inventory and rewards commit in commerce.go.

===========================================================================
*/
package action

import (
	"math"
	"slices"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	tradeErrWrongMerchant   uint8 = 0x45
	tradeErrOwner           uint8 = 0x68
	tradeErrVehicleDistance uint8 = 0x65
	tradeErrQuest           uint8 = 0xb0
	tradeErrJob             uint8 = 5
	// v1.150's 47 authored goods rows have stack caps 10 or 40.
	maxTradeGoodsStack uint16  = 40
	tradeVehicleRange  float64 = 1000.0
)

/*
================
tradeAdmission
================
*/
func (rt *Runtime) tradeAdmission(division string, c *enterworld.Character, npc simulation.NpcDef, item inventory.Item) uint8 {
	special := npc.TalkFlags&simulation.NpcTalkFlagSpecialTrade != 0
	if !inventory.IsTradeGoods(item.TypeFlags) {
		if special {
			return tradeErrWrongMerchant
		}
		return 0
	}
	job := enterworld.DressedJob(c)
	if job != domain.JobTrader && job != domain.JobThief {
		return tradeErrJob
	}
	if !special {
		return tradeErrWrongMerchant
	}
	if quest := commerce.RequiredTradeQuest(npc.Codename); quest != "" {
		id := rt.Commerce.TradeQuests[quest]
		if id == 0 || !slices.Contains(c.CompletedQuestIds, id) {
			return tradeErrQuest
		}
	}
	if item.TradeOwner != c.Job.Alias && npc.TalkFlags&simulation.NpcTalkFlagThiefBuy == 0 {
		return tradeErrOwner
	}
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned || c.ActiveCOS.CurrentHP <= 0 {
		return tradeErrVehicleDistance
	}
	ref, valid := rt.cosReference(c.ActiveCOS)
	if !valid || !isVehicleCOS(ref.TidWord) {
		return tradeErrVehicleDistance
	}
	now := rt.Now().UnixMilli()
	a := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	b := rt.cosLiveSpawn(division, c, now)
	if !(math.Hypot(simulation.WorldDistance2D(a, b), a.Y-b.Y) <= tradeVehicleRange) {
		return tradeErrVehicleDistance
	}
	return 0
}

/*
================
merchantStocksItem

4C81F0/867660: the origin merchant buys its own merchandise at SellPrice.
================
*/
func (rt *Runtime) merchantStocksItem(npc simulation.NpcDef, ref uint32) bool {
	found := false
	rt.eachShopOffer(npc, func(_ uint8, offer commerce.Offer) {
		if len(offer.Contents) == 0 {
			found = found || offer.Ref != nil && offer.Ref.RefObjID == ref
			return
		}
		for _, entry := range offer.Contents {
			found = found || entry.Ref != nil && entry.Ref.RefObjID == ref
		}
	})
	return found
}

/*
================
tradeSaleValue

The native foreign sale multiplies both unit prices as uint32 before their
signed 64-bit difference. Thief valuation sign-extends the IMUL result.
================
*/
func (rt *Runtime) tradeSaleValue(division string, c *enterworld.Character, npc simulation.NpcDef, item inventory.Item) (uint64, int64, bool) {
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil || ref.RefObjID != item.RefObjID || ref.TypeFlags() != item.TypeFlags {
		return 0, 0, false
	}
	if item.Quantity == 0 || item.Quantity > maxTradeGoodsStack || item.Quantity > rt.maxStackFor(item.TypeFlags, item.Codename) {
		return 0, 0, false
	}
	base, present := ref.NativeFields.Lookup("price")
	if !present || base < 1 || base > math.MaxUint32 || base != math.Trunc(base) {
		return 0, 0, false
	}
	var credit, profit int64
	switch {
	case npc.TalkFlags&simulation.NpcTalkFlagThiefBuy != 0:
		credit, profit = commerce.ThiefGoodsValue(uint32(base), item.Quantity)
	case rt.merchantStocksItem(npc, item.RefObjID):
		unit, valid := commerce.SaleUnitPrice(ref, item.MagicOptions, rt.Commerce.Magic)
		if !valid {
			return 0, 0, false
		}
		credit = int64(uint32(unit) * uint32(item.Quantity))
	default:
		quote, found := rt.Commerce.TradeQuotations[[2]uint32{npc.RefObjID, item.RefObjID}]
		if !found || quote.Lower != quote.Upper {
			return 0, 0, false
		}
		unit, err := commerce.TradeQuotationPrice(uint32(base), quote)
		if err != nil {
			return 0, 0, false
		}
		credit = int64(unit * uint32(item.Quantity))
		profit = credit - int64(uint32(base)*uint32(item.Quantity))
	}
	taxed, valid := commerce.AdjustPrice(uint64(max(credit, 0)), rt.commerceTax(division, npc.RefObjID, c), false)
	return taxed, max(profit, 0), valid
}
