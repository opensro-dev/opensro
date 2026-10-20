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
TestSunScaleM60WidensSunSixtyfoldOnly
================
*/
func TestSunScaleM60WidensSunSixtyfoldOnly(t *testing.T) {
	for _, level := range []uint8{76, 80, 85, 86, 90} {
		native := equipment.classes[1][level-1]
		scaled := scaleSunClass(native, level, 60)
		sun := sweepMass(scaled, sunClassGroup)
		if want := sweepMass(native, sunClassGroup) * 60; sun != want {
			t.Fatalf("level %d Sun mass %d, want %d (native %d x60)", level, sun, want, sweepMass(native, sunClassGroup))
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
	// the integer sweep above (TestSunScaleM60WidensSunSixtyfoldOnly):
	// the published chance folds each class mass through the combined
	// roll's modulo acceptance grid, which inflates the degenerate
	// high-level rows (at 85 the native row ends at threshold 890 of a
	// million) identically with this port on or off. So: at 76, whose
	// row is wide enough for the grid to be faithful, every level-76
	// degree-9 Sun item's published chance is sixty times its native one
	// and every Star/Moon item stays at its native number (while G26
	// items with ref.Level > 76 step 26-3=23 into G23 degree-8 Sun per
	// native 0x00724120); at 81, where all 51 G26 items satisfy
	// ref.Level <= 81, every degree-9 Sun item is 60x and every other
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
			// monster level 76 under native 0x00724120 (#1222).
			if c.Chance < base {
				t.Fatalf("level 76 %s chance %v, want >= native %v", c.Codename, c.Chance, base)
			}
			continue
		}
		want, tol := base, 0.10
		if isSunCodename(c.Codename) {
			want, tol = base*60, 0.06
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
			want, tol = base*60, 0.06
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
