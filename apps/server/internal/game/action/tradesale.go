/*
===========================================================================

tradesale.go - special and thief merchant admission and cargo valuation

Natively the special trade admits the sale and the purchase check a purchase; the quote values the complete quantity before tax.
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
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	tradeErrWrongMerchant   uint8 = 0x45
	tradeErrOwner           uint8 = 0x68
	tradeErrVehicleDistance uint8 = 0x65
	tradeErrQuest           uint8 = 0xb0
	tradeErrJob             uint8 = 5
	// tradeErrOnlyTrader is the native purchase check's 0x1860 (UIIT_MSG_STRGERR_ONLY_TRADER_
	// CAN_BUY_SPECIALTY); tradeErrBuyerLevel its 0x184E, which the v1.150
	// client shows no text for.
	tradeErrOnlyTrader uint8 = 0x60
	tradeErrBuyerLevel uint8 = 0x4e
	// minTradeBuyerLevel is the native purchase check's level floor (level < 0x14).
	minTradeBuyerLevel int64 = 20
	// tradeErrOverStack is the native sale's 0x1815: a sale above the item's stack
	// cap (the client shows ENABLEINPUT_FEWERTHAN_REMAIN, as for 0x14).
	tradeErrOverStack uint8 = 0x15
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
	// The native sale refuses job state 3 (a hunter's suit) and 4 (no job suit):
	// only a suited trader or thief sells goods.
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
	if !rt.transportSummoned(c) {
		return tradeErrVehicleDistance
	}
	if rt.transportBeyond(division, c, tradeVehicleRange) {
		return tradeErrVehicleDistance
	}
	return 0
}

/*
================
tradePurchaseAdmission

The native purchase check, the trade goods arm of a shop purchase,
in its order: it admits only a player whose job state
(the worn suit's kind) is the trader's 1 and whose first
COS is a transport, else 0x1860; then level 20 (0x184E), the merchant's
required trade quest (0x18B0) and the transport within 1000 (0x1865). The
0x404 branch refuses only the TID4 2 goods v1.150 never sells.
================
*/
func (rt *Runtime) tradePurchaseAdmission(division string, c *enterworld.Character, npc simulation.NpcDef) uint8 {
	// INFERENCE: the native purchase check has no merchant test of its own; only special
	// traders stock goods, so another merchant is refused as the native sale would.
	if npc.TalkFlags&simulation.NpcTalkFlagSpecialTrade == 0 {
		return tradeErrWrongMerchant
	}
	if enterworld.DressedJob(c) != domain.JobTrader || !rt.transportSummoned(c) {
		return tradeErrOnlyTrader
	}
	if c.Level == nil || *c.Level < minTradeBuyerLevel {
		return tradeErrBuyerLevel
	}
	if quest := commerce.RequiredTradeQuest(npc.Codename); quest != "" {
		id := rt.Commerce.TradeQuests[quest]
		if id == 0 || !slices.Contains(c.CompletedQuestIds, id) {
			return tradeErrQuest
		}
	}
	if rt.transportBeyond(division, c, tradeVehicleRange) {
		return tradeErrVehicleDistance
	}
	return 0
}

/*
================
transportSummoned

Whether the player's live COS is a summoned transport (as natively).
================
*/
func (rt *Runtime) transportSummoned(c *enterworld.Character) bool {
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned || c.ActiveCOS.CurrentHP <= 0 {
		return false
	}
	ref, valid := rt.cosReference(c.ActiveCOS)
	return valid && isVehicleCOS(ref.TidWord)
}

/*
================
transportBeyond

Whether the owner's summoned transport stands farther than limit from its
owner, measured in 3D between their live positions (the relative vector's
length, as the native sale and teleport measure it). A position that is not finite
counts as beyond.
================
*/
func (rt *Runtime) transportBeyond(division string, c *enterworld.Character, limit float64) bool {
	now := rt.Now().UnixMilli()
	a := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	b := rt.cosLiveSpawn(division, c, now)
	return !(math.Hypot(simulation.WorldDistance2D(a, b), a.Y-b.Y) <= limit)
}

/*
================
merchantStocksItem

Natively the origin merchant buys its own merchandise at SellPrice.
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
signed 64-bit difference. Thief valuation sign-extends the IMUL result. s is
the sale's one snapshot of the live tunables.
================
*/
func (rt *Runtime) tradeSaleValue(division string, c *enterworld.Character, npc simulation.NpcDef, item inventory.Item, s *tunables.Settings) (uint64, int64, int64, bool) {
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil || ref.RefObjID != item.RefObjID || ref.TypeFlags() != item.TypeFlags {
		return 0, 0, 0, false
	}
	if item.Quantity == 0 || item.Quantity > maxTradeGoodsStack || item.Quantity > rt.maxStackFor(item.TypeFlags, item.Codename) {
		return 0, 0, 0, false
	}
	base, present := ref.NativeFields.Lookup("price")
	if !present || base < 1 || base > math.MaxUint32 || base != math.Trunc(base) {
		return 0, 0, 0, false
	}
	var credit, profit int64
	switch {
	case npc.TalkFlags&simulation.NpcTalkFlagThiefBuy != 0:
		credit, profit = commerce.ThiefGoodsValue(uint32(base), item.Quantity)
	case rt.merchantStocksItem(npc, item.RefObjID):
		unit, valid := commerce.SaleUnitPrice(ref, item.MagicOptions, rt.Commerce.Magic)
		if !valid {
			return 0, 0, 0, false
		}
		credit = int64(uint32(unit) * uint32(item.Quantity))
	default:
		// Version-scope inference: v1.188 clears trade safety and
		// emits 34D5 here. v1.150 75AF59 consumes only the special-shop
		// mode byte, without the later safety limits/counters, and its
		// client has no 34D5 instruction operand or safety UI symbols.
		// This port therefore has no later-version safety state to reset.
		quote, found := rt.Commerce.TradeQuotations[[2]uint32{npc.RefObjID, item.RefObjID}]
		if !found {
			// Native (the reference server's trade quote,
			// ASM-verified): a foreign good with no quotation entry is
			// not refused. The native logs "CANNOT FIND QUOT DATA"
			// and sells at the item's base sell price
			// ([RefObjData+0xB0] * quantity), taxed below like
			// every path, with the profit output left at zero.
			// Unquoted trade goods (such as Asia Minor's TRADE_AM_01..03)
			// have no _ItemQuotation rows, so this is their foreign sale
			// fallback.
			unit, valid := commerce.SaleUnitPrice(ref, item.MagicOptions, rt.Commerce.Magic)
			if !valid {
				return 0, 0, 0, false
			}
			credit = int64(uint32(unit) * uint32(item.Quantity))
			break
		}
		if quote.Lower != quote.Upper {
			return 0, 0, 0, false
		}
		unit, err := commerce.TradeQuotationPrice(uint32(base), quote)
		if err != nil {
			return 0, 0, 0, false
		}
		credit = int64(unit * uint32(item.Quantity))
		profit = credit - int64(uint32(base)*uint32(item.Quantity))
		// Port-only, not native: Jangan route profit (tradetiers.go).
		credit, profit = RaiseRouteProfit(credit, profit, rt.routeProfitPercent(npc, item, s))
	}
	taxed, valid := commerce.AdjustPrice(uint64(max(credit, 0)), rt.commerceTax(division, npc, c), false)
	return taxed, max(profit, 0), int64(uint64(max(credit, 0)) - taxed), valid
}
