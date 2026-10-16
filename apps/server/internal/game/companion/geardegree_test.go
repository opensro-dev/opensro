/*
===========================================================================

geardegree_test.go - degree bands, blue caps and improving roll floors

===========================================================================
*/
package companion

import (
	"encoding/json"
	"math"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestGearDegreeBandsAndRolls
================
*/
func TestGearDegreeBandsAndRolls(t *testing.T) {
	levels := []uint8{1, 8, 16, 24, 32, 42, 52, 64, 76}
	caps := []int{1, 2, 2, 3, 3, 4, 4, 5, 6}
	for i, d := range D.Gear {
		if d.MinPetLevel != levels[i] || d.MaxBlues != caps[i] || d.RollFloor != float64(i)/16 {
			t.Fatalf("degree %d: %+v", i+1, d)
		}
		for level := levels[i]; level <= 100 && (i == 8 || level < levels[i+1]); level++ {
			for _, unique := range []bool{false, true} {
				got, ok := DegreeForLevel(level, unique)
				want := min(i+1, 8)
				if !unique {
					want = i
				}
				if !ok || got.Degree != want+1 {
					t.Fatalf("level %d unique %v: %+v", level, unique, got)
				}
			}
		}
		for slot := range d.Items {
			for _, rarity := range []float64{0, d.RareChance + 0.001} {
				for _, pick := range []float64{0, 0.5, 0.999999} {
					rolls := fixedRoller{rarity, 0.999999}
					if rarity != 0 {
						rolls[1] = 0
					}
					for range 40 {
						rolls = append(rolls, pick)
					}
					options := RollGear(d, slot, 0, &rolls)
					want := min(2, caps[i])
					if rarity == 0 {
						want = caps[i]
					}
					if len(options) != want {
						t.Fatalf("D%d slot%d: got%d blues, want%d", d.Degree, slot, len(options), want)
					}
					for _, option := range options {
						a, value, ok := DecodeAffix(option)
						floor := a.Min + math.Floor((a.Max-a.Min)*d.RollFloor)
						if !ok || float64(value) < floor || float64(value) > a.Max {
							t.Fatalf("D%d invalid roll %v: %d", d.Degree, a, value)
						}
					}
				}
			}
		}
	}
}

/*
================
TestPetGearSixBluesJSONCompatibility

Old five-option saves still load; new six-option saves retain every option.
================
*/
func TestPetGearSixBluesJSONCompatibility(t *testing.T) {
	var gear domain.PetGear
	if err := json.Unmarshal([]byte(`{"refObjId":30109,"options":[1,2,3,4,5]}`), &gear); err != nil || gear.Options[4] != 5 || gear.Options[5] != 0 {
		t.Fatalf("old gear: %+v %v", gear, err)
	}
	gear.Options[5] = EncodeAffix(D.Affixes[0].Code, 42)
	raw, err := json.Marshal(gear)
	if err != nil {
		t.Fatal(err)
	}
	var restored domain.PetGear
	if err := json.Unmarshal(raw, &restored); err != nil || restored != gear {
		t.Fatalf("sixth blue lost: %+v %v", restored, err)
	}
}
