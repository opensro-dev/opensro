/*
===========================================================================

equipment_test.go - tests for ordinary and rare equipment selection

===========================================================================
*/

package loot

import (
	"errors"
	"strings"
	"testing"
)

/*
================
TestEquipmentSelectionAcrossShippedLevelRange
================
*/
func TestEquipmentSelectionAcrossShippedLevelRange(t *testing.T) {
	for _, tc := range []struct {
		level  uint8
		suffix string
	}{
		{3, "01_B"}, {20, "03_B"}, {50, "06_C"}, {80, "09_B"}, {90, "09_C"},
	} {
		for _, country := range []uint8{0, 1} {
			group, ok := EquipmentGroup(tc.level, false, 0)
			if !ok {
				t.Fatalf("level %d has no class", tc.level)
			}
			r, ok := SelectEquipment(country, group, false, tc.level, func() (uint32, error) { return 0, nil })
			if !ok || !strings.HasSuffix(r.Codename, tc.suffix) || r.Level > tc.level {
				t.Fatalf("level %d country %d: %+v / %v", tc.level, country, r, ok)
			}
		}
	}
	// Inspect every assigned bucket against both sides of the required-level
	// fallback. This catches higher armor requirements within a weapon tier.
	for key, b := range equipment.buckets {
		for i, ref := range b.refs {
			var wanted uint32
			if i > 0 {
				wanted = b.weights[i-1] + 1
			}
			for _, level := range []uint8{1, ref.Level, 90} {
				calls := 0
				r, ok := SelectEquipment(key.country, key.group, key.rare, level, func() (uint32, error) {
					calls++
					if calls == 1 {
						return wanted, nil
					}
					return 0, nil
				})
				if ok && (r.Level > level || r.Type != ref.Type || r.Country != key.country || r.Rare != key.rare) {
					t.Fatalf("fallback changed family or exceeded level: %+v -> %+v at %d", ref, r, level)
				}
				if level >= ref.Level && (!ok || r.Codename != ref.Codename) {
					t.Fatalf("admissible assignment lost: %+v -> %+v", ref, r)
				}
			}
		}
	}
}

/*
================
TestEquipmentProbabilityBoundaries
================
*/
func TestEquipmentProbabilityBoundaries(t *testing.T) {
	if g, ok := EquipmentGroup(1, false, 33332); !ok || g != 0 {
		t.Fatal("lower-bound inclusion lost")
	}
	if _, ok := EquipmentGroup(1, false, 33333); ok {
		t.Fatal("float32 threshold was rounded up")
	}
	for _, level := range []uint8{0, 181, 255} {
		if _, ok := EquipmentGroup(level, false, 0); ok {
			t.Fatal("invalid level admitted")
		}
	}
	if _, ok := EquipmentGroup(80, false, 999999); ok {
		t.Fatal("class miss admitted")
	}
	if g, ok := EquipmentGroup(80, true, 0); !ok || g != 24 {
		t.Fatalf("rare level80 group=%d/%v", g, ok)
	}
}

/*
================
TestAbsoluteRateAndRandomFailure
================
*/
func TestAbsoluteRateAndRandomFailure(t *testing.T) {
	for _, rate := range []uint32{0, 50, 100} {
		r := equipmentRef{Codename: "fixture", Type: "weapon", Weight: 100, Absolute: rate, Level: 1}
		c := equipmentCatalog{buckets: map[equipmentKey]*equipmentBucket{{0, 0, false}: {refs: []equipmentRef{r}, weights: []uint32{100}}}, widths: [2]int{1, 1}}
		for roll := uint32(0); roll < 100; roll++ {
			_, ok := c.selectEquipment(0, 0, false, 1, func() (uint32, error) { return roll, nil })
			if ok != (roll <= rate) {
				t.Fatalf("absolute %d roll %d: %v", rate, roll, ok)
			}
		}
	}
	for failAt := 1; failAt <= 2; failAt++ {
		calls := 0
		_, ok := SelectEquipment(0, 25, false, 80, func() (uint32, error) {
			calls++
			if calls == failAt {
				return 0, errors.New("rng unavailable")
			}
			return 0, nil
		})
		if ok {
			t.Fatal("RNG failure generated equipment")
		}
	}
}

/*
================
TestRareEquipmentFallbackStepsWithinTier

The native server sets the rare-equipment table's class-fallback
stride (+0x30) to 3 so the empty-bucket and required-level-exceeded
fallbacks step to the previous degree's same [Star, Moon, Sun] tier
rather than into an adjacent seal grade.
================
*/
func TestRareEquipmentFallbackStepsWithinTier(t *testing.T) {
	rollZero := func() (uint32, error) { return 0, nil }
	// Empty degree-10 Star (G27) and Moon (G28) buckets at level 86 must step
	// by 3 into degree-9 Star (G24 -> _09_A_RARE) and Moon (G25 -> _09_B_RARE),
	// never into degree-9 Sun (G26 -> _09_C_RARE).
	star10, ok := SelectEquipment(0, 27, true, 86, rollZero)
	if !ok || !strings.HasSuffix(star10.Codename, "_09_A_RARE") {
		t.Fatalf("empty G27 fallback: got %+v / %v, want *_09_A_RARE", star10, ok)
	}
	moon10, ok := SelectEquipment(0, 28, true, 86, rollZero)
	if !ok || !strings.HasSuffix(moon10.Codename, "_09_B_RARE") {
		t.Fatalf("empty G28 fallback: got %+v / %v, want *_09_B_RARE", moon10, ok)
	}
	// Required-level fallback within degree-9 Star (G24), Moon (G25), and Sun
	// (G26) at level 76 must step by 3 into degree-8 Star (G21 -> _08_*A_RARE),
	// Moon (G22 -> _08_*B_RARE), and Sun (G23 -> _08_*C_RARE).
	for _, tc := range []struct {
		group  int
		suffix string
	}{
		{24, "_A_RARE"},
		{25, "_B_RARE"},
		{26, "_C_RARE"},
	} {
		b := equipment.buckets[equipmentKey{0, tc.group, true}]
		foundHigh := false
		for i, ref := range b.refs {
			if ref.Level <= 76 || strings.HasSuffix(ref.Codename, "_BA_A_RARE") {
				continue
			}
			foundHigh = true
			var wanted uint32
			if i > 0 {
				wanted = b.weights[i-1] + 1
			}
			calls := 0
			got, ok := SelectEquipment(0, tc.group, true, 76, func() (uint32, error) {
				calls++
				if calls == 1 {
					return wanted, nil
				}
				return 0, nil
			})
			if !ok || got.Group != tc.group-3 || !strings.Contains(got.Codename, "_08_") {
				t.Fatalf("group %d ref %s (lvl %d) at level 76 fell back to %+v / %v, want group %d (*_08_*)", tc.group, ref.Codename, ref.Level, got, ok, tc.group-3)
			}
		}
		if !foundHigh {
			t.Fatalf("group %d had no >76 item to exercise level fallback", tc.group)
		}
	}
}

/*
================
BenchmarkEquipmentSelection
================
*/
func BenchmarkEquipmentSelection(b *testing.B) {
	roll := func() (uint32, error) { return 1234, nil }
	for i := 0; i < b.N; i++ {
		SelectEquipment(0, 25, false, 80, roll)
	}
}
