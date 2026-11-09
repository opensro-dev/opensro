/*
===========================================================================

caravanstars_test.go - the port-only 5-star caravan roster body

===========================================================================
*/
package wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

/*
================
TestEncodeCaravanStarsLayout
================
*/
func TestEncodeCaravanStarsLayout(t *testing.T) {
	got := EncodeCaravanStars([]uint32{7, 0x2A})
	want := binary.LittleEndian.AppendUint16(nil, 2)
	want = binary.LittleEndian.AppendUint32(want, 7)
	want = binary.LittleEndian.AppendUint32(want, 0x2A)
	if !bytes.Equal(got, want) {
		t.Fatalf("body % X, want % X", got, want)
	}
	if got := EncodeCaravanStars(nil); !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("empty body % X, want a zero count", got)
	}
	if n := binary.LittleEndian.Uint16(EncodeCaravanStars(make([]uint32, CaravanStarsMax+5))); n != CaravanStarsMax {
		t.Fatalf("count %d, want the %d cap", n, CaravanStarsMax)
	}
}
