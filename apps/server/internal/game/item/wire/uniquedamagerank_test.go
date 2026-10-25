/*
===========================================================================

uniquedamagerank_test.go - the port-only unique damage ranking body

===========================================================================
*/
package wire

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

/*
================
TestEncodeUniqueDamageRankLayout
================
*/
func TestEncodeUniqueDamageRankLayout(t *testing.T) {
	got := EncodeUniqueDamageRank(UniqueDamageRank{
		RefObjID:      1954,
		Total:         0x1_0000_0002,
		Rows:          []UniqueDamageRow{{Name: "Ann", Damage: 900, PartyRank: 2}, {Name: "Bo", Damage: 40}},
		SelfRank:      14,
		SelfDamage:    5,
		SelfPartyRank: 1,
	})
	want := binary.LittleEndian.AppendUint32(nil, 1954)
	want = binary.LittleEndian.AppendUint64(want, 0x1_0000_0002)
	want = append(want, 2)
	want = append(want, 3, 'A', 'n', 'n')
	want = binary.LittleEndian.AppendUint32(want, 900)
	want = append(want, 2)
	want = append(want, 2, 'B', 'o')
	want = binary.LittleEndian.AppendUint32(want, 40)
	want = append(want, 0)
	want = binary.LittleEndian.AppendUint16(want, 14)
	want = binary.LittleEndian.AppendUint32(want, 5)
	want = append(want, 1)
	if !bytes.Equal(got, want) {
		t.Fatalf("body % X, want % X", got, want)
	}

	// Rows past the cap and name bytes past the length byte's bound are cut.
	capped := EncodeUniqueDamageRank(UniqueDamageRank{Rows: make([]UniqueDamageRow, UniqueDamageRankMaxRows+3)})
	if capped[12] != UniqueDamageRankMaxRows {
		t.Fatalf("count %d, want the %d cap", capped[12], UniqueDamageRankMaxRows)
	}
	long := EncodeUniqueDamageRank(UniqueDamageRank{Rows: []UniqueDamageRow{{Name: strings.Repeat("x", 200)}}})
	if long[13] != uniqueDamageRankMaxName {
		t.Fatalf("name length %d, want the %d cap", long[13], uniqueDamageRankMaxName)
	}
}
