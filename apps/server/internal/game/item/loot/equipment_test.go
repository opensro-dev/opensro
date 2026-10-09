package loot

import (
	"errors"
	"strings"
	"testing"
)

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

func BenchmarkEquipmentSelection(b *testing.B) {
	roll := func() (uint32, error) { return 1234, nil }
	for i := 0; i < b.N; i++ {
		SelectEquipment(0, 25, false, 80, roll)
	}
}
