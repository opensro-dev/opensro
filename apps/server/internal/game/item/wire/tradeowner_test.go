/*
===========================================================================
tradeowner_test.go - CSOItem 78C966 quantity and original trader string
===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestTradeOwnerItemBodyRoundTripAndTruncation
================
*/
func TestTradeOwnerItemBodyRoundTripAndTruncation(t *testing.T) {
	body := ItemBody{RefObjID: 2151, TypeFlags: PackTypeFlags(3, 3, 8, 1), Quantity: 3, TradeOwner: "Trader"}
	expected := []byte{0x67, 8, 0, 0, 3, 0, 6, 0, 'T', 'r', 'a', 'd', 'e', 'r'}
	encoded := body.Encode()
	if !bytes.Equal(encoded, expected) || body.EncodedSize() != len(encoded) {
		t.Fatalf("body %x", encoded)
	}
	got, err := readItemBody(NewReader(encoded), body.TypeFlags)
	if err != nil || got.TradeOwner != body.TradeOwner || got.Quantity != 3 {
		t.Fatalf("round trip %+v %v", got, err)
	}
	for n := 0; n < len(encoded); n++ {
		if _, err := readItemBody(NewReader(encoded[:n]), body.TypeFlags); err == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
	body.TradeOwner = ""
	if body.EncodedSize() != 8 || len(body.Encode()) != 8 {
		t.Fatal("stolen unnamed goods still carry empty string")
	}
	body.TypeFlags = PackTypeFlags(3, 3, 1, 1)
	if body.EncodedSize() != 6 || len(body.Encode()) != 6 {
		t.Fatal("ordinary goods acquired owner field")
	}
}
