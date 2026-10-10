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
nativeStackIdentityMatches

The native reference comparison precedes the trade-owner string comparison.
Ordinary merchandise retains its reference-only merge policy here.
================
*/
func nativeStackIdentityMatches(a, b Item) bool {
	if a.RefObjID != b.RefObjID {
		return false
	}
	if IsTradeGoods(a.TypeFlags) || IsTradeGoods(b.TypeFlags) {
		return a.TradeOwner == b.TradeOwner
	}
	return true
}

/*
================
stackIdentityMatches

Port-only, not native: quantity transfers between value-carrying stones
must preserve assimilation. Whole-container native singles use the native
identity instead; other placement paths cannot pour into a full singleton.
================
*/
func stackIdentityMatches(a, b Item) bool {
	if !nativeStackIdentityMatches(a, b) {
		return false
	}
	if wire.EtcCarriesPlusByte(a.TypeFlags) || wire.EtcCarriesPlusByte(b.TypeFlags) {
		return a.Plus == b.Plus
	}
	return true
}

/*
================
wholeStackIdentityMatches

Native 756A99..756AD5 and 756BF4..756C14 exchange counts even at cap one;
7895F0 does not compare Plus. Retained stacks after rollback still need
the port-only value guard, or a count exchange changes assimilation stock.
================
*/
func wholeStackIdentityMatches(a, b Item, stackCap uint16) bool {
	if stackCap == 1 && a.Quantity == 1 && b.Quantity == 1 {
		return nativeStackIdentityMatches(a, b)
	}
	return stackIdentityMatches(a, b)
}
