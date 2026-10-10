/*
===========================================================================

tradegoods.go - stack identity: trade cargo owners and stone assimilation

CGItem_CheckMergeEligibility (490230) compares the recorded owner names of
trade goods before combining their quantities. A pickup or container move
must not turn another trader's goods into the destination stack's goods.

Magic and attribute stones carry their assimilation value in Plus. Natively
they stack 1, so the original never compares it; SRO_STACK_SIZES can let
them stack (port-only, #583), and a merge must then keep that value.

===========================================================================
*/
package inventory

import "opensro.online/server/internal/game/item/wire"

const (
	tradeGoodsMask uint16 = 0x7fe
	tradeGoodsType uint16 = 0x46c
)

/*
================
IsTradeGoods
================
*/
func IsTradeGoods(flags uint16) bool {
	return flags&tradeGoodsMask == tradeGoodsType
}

/*
================
stackIdentityMatches

The native reference comparison precedes the trade-owner string comparison.
Ordinary merchandise retains its reference-only merge policy here.

Port-only, not native: stones whose rows carry a Plus byte (the
assimilation value, wire.EtcCarriesPlusByte) merge only with an equal
value, or a 40% stone poured into a 90% stack would become 90%. At the
native cap of 1 no merge reaches this test, so native play is unchanged.
================
*/
func stackIdentityMatches(a, b Item) bool {
	if a.RefObjID != b.RefObjID {
		return false
	}
	if IsTradeGoods(a.TypeFlags) || IsTradeGoods(b.TypeFlags) {
		return a.TradeOwner == b.TradeOwner
	}
	if wire.EtcCarriesPlusByte(a.TypeFlags) || wire.EtcCarriesPlusByte(b.TypeFlags) {
		return a.Plus == b.Plus
	}
	return true
}
