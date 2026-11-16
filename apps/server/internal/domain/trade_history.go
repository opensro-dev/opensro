/*
===========================================================================

trade_history.go - one party's record of one committed trade (port-only)

The vocabulary of the store's trade_history table (layout 13). Port-only,
not native: the original keeps no record of a player-to-player exchange or
a stall sale. Each committed trade writes two rows, one per party, and each
row names the other party by account, character and name as it stood at the
trade. The read side applies the counterparty's current privacy choice, as
the unique-kill record does, so a hidden player stops reading by name.

INFERENCE: the v1.150 client shows the exchange and stall windows only, and
the v1.188 texts name no journal, so the record is a port-side bookkeeping
row with no gameplay effect. It ships always on, like the economy ledger.

===========================================================================
*/
package domain

// The kinds a row records. An exchange is the player-to-player swap. A
// stall sale is a direct purchase or sale at a stall; the network kinds are
// the stall network's, which names a commission the seller pays.
const (
	TradeKindExchange         = "exchange"
	TradeKindStallBuy         = "stall_buy"
	TradeKindStallSell        = "stall_sell"
	TradeKindStallNetworkBuy  = "stall_network_buy"
	TradeKindStallNetworkSell = "stall_network_sell"
)

/*
================
TradeItem

One item stack a trade moved: its reference and how many. The item's body
never reaches the record; the reference is enough to name what changed hands.
================
*/
type TradeItem struct {
	RefObjID uint32 `json:"refObjId"`
	Quantity uint16 `json:"quantity"`
}

/*
================
TradeRecord

One party's row of one committed trade: the party and the other party, what
the party paid and received, the commission it paid (a network sale's seller
only), and the stacks it gave and received. AtMs is stamped when the trade is
noted, inside its commit door.
================
*/
type TradeRecord struct {
	AtMs           int64
	Division       string
	AccountID      string
	CharID         int64
	CharName       string
	OtherAccountID string
	OtherCharID    int64
	OtherName      string
	Kind           string
	GoldPaid       uint64
	GoldReceived   uint64
	Commission     uint64
	ItemsGiven     []TradeItem
	ItemsReceived  []TradeItem
}

/*
================
TradeEntry

A stored row as the read side receives it: its sequence number, the row, and
whether the counterparty's character is hidden now (the read side masks the
name for a hidden one).
================
*/
type TradeEntry struct {
	Seq         int64
	Record      TradeRecord
	OtherHidden bool
}

/*
================
TradeCursor

The boundary of a page: the rows strictly older than (AtMs, Seq) in newest-
first order. A page's next cursor is its last row's pair.
================
*/
type TradeCursor struct {
	AtMs int64
	Seq  int64
}

/*
================
TradePage

One page of entries, newest first. More says rows older than the last entry
remain, so a caller asks again with that entry's cursor.
================
*/
type TradePage struct {
	Entries []TradeEntry
	More    bool
}
