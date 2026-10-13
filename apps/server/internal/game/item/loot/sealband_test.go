/*
===========================================================================

sealband_test.go - tests for sealband.go

===========================================================================
*/

package loot

import (
	"math"
	"regexp"
	"testing"
)

// sealKind names a rare item's degree and seal letter from its codename.
var sealKind = regexp.MustCompile(`_(\d\d)_(?:[A-Z]+_)*([ABC])_RARE$`)

/*
================
rareShares

A rare attempt's chance per degree and seal letter.
================
*/
func rareShares(chances []Chance) map[string]float64 {
	out := map[string]float64{}
	for _, c := range chances {
		if m := sealKind.FindStringSubmatch(c.Codename); m != nil {
			out[m[1]+m[2]] += c.Chance
		}
	}
	return out
}

/*
================
TestSealBandGivesLevels86To94Level85StarAndMoon
================
*/
func TestSealBandGivesLevels86To94Level85StarAndMoon(t *testing.T) {
	if sealBandRows == nil {
		t.Fatal("the catalogue's rare rows lost the band's prefix shape")
	}
	edge := rareShares(EquipmentAttemptChances(sealBandEdge, 0, true))
	if edge["09A"] == 0 || edge["09B"] == 0 {
		t.Fatalf("level %d rolls no degree-9 Star or Moon: %v", sealBandEdge, edge)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-12 }
	for level := uint8(sealBandEdge + 1); level <= sealBandEnd; level++ {
		native := rareShares(EquipmentAttemptChances(level, 0, true))
		band := rareShares(SealBandRareChances(level, 0))
		if native["09A"] != 0 || native["09B"] != 0 {
			t.Fatalf("level %d already rolls Star or Moon natively: %v", level, native)
		}
		// The appended band sits at another place in the combined roll than
		// level 85's, so its mass differs by the roll's modulo bias only.
		close := func(a, b float64) bool { return math.Abs(a-b) <= 0.005*b }
		if !close(band["09A"], edge["09A"]) || !close(band["09B"], edge["09B"]) {
			t.Fatalf("level %d: Star %g Moon %g, want level %d's %g %g", level, band["09A"], band["09B"], sealBandEdge, edge["09A"], edge["09B"])
		}
		if !near(band["09C"], native["09C"]) {
			t.Fatalf("level %d: Sun %g, want its own %g", level, band["09C"], native["09C"])
		}
	}
	for _, level := range []uint8{1, 50, 76, sealBandEdge, sealBandEnd + 1, 100} {
		native := EquipmentAttemptChances(level, 0, true)
		band := SealBandRareChances(level, 0)
		if len(native) != len(band) {
			t.Fatalf("level %d outside the band changed: %d vs %d chances", level, len(native), len(band))
		}
		for i := range native {
			if native[i] != band[i] {
				t.Fatalf("level %d outside the band changed at %v", level, native[i])
			}
		}
	}
}
