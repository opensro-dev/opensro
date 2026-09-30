/*
===========================================================================

betaplayermap_test.go - the port-only world map roster body

===========================================================================
*/
package wire

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

/*
================
TestEncodeBetaPlayerMapLayout
================
*/
func TestEncodeBetaPlayerMapLayout(t *testing.T) {
	got := EncodeBetaPlayerMap([]BetaMapPlayer{{Gid: 7, RegionID: 0x694F, X: 1040, Z: 160.5, Name: "Ann"}})
	want := binary.LittleEndian.AppendUint16(nil, 1)
	want = binary.LittleEndian.AppendUint32(want, 7)
	want = binary.LittleEndian.AppendUint16(want, 0x694F)
	want = binary.LittleEndian.AppendUint32(want, math.Float32bits(1040))
	want = binary.LittleEndian.AppendUint32(want, math.Float32bits(160.5))
	want = append(want, 3, 'A', 'n', 'n')
	if !bytes.Equal(got, want) {
		t.Fatalf("body % X, want % X", got, want)
	}
	if n := binary.LittleEndian.Uint16(EncodeBetaPlayerMap(make([]BetaMapPlayer, BetaPlayerMapMaxPlayers+5))); n != BetaPlayerMapMaxPlayers {
		t.Fatalf("count %d, want the %d cap", n, BetaPlayerMapMaxPlayers)
	}
}
