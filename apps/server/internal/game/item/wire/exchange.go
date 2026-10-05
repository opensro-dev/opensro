/*
===========================================================================

exchange.go - the player-to-player exchange frames

The v1.150 client's exchange (CIFExchange, interface child 0x1A):

  - 0x7237 [u32 target gid] asks (NetClient_SendTradeRequest7237); the
    target hears the 0x3393 kind-1 prompt and answers {1, 1} or {1, 2}.
  - The requester's window opens on 0xB237 [1][u32 partner gid] (75B370),
    the target's on 0x3219 [u32 partner gid] (75B420); a refusal is
    0xB237 [2][code].
  - Items and gold move with 0x706D types 4 (bag -> exchange, [u8 bag
    slot]), 5 (exchange -> bag, [u8 exchange slot]) and 0x0D (gold,
    [u32]); 0xB06D answers [4][bag][exchange slot], [5][slot] and
    [0x0D][u32] (759A30, 74EF50).
  - 0x3569 [u32 owner gid][u8 count] then per offer ([u8 bag slot] when
    the owner is the reader)[u8 exchange slot][CSOItem] lists a side
    (75B690); 0x30BB [2][u32] is the partner's gold (75B580).
  - 0x7095 confirms (0xB095 [1], the partner hears 0x37CF), 0x734A
    approves (0xB34A [1]), 0x72DB cancels (0xB2DB [1]); every refusal is
    [2][code], a category 1 notice.
  - 0x3272 reports the swap: the client puts the partner's items in its
    first empty bag slots and takes its own out (765260). 0x3457 [u8 code]
    ends a session that failed or was cancelled (75B820).

===========================================================================
*/
package wire

const (
	OpExchangeRequest       uint16 = 0x7237
	OpExchangeRequestResult uint16 = 0xB237
	OpExchangeOpened        uint16 = 0x3219
	OpExchangeConfirm       uint16 = 0x7095
	OpExchangeConfirmResult uint16 = 0xB095
	OpExchangePartnerLocked uint16 = 0x37CF
	OpExchangeApprove       uint16 = 0x734A
	OpExchangeApproveResult uint16 = 0xB34A
	OpExchangeCancel        uint16 = 0x72DB
	OpExchangeCancelResult  uint16 = 0xB2DB
	OpExchangeOffer         uint16 = 0x3569
	OpExchangePartnerGold   uint16 = 0x30BB
	OpExchangeSucceeded     uint16 = 0x3272
	OpExchangeFailed        uint16 = 0x3457

	MoveTypeExchangePut  uint8 = 0x04
	MoveTypeExchangeTake uint8 = 0x05
	MoveTypeExchangeGold uint8 = 0x0D

	exchangePartnerGoldKind uint8 = 2
)

// Category 1 notice codes the exchange answers with.
const (
	ExchangeErrInvalidTarget   uint8 = 0x03
	ExchangeErrTooFar          uint8 = 0x04
	ExchangeErrBusy            uint8 = 0x1E
	ExchangeErrBayFull         uint8 = 0x21
	ExchangeErrInventoryFull   uint8 = 0x22
	ExchangeErrDenied          uint8 = 0x28
	ExchangeErrCancelled       uint8 = 0x2B
	ExchangeErrCancelledByPeer uint8 = 0x2C
	ExchangeErrPartnerSpace    uint8 = 0x36
	ExchangeErrCannotTrade     uint8 = 0x3A
)

/*
================
ExchangeOfferRow

One row of a 0x3569 list.
================
*/
type ExchangeOfferRow struct {
	BagSlot      uint8
	ExchangeSlot uint8
	Item         ItemBody
}

/*
================
EncodeExchangeOffer3569

The owner's list as reader sees it: the bag slot rides only to the owner.
================
*/
func EncodeExchangeOffer3569(ownerGid uint32, rows []ExchangeOfferRow, toOwner bool) []byte {
	w := NewWriter(6 + 48*len(rows)).U32(ownerGid).U8(uint8(len(rows)))
	for _, row := range rows {
		if toOwner {
			w.U8(row.BagSlot)
		}
		w.U8(row.ExchangeSlot).Bytes(row.Item.Encode())
	}
	return w.Payload()
}

/*
================
EncodeExchangePartnerGold30BB
================
*/
func EncodeExchangePartnerGold30BB(gold uint32) []byte {
	return NewWriter(5).U8(exchangePartnerGoldKind).U32(gold).Payload()
}

/*
================
EncodeExchangeResult

[1] (then the body, if any) or [2][code].
================
*/
func EncodeExchangeResult(code uint8, body ...byte) []byte {
	if code != 0 {
		return []byte{ResultError, code}
	}
	return append([]byte{ResultSuccess}, body...)
}

/*
================
EncodeExchangeGid
================
*/
func EncodeExchangeGid(gid uint32) []byte {
	return NewWriter(4).U32(gid).Payload()
}

/*
================
EncodeExchangePutResult
================
*/
func EncodeExchangePutResult(bagSlot, exchangeSlot uint8) []byte {
	return []byte{ResultSuccess, MoveTypeExchangePut, bagSlot, exchangeSlot}
}

/*
================
EncodeExchangeTakeResult
================
*/
func EncodeExchangeTakeResult(exchangeSlot uint8) []byte {
	return []byte{ResultSuccess, MoveTypeExchangeTake, exchangeSlot}
}

/*
================
EncodeExchangeGoldResult
================
*/
func EncodeExchangeGoldResult(gold uint32) []byte {
	return NewWriter(6).U8(ResultSuccess).U8(MoveTypeExchangeGold).U32(gold).Payload()
}
