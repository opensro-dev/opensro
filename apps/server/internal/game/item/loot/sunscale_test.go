/*
===========================================================================

sunscale_test.go - tests for the beta's wider degree-9 Sun class

Verifies that deriveSunClassGroup resolves group 26 (*_09_C_RARE), that
scaleSunClass widens only group 26 by the multiplier while leaving every
other class width untouched, and that ComposedRareChances matches the
scaled row across levels 76-90.

===========================================================================
*/
package loot

import (
	"math"
	"strings"
	"testing"
)

/*
================
isSunCodename

Every degree-9 Sun item of the scaled class carries _09_ and ends in
_C_RARE: weapons and shields exactly (ITEM_CH_SWORD_09_C_RARE),
armor and accessories with slot letters before it
(ITEM_CH_M_CLOTHES_09_AA_C_RARE, ITEM_CH_RING_09_C_RARE). A degree-8
_08_ C_RARE is the unscaled degree-8 Sun class.
================
*/
func isSunCodename(codename string) bool {
	return strings.Contains(codename, "_09_") && strings.HasSuffix(codename, "_C_RARE")
}

/*
================
isDegree8SunCodename
================
*/
func isDegree8SunCodename(codename string) bool {
	return strings.Contains(codename, "_08_") && strings.HasSuffix(codename, "_C_RARE")
}

/*
================
sweepMass

The sampler the stamp asks for: sweep the million-roll over the native
and the m=60 scaled row and compare class masses exactly. Widths are
integer thresholds, so the comparison is exact, not statistical.
================
*/
func sweepMass(row []classThreshold, group int) uint32 {
	var below, mass uint32
	for _, class := range row {
		if class.group == group {
			mass += class.threshold - below
		}
		below = class.threshold
	}
	return mass
}

/*
================
TestSunScaleDerivesTheCRAREClass
================
*/
func TestSunScaleDerivesTheCRAREClass(t *testing.T) {
	if sunClassGroup != 26 {
		t.Fatalf("derived Sun class %d, want 26 (the *_09_C_RARE class)", sunClassGroup)
	}
	if !SunScaleCovers(76) || !SunScaleCovers(90) || SunScaleCovers(75) || SunScaleCovers(91) {
		t.Fatal("the scale covers the wrong levels")
	}
}

/*
================
moonClassGroupForTest

The *_09_B_RARE class, derived from the catalogue's bucket references
the same way deriveSunClassGroup derives the Sun class.
================
*/
func moonClassGroupForTest(t *testing.T) int {
	t.Helper()
	group := -1
	for key, bucket := range equipment.buckets {
		if !key.rare {
			continue
		}
		for _, ref := range bucket.refs {
			if !strings.Contains(ref.Codename, "_09_") || !strings.HasSuffix(ref.Codename, "_B_RARE") {
				continue
			}
			if group >= 0 && group != key.group {
				t.Fatal("the *_09_B_RARE buckets disagree on their class")
			}
			group = key.group
		}
	}
	if group < 0 {
		t.Fatal("the catalogue holds no *_09_B_RARE class")
	}
	return group
}

/*
================
moonCapMass

The cap the Sun class answers to in row: the Moon class's own width,
or its last native width (level 85's row) when the row rolls no Moon
class (levels 86-90 without the seal band).
================
*/
func moonCapMass(row []classThreshold, moon int) uint32 {
	if mass := sweepMass(row, moon); mass > 0 {
		return mass
	}
	return sweepMass(equipment.classes[1][sealMoonEdge-1], moon)
}

/*
================
TestSunScaleCapsTheSunClassAtTheMoonClass

Owner decision (#1605 P1): the widened Sun class is never wider than
the Moon class, so with the degree-9 keep factors (Star x3, Moon x2,
Sun x1 over the widening 3) a Sun expects about twice a Moon's kills.
Below the cap the multiplier is exact; every other class keeps its
native width.
================
*/
func TestSunScaleCapsTheSunClassAtTheMoonClass(t *testing.T) {
	moon := moonClassGroupForTest(t)
	for level := uint8(76); level <= 90; level++ {
		native := equipment.classes[1][level-1]
		capMass := moonCapMass(native, moon)
		if capMass == 0 {
			t.Fatalf("level %d: no Moon cap derivable", level)
		}
		for _, multiplier := range []int{2, 60} {
			scaled := scaleSunClass(native, level, multiplier)
			sun := sweepMass(scaled, sunClassGroup)
			want := sweepMass(native, sunClassGroup) * uint32(multiplier)
			if want > capMass {
				want = capMass
			}
			if sun != want {
				t.Fatalf("level %d x%d Sun mass %d, want %d (native %d, Moon cap %d)",
					level, multiplier, sun, want, sweepMass(native, sunClassGroup), capMass)
			}
			for group := 0; group < equipment.widths[1]; group++ {
				if group == sunClassGroup {
					continue
				}
				if got, want := sweepMass(scaled, group), sweepMass(native, group); got != want {
					t.Fatalf("level %d class %d mass %d, want native %d", level, group, got, want)
				}
			}
		}
	}
}

/*
================
TestSunCapKeepsTheSealLadder

Per-kill degree-9 chances under the shipped defaults (seal band on,
the Sun rate at its default x60, 20 drop passes, the seal-widened rare
window 495/32768, keep factors Star 1, Moon 2/3, Sun 1/3): Star is
more common than Moon, Moon more common than Sun, and a Sun expects
about twice a Moon's kills at every level.
================
*/
func TestSunCapKeepsTheSealLadder(t *testing.T) {
	keep := map[byte]float64{'A': 1, 'B': 2.0 / 3, 'C': 1.0 / 3}
	const window = 495.0 / 32768.0
	for _, level := range []uint8{76, 80, 85, 90} {
		for _, country := range []uint8{0, 1} {
			tier := map[byte]float64{}
			for _, chance := range ComposedRareChances(level, country, true, 60) {
				if !strings.Contains(chance.Codename, "_09_") {
					continue
				}
				switch {
				case strings.HasSuffix(chance.Codename, "_A_RARE"):
					tier['A'] += chance.Chance
				case strings.HasSuffix(chance.Codename, "_B_RARE"):
					tier['B'] += chance.Chance
				case strings.HasSuffix(chance.Codename, "_C_RARE"):
					tier['C'] += chance.Chance
				}
			}
			perKill := func(seal byte) float64 {
				return 1 - math.Pow(1-window*tier[seal]*keep[seal], 20)
			}
			star, moon, sun := perKill('A'), perKill('B'), perKill('C')
			if !(star > moon && moon > sun && sun > 0) {
				t.Fatalf("level %d country %d: per-kill Star %g Moon %g Sun %g, want Star > Moon > Sun > 0",
					level, country, star, moon, sun)
			}
			if ratio := sun / moon; ratio < 0.35 || ratio > 0.65 {
				t.Fatalf("level %d country %d: Sun/Moon per-kill ratio %g, want about 1/2", level, country, ratio)
			}
		}
	}
}

/*
================
TestSunScaleM1IsTheNativeRow
================
*/
func TestSunScaleM1IsTheNativeRow(t *testing.T) {
	for _, level := range []uint8{76, 85, 90} {
		native := equipment.classes[1][level-1]
		scaled := scaleSunClass(native, level, 1)
		if len(scaled) != len(native) {
			t.Fatalf("level %d row length %d, want %d", level, len(scaled), len(native))
		}
		for i := range native {
			if scaled[i] != native[i] {
				t.Fatalf("level %d class %d %+v, want native %+v", level, i, scaled[i], native[i])
			}
		}
	}
}

/*
================
TestSunScaleComposedChancesMatchTheRow
================
*/
func TestSunScaleComposedChancesMatchTheRow(t *testing.T) {
	// The odds publication and the roll must agree. Exactness lives in
	// the integer sweep above (TestSunScaleCapsTheSunClassAtTheMoonClass):
	// the published chance folds each class mass through the combined
	// roll's modulo acceptance grid, which inflates the degenerate
	// high-level rows (at 85 the native row ends at threshold 890 of a
	// million) identically with this port on or off. So: at 76, whose
	// row is wide enough for the grid to be faithful, every level-76
	// degree-9 Sun item's published chance is the capped multiplier
	// (the Moon class's 64 over the native 3) times its native one
	// and every Star/Moon item stays at its native number (while G26
	// items with ref.Level > 76 step 26-3=23 into G23 degree-8 Sun per
	// the native selection); at 81, where all 51 G26 items satisfy
	// ref.Level <= 81, every degree-9 Sun item is at the capped
	// multiplier (320 over the native 6) and every other
	// item (including degree-8 Sun) stays at 1x; and a multiplier of 1
	// reproduces the native publication exactly, at any level.
	for _, level := range []uint8{76, 81, 85} {
		nativeChances := EquipmentAttemptChances(level, 0, true)
		nativeByName := map[string]float64{}
		for _, c := range nativeChances {
			nativeByName[c.Codename] += c.Chance
		}
		for _, c := range ComposedRareChances(level, 0, false, 1) {
			if base, ok := nativeByName[c.Codename]; !ok || c.Chance != base {
				t.Fatalf("level %d %s chance %v at m=1, want native %v", level, c.Codename, c.Chance, base)
			}
		}
	}
	base76 := nativeByNameAt(t, 76)
	checked76 := 0
	for _, c := range ComposedRareChances(76, 0, false, 60) {
		base, ok := base76[c.Codename]
		if !ok || base == 0 {
			continue
		}
		if isDegree8SunCodename(c.Codename) {
			// G26 items with ref.Level in 77..81 step by 3 (G26 -> G23) at
			// monster level 76 under the native selection (#1222).
			if c.Chance < base {
				t.Fatalf("level 76 %s chance %v, want >= native %v", c.Codename, c.Chance, base)
			}
			continue
		}
		want, tol := base, 0.10
		if isSunCodename(c.Codename) {
			want, tol = base*sunChanceMultiplier(t, 76), 0.06
			checked76++
		}
		if math.Abs(c.Chance-want) > want*tol+1e-12 {
			t.Fatalf("level 76 %s chance %v, want %v", c.Codename, c.Chance, want)
		}
	}
	if checked76 == 0 {
		t.Fatal("level 76 published no Sun chance to compare")
	}
	base81 := nativeByNameAt(t, 81)
	checked81 := 0
	for _, c := range ComposedRareChances(81, 0, false, 60) {
		base, ok := base81[c.Codename]
		if !ok || base == 0 {
			continue
		}
		want, tol := base, 0.10
		if isSunCodename(c.Codename) {
			want, tol = base*sunChanceMultiplier(t, 81), 0.06
			checked81++
		}
		if math.Abs(c.Chance-want) > want*tol+1e-12 {
			t.Fatalf("level 81 %s chance %v, want %v", c.Codename, c.Chance, want)
		}
	}
	if checked81 != 51 {
		t.Fatalf("level 81 checked %d Sun items, want all 51", checked81)
	}
}

/*
================
sunChanceMultiplier

The multiplier a degree-9 Sun item's published chance grows by at
x60: the capped Sun class mass over its native mass.
================
*/
func sunChanceMultiplier(t *testing.T, level uint8) float64 {
	t.Helper()
	native := equipment.classes[1][level-1]
	sun := sweepMass(native, sunClassGroup)
	want := sun * 60
	if capMass := moonCapMass(native, moonClassGroupForTest(t)); want > capMass {
		want = capMass
	}
	return float64(want) / float64(sun)
}

/*
================
nativeByNameAt
================
*/
func nativeByNameAt(t *testing.T, level uint8) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, c := range EquipmentAttemptChances(level, 0, true) {
		out[c.Codename] += c.Chance
	}
	return out
}
