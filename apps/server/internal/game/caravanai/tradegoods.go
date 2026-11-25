/*
===========================================================================

tradegoods.go - the specialty goods an AI trader's cargo is made of

Port-only, not native (SRO_AI_CARAVANS; #3704 slice 5). A parley tribute
is the trader's own cargo: real specialty trade goods dropped on the
ground, never goods conjured for the occasion. This file names which
goods row each origin town's cargo is.

The town mapping is read off the shipped data, not invented: npcpos.txt
places NPC_CH_SPECIAL (RefObjID 2010) in region 25000 (Jangan) and
NPC_WC_SPECIAL (RefObjID 2059) in region 26265 (Donwhang), and the
specialty tabs STORE_CH_SPECIAL_TAB1 / STORE_WC_SPECIAL_TAB1 sell
ITEM_ETC_TRADE_CH_01.. / ITEM_ETC_TRADE_WC_01... NPC_AM_SPECIAL has no
npcpos row; with Jangan and Donwhang pinned, Hotan takes the AM family
by elimination - an inference, flagged for the owner.

===========================================================================
*/
package caravanai

/*
================
TradeGoodsCodename

The specialty trade-goods row an agent trading out of origin carries:
the goods its origin town's specialty shop sells. False for a town
with no specialty goods.
================
*/
func TradeGoodsCodename(origin TownID) (string, bool) {
	switch origin {
	case TownJangan:
		return "ITEM_ETC_TRADE_CH_01", true
	case TownDonwhang:
		return "ITEM_ETC_TRADE_WC_01", true
	case TownHotan:
		return "ITEM_ETC_TRADE_AM_01", true
	}
	return "", false
}

/*
================
TradeGoodsFor

The specialty trade-goods row the named agent's cargo is made of (its
origin town's goods), for the consumer's parley ground drop.
================
*/
func (r *Runtime) TradeGoodsFor(division, agentName string) (string, bool) {
	r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		return "", false
	}
	return TradeGoodsCodename(unit.OriginTown)
}
