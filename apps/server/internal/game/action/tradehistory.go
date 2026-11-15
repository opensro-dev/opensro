/*
===========================================================================

tradehistory.go - the trade records a committed exchange or stall sale notes

Port-only, not native: the record of each committed trade (domain
trade_history.go). Both parties get a row, built here from the swap the
commit door has just decided: exchange.go's commitExchange and stall.go's
sellStallSlot, which covers the direct stall and the stall network alike.

The rows are noted from inside the trade's own commit door, after the gold
and the items have moved and before the door reports success, so the store
writes them in the same transaction. A refused trade notes nothing: each note
sits on the committed branch only. The action package never imports the
store; the composition wires enterworld Deps.TradeNote to it.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/exchange"
	"opensro.online/server/internal/game/item/stall"
)

/*
================
noteTrades

Notes a committed trade's records with the deps that keep them (enterworld
Deps.NoteTrade). A composition that keeps no trade history (a fixture) has
nothing to note with. Called from inside the trade's commit door.
================
*/
func (rt *Runtime) noteTrades(records []domain.TradeRecord) {
	if recorder, ok := rt.deps.(interface {
		NoteTrade(records []domain.TradeRecord)
	}); ok {
		recorder.NoteTrade(records)
	}
}

/*
================
tradeRecordOf

The skeleton of one party's record: the party, the other party and the kind.
The caller sets the gold and the items the party paid, received and moved.
================
*/
func tradeRecordOf(division string, self, other *domain.Character, kind string) domain.TradeRecord {
	return domain.TradeRecord{
		Division:       division,
		AccountID:      self.AccountID,
		CharID:         self.ID,
		CharName:       self.Name,
		OtherAccountID: other.AccountID,
		OtherCharID:    other.ID,
		OtherName:      other.Name,
		Kind:           kind,
	}
}

/*
================
exchangeTradeItems

The stacks one exchange side put on the table, as the record names them: the
reference and the count, never the body.
================
*/
func exchangeTradeItems(side exchange.Side) []domain.TradeItem {
	items := []domain.TradeItem{}
	for _, offer := range side.Offers {
		if offer != nil {
			items = append(items, domain.TradeItem{RefObjID: offer.RefObjID, Quantity: offer.Quantity})
		}
	}
	return items
}

/*
================
exchangeTradeRecords

The two records of a committed exchange. The requester paid its side's gold
and gave its side's stacks, and received the partner's; the partner mirrors
that. An exchange charges no commission.
================
*/
func exchangeTradeRecords(division string, requester, partner *domain.Character, own, theirs exchange.Side) []domain.TradeRecord {
	first := tradeRecordOf(division, requester, partner, domain.TradeKindExchange)
	first.GoldPaid, first.GoldReceived = own.Gold, theirs.Gold
	first.ItemsGiven, first.ItemsReceived = exchangeTradeItems(own), exchangeTradeItems(theirs)
	second := tradeRecordOf(division, partner, requester, domain.TradeKindExchange)
	second.GoldPaid, second.GoldReceived = theirs.Gold, own.Gold
	second.ItemsGiven, second.ItemsReceived = exchangeTradeItems(theirs), exchangeTradeItems(own)
	return []domain.TradeRecord{first, second}
}

/*
================
stallTradeRecords

The two records of one stall sale. The buyer paid the price and received the
stack. The owner gave the stack and received the price less the network's
commission, which a direct sale never charges (commission is zero there). A
network sale names the network kinds, so the history tells them apart.
================
*/
func stallTradeRecords(division string, owner, buyer *domain.Character, offer stall.Slot, commission uint64, network bool) []domain.TradeRecord {
	buyKind, sellKind := domain.TradeKindStallBuy, domain.TradeKindStallSell
	if network {
		buyKind, sellKind = domain.TradeKindStallNetworkBuy, domain.TradeKindStallNetworkSell
	}
	bought := tradeRecordOf(division, buyer, owner, buyKind)
	bought.GoldPaid = offer.Price
	bought.ItemsReceived = []domain.TradeItem{{RefObjID: offer.RefObjID, Quantity: offer.Quantity}}
	sold := tradeRecordOf(division, owner, buyer, sellKind)
	sold.GoldReceived = offer.Price - commission
	sold.Commission = commission
	sold.ItemsGiven = []domain.TradeItem{{RefObjID: offer.RefObjID, Quantity: offer.Quantity}}
	return []domain.TradeRecord{bought, sold}
}
