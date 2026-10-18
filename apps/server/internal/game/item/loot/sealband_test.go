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
TestSealBandGivesLevels81To94EndgameStarAndMoon
================
*/
func TestSealBandGivesLevels81To94EndgameStarAndMoon(t *testing.T) {
	if sealBandRows == nil {
		t.Fatal("the catalogue's rare rows lost the band's prefix shape")
	}
	starEdge := rareShares(EquipmentAttemptChances(sealStarEdge, 0, true))
	if starEdge["09A"] == 0 {
		t.Fatalf("level %d rolls no degree-9 Star: %v", sealStarEdge, starEdge)
	}
	moonEdge := rareShares(EquipmentAttemptChances(sealMoonEdge, 0, true))
	if moonEdge["09A"] == 0 || moonEdge["09B"] == 0 {
		t.Fatalf("level %d rolls no degree-9 Star or Moon: %v", sealMoonEdge, moonEdge)
	}
	// Measure level 80's G24 prefix (threshold 3554) and level 85's G25 prefix
	// (threshold 320) directly so the position shift of appended entries does
	// not conflate with existing G27/G28 entries.
	starPrefixAcc := chanceAccumulator{}
	for group, classMass := range classChances(equipment.classes[1][sealStarEdge-1][:1]) {
		equipment.selectionChances(0, group, true, sealStarEdge, classMass, starPrefixAcc.add)
	}
	starPrefix := rareShares(starPrefixAcc.sorted())
	if starPrefix["09A"] <= 0 {
		t.Fatalf("level %d prefix carries no Star mass: %v", sealStarEdge, starPrefix)
	}
	moonPrefixAcc := chanceAccumulator{}
	for group, classMass := range classChances(equipment.classes[1][sealMoonEdge-1][:1]) {
		equipment.selectionChances(0, group, true, sealMoonEdge, classMass, moonPrefixAcc.add)
	}
	moonPrefix := rareShares(moonPrefixAcc.sorted())
	if moonPrefix["09A"] <= 0 || moonPrefix["09B"] <= 0 {
		t.Fatalf("level %d prefix carries no Star or Moon mass: %v", sealMoonEdge, moonPrefix)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-12 }
	close := func(a, b float64) bool { return math.Abs(a-b) <= 0.005*b }
	for level := uint8(sealStarEdge + 1); level <= sealBandEnd; level++ {
		native := rareShares(EquipmentAttemptChances(level, 0, true))
		band := rareShares(SealBandRareChances(level, 0))
		if native["09A"] == 0 {
			t.Fatalf("level %d lost native G27->G24 Star fallback: %v", level, native)
		}
		if level <= 89 && native["09B"] == 0 {
			t.Fatalf("level %d lost native G25/G28 Moon: %v", level, native)
		}
		if level >= 90 && native["09B"] != 0 {
			t.Fatalf("level %d already rolls Moon natively: %v", level, native)
		}
		// Levels 81-84 append level 80's G24 Star prefix (3554); at levels
		// 81-83 some high-ReqLevel G24 picks step 24-3=21 into degree-8 Star
		// (08A), so the combined 09A+08A gain equals the prefix's 09A mass.
		// Levels 85 leaves Moon native and appends the G24 Star prefix (all
		// G24 items fit at level 85). Levels 86-89 append both the G24 Star
		// prefix and level 85's G25 Moon prefix. Levels 90-94 keep their
		// native G27 Star and append level 85's G25 Moon prefix.
		addedStar := (band["09A"] - native["09A"]) + (band["08A"] - native["08A"])
		addedMoon := band["09B"] - native["09B"]
		switch {
		case level <= sealMoonEdge:
			if !close(addedStar, starPrefix["09A"]) || !near(addedMoon, 0) {
				t.Fatalf("level %d: added Star %g Moon %g, want Star %g Moon 0", level, addedStar, addedMoon, starPrefix["09A"])
			}
		case level <= sealStarEnd:
			wantStar := starPrefix["09A"] + moonPrefix["09A"]
			if !close(addedStar, wantStar) || !close(addedMoon, moonPrefix["09B"]) {
				t.Fatalf("level %d: added Star %g Moon %g, want %g %g", level, addedStar, addedMoon, wantStar, moonPrefix["09B"])
			}
		default:
			if !close(addedStar, moonPrefix["09A"]) || !close(addedMoon, moonPrefix["09B"]) {
				t.Fatalf("level %d: added Star %g Moon %g, want %g %g", level, addedStar, addedMoon, moonPrefix["09A"], moonPrefix["09B"])
			}
		}
		if !near(band["09C"], native["09C"]) {
			t.Fatalf("level %d: Sun %g, want its own %g", level, band["09C"], native["09C"])
		}
	}
	for _, level := range []uint8{1, 50, 76, sealStarEdge, sealBandEnd + 1, 100} {
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
