/*
===========================================================================

itemmall_test.go - native purchase field order and malformed packet rejection

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestMallPurchaseNativeLayout

Distinct field bytes detect swapped shop/tab identities and point/package
fields. The oracle follows the write widths at client 69825E.
================
*/
func TestMallPurchaseNativeLayout(t *testing.T) {
	want := []byte{0x18, 0x54, 0x03, 2, 3, 4, 5, 0, 7, 0, 0, 0, 0x44, 0x33, 0x22, 0x11}
	request := MallPurchase{Group: 852, Shop: 2, Tab: 3, Slot: 4, Quantity: 5, Points: 7, Package: 0x11223344}
	encoded, err := request.Encode()
	if err != nil || !bytes.Equal(encoded, want) {
		t.Fatalf("encoded %x, error %v; want %x", encoded, err, want)
	}
	decoded, err := DecodeMallPurchase(want)
	if err != nil || decoded != request {
		t.Fatalf("decoded %+v, error %v; want %+v", decoded, err, request)
	}
	for size := 0; size < len(want); size++ {
		if _, err := DecodeMallPurchase(want[:size]); err == nil {
			t.Fatalf("accepted truncated purchase with %d bytes", size)
		}
	}
	if _, err := DecodeMallPurchase(append(encoded, 0)); err == nil {
		t.Fatal("accepted trailing bytes")
	}
	for _, offset := range []int{0, 1, 6, 12} {
		invalid := append([]byte(nil), want...)
		switch offset {
		case 1, 6:
			clear(invalid[offset : offset+2])
		case 12:
			clear(invalid[offset : offset+4])
		default:
			invalid[offset] = 0
		}
		if _, err := DecodeMallPurchase(invalid); err == nil {
			t.Fatalf("accepted invalid field at %d", offset)
		}
	}
}

/*
================
TestMallPurchaseResultNativeLayout
================
*/
func TestMallPurchaseResultNativeLayout(t *testing.T) {
	q := MallPurchase{Group: 852, Shop: 2, Tab: 3, Slot: 4, Quantity: 5, Package: 1}
	got, err := EncodeMallPurchaseResult(q, []uint8{13, 14})
	want := []byte{1, 0x18, 0x54, 3, 2, 3, 4, 2, 13, 14, 5, 0}
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("mall reply: %x %v, want %x", got, err, want)
	}
	for _, slots := range [][]uint8{nil, make([]uint8, 256)} {
		if _, err := EncodeMallPurchaseResult(q, slots); err == nil {
			t.Fatal("accepted invalid native destination count")
		}
	}
}
