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
	// Level 85's rare row begins with the G25 prefix (threshold 320) that
	// buildSealBandRows appends onto levels 86-94. Measure that prefix entry's
	// own degree-9 Star and Moon shares at level 85 directly so G27's position
	// shift between levels 85 and 86 does not add its modulo bias.
	prefixAcc := chanceAccumulator{}
	for group, classMass := range classChances(equipment.classes[1][sealBandEdge-1][:1]) {
		equipment.selectionChances(0, group, true, sealBandEdge, classMass, prefixAcc.add)
	}
	prefix := rareShares(prefixAcc.sorted())
	if prefix["09A"] <= 0 || prefix["09B"] <= 0 {
		t.Fatalf("level %d prefix carries no Star or Moon mass: %v", sealBandEdge, prefix)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-12 }
	close := func(a, b float64) bool { return math.Abs(a-b) <= 0.005*b }
	for level := uint8(sealBandEdge + 1); level <= sealBandEnd; level++ {
		native := rareShares(EquipmentAttemptChances(level, 0, true))
		band := rareShares(SealBandRareChances(level, 0))
		// With native step=3 (726f19 / 724120), G27 (D10 Star) at levels 86-94
		// steps 27-3=24 into D9 Star natively, and G28 (D10 Moon) at levels
		// 86-89 steps 28-3=25 into D9 Moon natively; levels 90-94 have no
		// native D9 Moon without the band.
		if native["09A"] == 0 {
			t.Fatalf("level %d lost native G27->G24 Star fallback: %v", level, native)
		}
		if level <= 89 && native["09B"] == 0 {
			t.Fatalf("level %d lost native G28->G25 Moon fallback: %v", level, native)
		}
		if level >= 90 && native["09B"] != 0 {
			t.Fatalf("level %d already rolls Moon natively: %v", level, native)
		}
		// The appended G25 prefix sits after each row's last threshold, so its
		// added Star/Moon mass matches level 85's prefix up to the combined
		// roll's modulo bias (< 0.5%).
		if !close(band["09A"]-native["09A"], prefix["09A"]) || !close(band["09B"]-native["09B"], prefix["09B"]) {
			t.Fatalf("level %d: added Star %g Moon %g, want prefix %g %g", level, band["09A"]-native["09A"], band["09B"]-native["09B"], prefix["09A"], prefix["09B"])
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
