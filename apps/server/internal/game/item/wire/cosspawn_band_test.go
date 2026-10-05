/*
===========================================================================

cosspawn_band_test.go - the band tails of the COS spawn row

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestCosSpawnCapturedMonsterOmitsHoldAndPvp

854FA0: after the owner name a captured quest monster (band 6) goes
straight to the owner gid; a transport (band 2) carries the hold and PvP
bytes between them.
================
*/
func TestCosSpawnCapturedMonsterOmitsHoldAndPvp(t *testing.T) {
	row := CosSpawnBand2{Band: CosBandCapturedMonster, RefObjID: 14745, Gid: 0x02800005, OwnerName: "Bob", OwnerGid: 0x186a5, PvpState: 7, State: 1}
	captured := EncodeCosSpawnBand2(row)
	tail := []byte{3, 0, 'B', 'o', 'b', 0xa5, 0x86, 1, 0, 1}
	if !bytes.HasSuffix(captured, tail) {
		t.Fatalf("band 6 tail % X", captured[len(captured)-len(tail):])
	}
	row.Band = 2
	transport := EncodeCosSpawnBand2(row)
	if len(transport) != len(captured)+2 || !bytes.HasSuffix(transport, []byte{'B', 'o', 'b', 0, 7, 0xa5, 0x86, 1, 0, 1}) {
		t.Fatalf("band 2 tail % X", transport[len(transport)-10:])
	}
}
