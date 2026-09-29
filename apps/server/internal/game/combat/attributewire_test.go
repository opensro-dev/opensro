/*
===========================================================================

attributewire_test.go - primary attributes survive shared stat publication

Effect refreshes encode the combat projection directly. Check the final
native words as well as the login adapter to cover both publication paths.

===========================================================================
*/
package combat

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestPlayerBaseStatsPreservesPrimaryAttributeWords

Different attributes catch swapped words; the upper bound catches narrowing.
================
*/
func TestPlayerBaseStatsPreservesPrimaryAttributeWords(t *testing.T) {
	const strengthOffset = 32
	const intellectOffset = 34
	for _, attributes := range [][2]int64{{59, 73}, {65535, 20}, {0, 0}} {
		character := &domain.Character{
			Level: pointer(1), Strength: pointer(attributes[0]), Intellect: pointer(attributes[1]),
		}
		stats, err := PlayerBaseStats(character, Catalogs{Items: itemRefs{}})
		if err != nil {
			t.Fatal(err)
		}
		for _, payload := range [][]byte{stats.Encode(), enterworld.BuildLoginStatBlock(character, stats)} {
			strength := binary.LittleEndian.Uint16(payload[strengthOffset:])
			intellect := binary.LittleEndian.Uint16(payload[intellectOffset:])
			if int64(strength) != attributes[0] || int64(intellect) != attributes[1] {
				t.Fatalf("attribute words = %d/%d, want %d/%d", strength, intellect, attributes[0], attributes[1])
			}
		}
	}
}
