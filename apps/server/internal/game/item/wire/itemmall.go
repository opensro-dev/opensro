/*
===========================================================================

itemmall.go - the v1.150 Item Mall purchase request

The request identifies authored merchandise and the player's point contribution.
It never authorizes a price or a grant; the commerce authority checks both.

===========================================================================
*/

package wire

import (
	"encoding/binary"
	"fmt"
)

// 6BB1E0 emits B06D [2,80] when the native mall cannot fund a purchase.
const ErrCodeMallInsufficientCurrency uint8 = 0x80

const (
	MoveTypeMallBuy  uint8 = 0x18
	MallPurchaseSize int   = 16
)

/*
================
MallPurchase

Client 69825E writes group, shop, tab, slot, quantity, points and package.
7058E0 resolves the group/shop identity from the authored reference mapping.
================
*/
type MallPurchase struct {
	Group    uint16
	Shop     uint8
	Tab      uint8
	Slot     uint8
	Quantity uint16
	Points   uint32
	Package  uint32
}

/*
================
Encode
================
*/
func (q MallPurchase) Encode() ([]byte, error) {
	if q.Group == 0 || q.Quantity == 0 || q.Package == 0 {
		return nil, fmt.Errorf("wire: invalid mall purchase identity or quantity")
	}
	return NewWriter(MallPurchaseSize).U8(MoveTypeMallBuy).
		U16(q.Group).U8(q.Shop).U8(q.Tab).U8(q.Slot).
		U16(q.Quantity).U32(q.Points).U32(q.Package).Payload(), nil
}

/*
================
DecodeMallPurchase

Reject truncated and extended packets before looking up merchandise.
================
*/
func DecodeMallPurchase(payload []byte) (MallPurchase, error) {
	if len(payload) != MallPurchaseSize || payload[0] != MoveTypeMallBuy {
		return MallPurchase{}, fmt.Errorf("wire: invalid mall purchase payload")
	}
	q := MallPurchase{
		Group:    binary.LittleEndian.Uint16(payload[1:3]),
		Shop:     payload[3],
		Tab:      payload[4],
		Slot:     payload[5],
		Quantity: binary.LittleEndian.Uint16(payload[6:8]),
		Points:   binary.LittleEndian.Uint32(payload[8:12]),
		Package:  binary.LittleEndian.Uint32(payload[12:16]),
	}
	if q.Group == 0 || q.Quantity == 0 || q.Package == 0 {
		return MallPurchase{}, fmt.Errorf("wire: invalid mall purchase identity or quantity")
	}
	return q, nil
}

/*
================
EncodeMallPurchaseResult

6981AE..698250 writes the native address, destination vector and quantity.
The quantity tail is present on mall purchases as well as ordinary shop buys.
================
*/
func EncodeMallPurchaseResult(q MallPurchase, slots []uint8) ([]byte, error) {
	if _, err := q.Encode(); err != nil {
		return nil, err
	}
	if len(slots) == 0 || len(slots) > 255 {
		return nil, fmt.Errorf("wire: invalid mall delivery count")
	}
	w := NewWriter(10 + len(slots)).U8(1).U8(MoveTypeMallBuy).
		U16(q.Group).U8(q.Shop).U8(q.Tab).U8(q.Slot).U8(uint8(len(slots)))
	for _, slot := range slots {
		w.U8(slot)
	}
	return w.U16(q.Quantity).Payload(), nil
}
