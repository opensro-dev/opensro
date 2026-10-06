/*
===========================================================================

tradegoods.go - trade cargo identity across stack operations

CGItem_CheckMergeEligibility (490230) compares the recorded owner names of
trade goods before combining their quantities. A pickup or container move
must not turn another trader's goods into the destination stack's goods.

===========================================================================
*/
package inventory

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
================
*/
func stackIdentityMatches(a, b Item) bool {
	if a.RefObjID != b.RefObjID {
		return false
	}
	if IsTradeGoods(a.TypeFlags) || IsTradeGoods(b.TypeFlags) {
		return a.TradeOwner == b.TradeOwner
	}
	return true
}
