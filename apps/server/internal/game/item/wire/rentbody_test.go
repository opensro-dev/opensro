/*
===========================================================================

rentbody_test.go - a rented row's deadline rides the equipment body (#4240)

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestRentDeadlineBodyRoundTrip

A rented avatar row's body ends with its deadline (8 more bytes than a
permanent row's); only a rental reference reads the leg.
================
*/
func TestRentDeadlineBodyRoundTrip(t *testing.T) {
	flags := PackTypeFlags(3, 1, 13, 1)
	rented := ItemBody{RefObjID: 4242, TypeFlags: flags, Plus: 3, VarianceBits: 7, Durability: 90,
		RentExpiresUnixMs: 1_800_000_000_000}
	permanent := rented
	permanent.RentExpiresUnixMs = 0
	if rented.EncodedSize()-permanent.EncodedSize() != 8 || len(rented.Encode())-len(permanent.Encode()) != 8 {
		t.Fatalf("rent leg = %d bytes, want 8", len(rented.Encode())-len(permanent.Encode()))
	}
	isRent := func(id uint32) bool { return id == 4242 }
	back, err := readItemBody(NewReader(rented.Encode()), flags, isRent)
	if err != nil || back.RentExpiresUnixMs != rented.RentExpiresUnixMs || back.Plus != 3 || back.Durability != 90 {
		t.Fatalf("rented round trip = %+v, %v", back, err)
	}
	// A permanent row of a non-rental reference carries no leg (the
	// encoder wrote none) and decodes the same with the predicate
	// installed. A rent-reference body WITHOUT the leg is malformed:
	// every row of a rental kind carries its deadline, exactly as
	// native keeps no rent row without its _RentItemInfo.
	permanent.RefObjID = 4243
	if bytes.Equal(rented.Encode()[4:len(permanent.Encode())], permanent.Encode()[4:]) != true {
		t.Fatal("the leg changed the permanent bytes past the reference")
	}
	short, err := readItemBody(NewReader(permanent.Encode()), flags, isRent)
	if err != nil || short.RentExpiresUnixMs != 0 {
		t.Fatalf("permanent round trip = %+v, %v", short, err)
	}
	legless := rented
	legless.RentExpiresUnixMs = 0
	if _, err := readItemBody(NewReader(legless.Encode()), flags, isRent); err == nil {
		t.Fatal("a rent-reference body without its deadline leg was accepted")
	}
}
