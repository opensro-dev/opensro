/*
===========================================================================

guildstorage_test.go - the guild warehouse move types on the wire

===========================================================================
*/

package wire

import (
	"bytes"
	"testing"
)

/*
================
TestGuildWarehouseMovesShareThePersonalLayouts

ItemMoveRequest_Serialize groups 0x1D with 1, 0x1E/0x1F with 2/3 and
0x20/0x21 with the gold amount.
================
*/
func TestGuildWarehouseMovesShareThePersonalLayouts(t *testing.T) {
	cases := []struct {
		request ItemMoveRequest
		body    []byte
	}{
		{ItemMoveRequest{MovementType: MoveTypeGuildStorage, SourceSlot: 1, DestSlot: 2, Quantity: 5, NpcGID: 7},
			[]byte{0x1d, 1, 2, 5, 0, 7, 0, 0, 0}},
		{ItemMoveRequest{MovementType: MoveTypeGuildStorageDeposit, SourceSlot: 13, DestSlot: 0, NpcGID: 7},
			[]byte{0x1e, 13, 0, 7, 0, 0, 0}},
		{ItemMoveRequest{MovementType: MoveTypeGuildStorageGoldWithdraw, GoldAmount: 300},
			[]byte{0x21, 0x2c, 1, 0, 0}},
	}
	for _, c := range cases {
		body, err := c.request.Encode()
		if err != nil || !bytes.Equal(body, c.body) {
			t.Fatalf("0x%02X encodes %x/%v, want %x", c.request.MovementType, body, err, c.body)
		}
		decoded, err := DecodeItemMoveRequest(body)
		if err != nil || decoded.MovementType != c.request.MovementType || decoded.SourceSlot != c.request.SourceSlot ||
			decoded.NpcGID != c.request.NpcGID || decoded.GoldAmount != c.request.GoldAmount {
			t.Fatalf("0x%02X decodes %+v/%v", c.request.MovementType, decoded, err)
		}
		personal, guild := PersonalStorageMove(c.request.MovementType)
		if !guild || personal == c.request.MovementType {
			t.Fatalf("0x%02X maps to 0x%02X", c.request.MovementType, personal)
		}
	}
}
