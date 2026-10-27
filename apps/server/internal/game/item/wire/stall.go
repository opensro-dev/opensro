/*
===========================================================================

stall.go - the street stall and stall network frames

The v1.150 client's stall (CIFStall, interface child 0x21):

  - 0x7049 [wstr title] opens a stall (NetClient_RequestStallCreate0x7049);
    0xB049 [1] answers (74F7A0), the stall shows nearby as 0x30DF [u32 gid]
    [wstr title][u32 decoration] (751430).
  - 0x742C closes it (0xB42C [1], 74F840); 0x33D1 [u32 gid][u8] tells the
    neighbours (74F8F0).
  - 0x761F [u32 owner gid] visits (0xB61F [1][u32 owner][wstr greeting]
    [u8 open][u8 mode] offers [u8 visitors][u32 gid]..., 751720); 0x76E7
    leaves (0xB6E7 [1], 74FB90).
  - 0x73F9 [u8 slot] buys (0xB3F9 [1][u8 slot], 74FC40): the buyer's
    client puts the slot's item in its first empty bag slot (5A3870).
  - 0x71A8 [u8 kind] edits (6FF420): 1 [slot][u16 count][u32 price][u8],
    2 [slot][bag slot][u16 count][u32 price][u32 category][u8], 3 [slot]
    [u8], 4 [u8 mode], 5 [u8 open][u8 network], 6 [wstr greeting], 7
    [wstr title]. 0xB1A8 [1][kind] (751A60) goes to everyone at the stall:
    1 [slot][u16][u32][u8 code], 2 and 3 [u8 code] offers, 4 [u8], 5 [u8
    open][u8 network code], 6 [wstr]; 0x34B7 [u32 gid][wstr] renames it
    for the neighbours (751520).
  - 0x3260 (755960) tells the stall: 1 [u32 gid] left, 2 [u32 gid] came,
    3 and 4 [u8 slot][str buyer] offers sold (4 through the network).

Offers are [u8 slot][CSOItem][u8 bag slot][u16 count][u32 price] until a
0xFF slot (74F9D0). Refusals are [2][code], a category 0x0A notice.

The stall network (CIFStallNetwork): 0x76F9 [u8 kind][u8 page][u32
category][u8] searches (0xB6F9 [1][u8 rows][u8 pages] then rows of
[CSOItem][u32 owner][u8 slot][u16 count][u8][u64 price][u64 serial],
766FC0), 0x72CA [u32 owner][u8 slot][u64 price][u16 count][u8][u64
serial] buys from a listed stall (0xB2CA [1], 7671D0).

===========================================================================
*/
package wire

const (
	OpStallCreate        uint16 = 0x7049
	OpStallCreateResult  uint16 = 0xB049
	OpStallClose         uint16 = 0x742C
	OpStallCloseResult   uint16 = 0xB42C
	OpStallVisit         uint16 = 0x761F
	OpStallVisitResult   uint16 = 0xB61F
	OpStallLeave         uint16 = 0x76E7
	OpStallLeaveResult   uint16 = 0xB6E7
	OpStallBuy           uint16 = 0x73F9
	OpStallBuyResult     uint16 = 0xB3F9
	OpStallEdit          uint16 = 0x71A8
	OpStallEditResult    uint16 = 0xB1A8
	OpStallOpened        uint16 = 0x30DF
	OpStallClosed        uint16 = 0x33D1
	OpStallRenamed       uint16 = 0x34B7
	OpStallEvent         uint16 = 0x3260
	OpStallNetworkSearch uint16 = 0x76F9
	OpStallNetworkResult uint16 = 0xB6F9
	OpStallNetworkBuy    uint16 = 0x72CA
	OpStallNetworkBought uint16 = 0xB2CA

	StallEditModify   uint8 = 1
	StallEditAdd      uint8 = 2
	StallEditRemove   uint8 = 3
	StallEditMode     uint8 = 4
	StallEditOpen     uint8 = 5
	StallEditGreeting uint8 = 6
	StallEditTitle    uint8 = 7

	StallEventLeft        uint8 = 1
	StallEventCame        uint8 = 2
	StallEventSold        uint8 = 3
	StallEventNetworkSold uint8 = 4

	stallOffersEnd uint8 = 0xFF
	// StallNetworkRegistered is 751A60's network code that prints
	// UIIT_MSG_WARENETWORK_REGIST_SUCCESS.
	StallNetworkRegistered uint8 = 1
)

// Category 0x0A codes: the low byte of the GameServer's 0x3Cxx stall
// errors, as the v1.150 notice table words them.
const (
	StallErrInvalidTarget uint8 = 0x03
	StallErrTooFar        uint8 = 0x04
	StallErrInvalidOp     uint8 = 0x05
	StallErrBadBagSlot    uint8 = 0x06
	StallErrBadCount      uint8 = 0x07
	StallErrBadPrice      uint8 = 0x08
	StallErrNameLength    uint8 = 0x09
	StallErrGreeting      uint8 = 0x0A
	StallErrNothingToSell uint8 = 0x0C
	StallErrMarketClosed  uint8 = 0x0E
	StallErrNotVisiting   uint8 = 0x0F
	StallErrBadSlot       uint8 = 0x10
	StallErrNotEnoughGold uint8 = 0x11
	StallErrInventoryFull uint8 = 0x12
	StallErrHasStall      uint8 = 0x14
	StallErrHostLeft      uint8 = 0x15
	StallErrBusy          uint8 = 0x16
	StallErrClosedByHost  uint8 = 0x17
	StallErrHostState     uint8 = 0x2B
	StallErrStallFull     uint8 = 0x30
	StallErrRideState     uint8 = 0x34
	StallErrMurderer      uint8 = 0x39
	StallErrJob           uint8 = 0x3B
	StallErrNoCategory    uint8 = 0x42
	StallErrNetworkTown   uint8 = 0x49
	StallErrRegisterTown  uint8 = 0x4A
	StallErrNetworkStale  uint8 = 0x4F
	StallErrNetworkGold   uint8 = 0x52
	StallErrNetworkBag    uint8 = 0x53
)

/*
================
StallOffer

One offer as the wire lists it.
================
*/
type StallOffer struct {
	Slot     uint8
	Item     ItemBody
	BagSlot  uint8
	Quantity uint16
	Price    uint32
}

/*
================
WriteStallOffers

Offers until the 0xFF end marker (74F9D0).
================
*/
func WriteStallOffers(w *Writer, offers []StallOffer) *Writer {
	for _, offer := range offers {
		w.U8(offer.Slot).Bytes(offer.Item.Encode()).U8(offer.BagSlot).U16(offer.Quantity).U32(offer.Price)
	}
	return w.U8(stallOffersEnd)
}

/*
================
EncodeStallVisit
================
*/
func EncodeStallVisit(owner uint32, greeting string, open bool, mode uint8, offers []StallOffer, visitors []uint32) []byte {
	w := NewWriter(64).U8(ResultSuccess).U32(owner).WStr(greeting).U8(boolByte(open)).U8(mode)
	WriteStallOffers(w, offers)
	w.U8(uint8(len(visitors)))
	for _, gid := range visitors {
		w.U32(gid)
	}
	return w.Payload()
}

/*
================
EncodeStallEdit

0xB1A8 [1][kind] and the kind's body.
================
*/
func EncodeStallEdit(kind uint8, body []byte) []byte {
	return append([]byte{ResultSuccess, kind}, body...)
}

/*
================
EncodeStallSold

0x3260 kinds 3 and 4: the slot, the buyer's name and the offers left.
================
*/
func EncodeStallSold(kind, slot uint8, buyer string, offers []StallOffer) []byte {
	w := NewWriter(64).U8(kind).U8(slot).Str(buyer)
	return WriteStallOffers(w, offers).Payload()
}

/*
================
EncodeStallVisitor

0x3260 kinds 1 and 2.
================
*/
func EncodeStallVisitor(kind uint8, gid uint32) []byte {
	return NewWriter(5).U8(kind).U32(gid).Payload()
}

/*
================
EncodeStallOpened
================
*/
func EncodeStallOpened(gid uint32, title string, decoration uint32) []byte {
	return NewWriter(16).U32(gid).WStr(title).U32(decoration).Payload()
}

/*
================
EncodeStallRenamed
================
*/
func EncodeStallRenamed(gid uint32, title string) []byte {
	return NewWriter(16).U32(gid).WStr(title).Payload()
}

/*
================
EncodeStallClosed
================
*/
func EncodeStallClosed(gid uint32) []byte {
	return NewWriter(5).U32(gid).U8(0).Payload()
}

/*
================
EncodeStallResult

[1] (and the body) or [2][code].
================
*/
func EncodeStallResult(code uint8, body ...byte) []byte {
	if code != 0 {
		return []byte{ResultError, code}
	}
	return append([]byte{ResultSuccess}, body...)
}

/*
================
StallListing

One stall network row.
================
*/
type StallListing struct {
	Item     ItemBody
	Owner    uint32
	Slot     uint8
	Quantity uint16
	Price    uint64
	Serial   uint64
}

/*
================
EncodeStallNetworkResult
================
*/
func EncodeStallNetworkResult(rows []StallListing, pages uint8) []byte {
	w := NewWriter(16 + 40*len(rows)).U8(ResultSuccess).U8(uint8(len(rows))).U8(pages)
	for _, row := range rows {
		w.Bytes(row.Item.Encode()).U32(row.Owner).U8(row.Slot).U16(row.Quantity).U8(0).U64(row.Price).U64(row.Serial)
	}
	return w.Payload()
}

/*
================
boolByte
================
*/
func boolByte(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}
