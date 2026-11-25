/*
===========================================================================

spiritbody_test.go - a Devil's Spirit's clock rides the body's durability field (#4381)

===========================================================================
*/
package wire

import (
	"encoding/binary"
	"testing"
)

/*
================
TestSpiritBodyCarriesItsClock

The client tells a dormant Spirit from an awake one by the durability
field, so a Spirit's body writes its data word there; any other avatar
keeps its durability.
================
*/
func TestSpiritBodyCarriesItsClock(t *testing.T) {
	const clock = 1_798_023_382
	// The durability field follows the reference, plus and variance.
	const durabilityAt = 4 + 1 + 8
	spirit := ItemBody{RefObjID: 51681, TypeFlags: PackTypeFlags(3, 1, 14, 1), Durability: 0, Data: clock}
	body := spirit.Encode()
	if got := binary.LittleEndian.Uint32(body[durabilityAt:]); got != clock {
		t.Fatalf("spirit durability field = %d, want its clock %d", got, clock)
	}
	back, err := readItemBody(NewReader(body), spirit.TypeFlags, nil)
	if err != nil || back.Data != clock {
		t.Fatalf("spirit round trip = %+v, %v", back, err)
	}
	avatar := ItemBody{RefObjID: 4242, TypeFlags: PackTypeFlags(3, 1, 13, 1), Durability: 90, Data: clock}
	if got := binary.LittleEndian.Uint32(avatar.Encode()[durabilityAt:]); got != 90 {
		t.Fatalf("avatar durability field = %d, want its durability 90", got)
	}
}
