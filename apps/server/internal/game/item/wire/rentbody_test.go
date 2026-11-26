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
	SetRentRefs([]uint32{4242})
	defer SetRentRefs(nil)
	flags := PackTypeFlags(3, 1, 13, 1)
	rented := ItemBody{RefObjID: 4242, TypeFlags: flags, Plus: 3, VarianceBits: 7, Durability: 90,
		RentExpiresUnixMs: 1_800_000_000_000}
	permanent := rented
	permanent.RefObjID = 4243
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
	if bytes.Equal(rented.Encode()[4:len(permanent.Encode())], permanent.Encode()[4:]) != true {
		t.Fatal("the leg changed the permanent bytes past the reference")
	}
	short, err := readItemBody(NewReader(permanent.Encode()), flags, isRent)
	if err != nil || short.RentExpiresUnixMs != 0 {
		t.Fatalf("permanent round trip = %+v, %v", short, err)
	}
	legless := permanent
	legless.RefObjID = 4242
	if _, err := readItemBody(NewReader(legless.Encode()[:len(legless.Encode())-8]), flags, isRent); err == nil {
		t.Fatal("a rent-reference body without its deadline leg was accepted")
	}
}

/*
================
TestDeadlineOnAnotherReferenceWritesNoLeg

The r204l live failure: the rent table named the later client's item
ids, so a rented Winter attachment (29411 here, 28268 there) carried a
deadline its reference was never marked for. The leg went out anyway,
and the client, measuring by the reference, read the body 8 bytes short:
"Invalid shop item body" on the mall delivery, then "Mall receipt lacks
delivery", then "Invalid inventory item body" on its blessing. A row is
measured by its reference alone, so that body is the permanent one.
================
*/
func TestDeadlineOnAnotherReferenceWritesNoLeg(t *testing.T) {
	SetRentRefs([]uint32{28268})
	defer SetRentRefs(nil)
	flags := PackTypeFlags(3, 1, 13, 1)
	rented := ItemBody{RefObjID: 29411, TypeFlags: flags, Plus: 1, Durability: 50,
		MagicOptions: []uint64{0x0000000500000f8}, RentExpiresUnixMs: 1_798_000_000_000}
	permanent := rented
	permanent.RentExpiresUnixMs = 0
	if !bytes.Equal(rented.Encode(), permanent.Encode()) || rented.EncodedSize() != len(permanent.Encode()) {
		t.Fatalf("deadline on a non-rental reference = %x, want the permanent body %x", rented.Encode(), permanent.Encode())
	}
	back, err := readItemBody(NewReader(rented.Encode()), flags, IsRentRef)
	if err != nil || back.RefObjID != 29411 || len(back.MagicOptions) != 1 || back.RentExpiresUnixMs != 0 {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}

/*
================
TestPermanentRowOfRentalRefCarriesZeroLeg

The r204l outage: a permanent row (no deadline) of a reference the
catalog names a rental kind must still write the leg, as 0, because the
client measures the body by the reference. Without it the next row's
bytes were read as the deadline and entry failed with S2.
================
*/
func TestPermanentRowOfRentalRefCarriesZeroLeg(t *testing.T) {
	SetRentRefs([]uint32{4242})
	defer SetRentRefs(nil)
	flags := PackTypeFlags(3, 1, 13, 1)
	permanent := ItemBody{RefObjID: 4242, TypeFlags: flags, Plus: 1, Durability: 50}
	other := permanent
	other.RefObjID = 4243
	encoded := permanent.Encode()
	if len(encoded) != permanent.EncodedSize() || len(encoded)-len(other.Encode()) != 8 {
		t.Fatalf("permanent rental-kind body = %d bytes, other = %d; want 8 more", len(encoded), len(other.Encode()))
	}
	if !bytes.Equal(encoded[len(encoded)-8:], make([]byte, 8)) {
		t.Fatalf("permanent leg = %x, want zero", encoded[len(encoded)-8:])
	}
	back, err := readItemBody(NewReader(encoded), flags, IsRentRef)
	if err != nil || back.RentExpiresUnixMs != 0 || back.Durability != 50 {
		t.Fatalf("permanent rental-kind round trip = %+v, %v", back, err)
	}
}
