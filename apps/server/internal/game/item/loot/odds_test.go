/*
===========================================================================

odds_test.go - the exact chances agree with the selectors they describe

Each test samples the real selector with a seeded 15-bit source and holds
the observed frequencies to the computed chances within a few standard
errors, so a change to either side shows up here.

===========================================================================
*/

package loot

import (
	"math"
	"math/rand/v2"
	"testing"
)

// oddsSamples is the selector runs per comparison.
const oddsSamples = 200_000

/*
================
seededRoll
================
*/
func seededRoll(seed uint64) func() (uint32, error) {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	return func() (uint32, error) { return uint32(r.IntN(int(randomRange))), nil }
}

/*
================
millionFrom
================
*/
func millionFrom(roll func() (uint32, error)) uint32 {
	a, _ := roll()
	b, _ := roll()
	return ((b << 15) | a) % 1_000_000
}

/*
================
compareChances

Every computed chance against its observed frequency, and the total
success against the observed successes.
================
*/
func compareChances(t *testing.T, label string, chances []Chance, observed map[chanceKey]int, successes int) {
	t.Helper()
	var total float64
	for _, c := range chances {
		total += c.Chance
		got := float64(observed[chanceKey{c.Codename, c.Count}]) / oddsSamples
		tolerance := 5*math.Sqrt(c.Chance*(1-c.Chance)/oddsSamples) + 1e-5
		if math.Abs(got-c.Chance) > tolerance {
			t.Errorf("%s %s x%d: computed %.6f, sampled %.6f", label, c.Codename, c.Count, c.Chance, got)
		}
		delete(observed, chanceKey{c.Codename, c.Count})
	}
	for key, n := range observed {
		if float64(n)/oddsSamples > 1e-4 {
			t.Errorf("%s %v sampled %d times but has no computed chance", label, key, n)
		}
	}
	if total > 1+1e-9 {
		t.Errorf("%s chances sum to %v", label, total)
	}
	if got := float64(successes) / oddsSamples; math.Abs(got-total) > 5*math.Sqrt(total*(1-total)/oddsSamples)+1e-5 {
		t.Errorf("%s success computed %.6f, sampled %.6f", label, total, got)
	}
}

/*
================
TestEquipmentChancesMatchTheSelector
================
*/
func TestEquipmentChancesMatchTheSelector(t *testing.T) {
	for _, tc := range []struct {
		level, country uint8
		rare           bool
	}{{8, 0, false}, {33, 3, false}, {57, 1, false}, {72, 3, true}} {
		roll := seededRoll(uint64(tc.level))
		observed, successes := map[chanceKey]int{}, 0
		for i := 0; i < oddsSamples; i++ {
			group, ok := EquipmentGroup(tc.level, tc.rare, millionFrom(roll))
			if !ok {
				continue
			}
			country := tc.country
			if country == 3 {
				n, _ := roll()
				country = uint8(n & 1)
			}
			if ref, ok := SelectEquipment(country, group, tc.rare, tc.level, roll); ok {
				observed[chanceKey{ref.Codename, max(ref.Count, 1)}]++
				successes++
			}
		}
		compareChances(t, "equipment", EquipmentAttemptChances(tc.level, tc.country, tc.rare), observed, successes)
	}
}

/*
================
TestConsumableChancesMatchTheSelector
================
*/
func TestConsumableChancesMatchTheSelector(t *testing.T) {
	for _, family := range []int{2, 3, 7, 10} {
		for _, level := range []uint8{5, 41, 88} {
			roll := seededRoll(uint64(family)*1000 + uint64(level))
			observed, successes := map[chanceKey]int{}, 0
			for i := 0; i < oddsSamples; i++ {
				if item, ok := SelectConsumable(family, level, millionFrom(roll), roll); ok {
					observed[chanceKey{item.Codename, item.Count}]++
					successes++
				}
			}
			compareChances(t, "consumable", ConsumableAttemptChances(family, level), observed, successes)
		}
	}
}

/*
================
TestAssignedChancesReadTheMonstersRows
================
*/
func TestAssignedChancesReadTheMonstersRows(t *testing.T) {
	rows, err := AssignedDropChances("MOB_CH_TIGERWOMAN")
	if err != nil || len(rows) == 0 {
		t.Fatalf("Tiger Girl rows %v, %v", rows, err)
	}
	for _, row := range rows {
		if row.Chance < 0 || row.Chance > 1 || row.PerKill < row.Chance-1e-12 && row.Min >= 1 {
			t.Errorf("%+v", row)
		}
	}
	if countedChance(1, 1, 1) != 1 || countedChance(0, 0, 0.5) != 0 {
		t.Fatal("countedChance edges")
	}
	// Count 1..2 over 32768 draws: an even split, so 0.5*p + 0.5*(1-(1-p)^2).
	if got, want := countedChance(1, 2, 0.5), 0.5*0.5+0.5*0.75; math.Abs(got-want) > 1e-12 {
		t.Fatalf("countedChance 1..2 = %v, want %v", got, want)
	}
}
