/*
===========================================================================

random_group_test.go - finite sample-space proofs for accepted loot outcomes

These tests compare integer admission masses with exhaustive enumeration and
exercise real group members, including rates too small for bounded retries.

===========================================================================
*/
package loot

import (
	"errors"
	"testing"
)

/*
================
TestModuloAcceptanceMassExhaustive
================
*/
func TestModuloAcceptanceMassExhaustive(t *testing.T) {
	for samples := uint64(1); samples <= 64; samples++ {
		for denominator := uint64(1); denominator <= 17; denominator++ {
			for threshold := uint64(0); threshold <= denominator; threshold++ {
				var want uint64
				for value := uint64(0); value < samples; value++ {
					if value%denominator <= threshold {
						want++
					}
				}
				if got := moduloAcceptanceMass(samples, denominator, threshold); got != want {
					t.Fatalf("%d samples, modulus %d, threshold %d: %d != %d", samples, denominator, threshold, got, want)
				}
			}
		}
	}
}

/*
================
wideRoll
================
*/
func wideRoll(value uint64) func() (uint32, error) {
	shift := wideRandomBits
	return func() (uint32, error) {
		if shift == 0 {
			return 0, errors.New("exhausted test entropy")
		}
		shift -= randomBits
		return uint32(value>>shift) & 32767, nil
	}
}

/*
================
TestGroupMassRetainsOriginalIndexModuloBias
================
*/
func TestGroupMassRetainsOriginalIndexModuloBias(t *testing.T) {
	pool := []groupDrop{{"A", 1}, {"B", 1}, {"C", 1}}
	selected := []bool{true, false, false}
	if groupMemberMass(1, 3, 1)-groupMemberMass(2, 3, 1) != combinedRange {
		t.Fatal("original candidate-index bias lost")
	}
	boundary := groupMemberMass(1, 3, 1)
	for _, tc := range []struct {
		value uint64
		index int
	}{{0, 1}, {boundary - 1, 1}, {boundary, 2}} {
		index, err := selectGroupMember(pool, selected, wideRoll(tc.value))
		if err != nil || index != tc.index {
			t.Fatalf("conditioned boundary %d: %d/%v", tc.value, index, err)
		}
	}
}

/*
================
TestEveryPublishedGroupMemberIsSelectable
================
*/
func TestEveryPublishedGroupMemberIsSelectable(t *testing.T) {
	if len(consumables.random) != 21 || len(consumables.groups) != 2 {
		t.Fatal("compatible ISRO group join changed")
	}
	for id, pool := range consumables.groups {
		var prefix uint64
		for index, row := range pool {
			got, err := selectGroupMember(pool, make([]bool, len(pool)), wideRoll(prefix))
			if err != nil || got != index {
				t.Fatalf("group %d member %s: %d/%v", id, row.Codename, got, err)
			}
			prefix += groupMemberMass(index, len(pool), row.Probability)
		}
	}
}

/*
================
TestAssignedGroupUsesAuthoredEquipmentInitialization
================
*/
func TestAssignedGroupUsesAuthoredEquipmentInitialization(t *testing.T) {
	c := consumableCatalog{groups: map[int][]groupDrop{1: {{"ITEM_CH_SWORD_03_A", 1}}},
		random: map[string][]assignedRandom{"M": {{Monster: "M", Group: 1, Min: 1, Max: 1, Probability: 1}}}}
	got := c.assigned("M", 1, zeroRoll)
	if len(got) != 1 || !got[0].Assigned || got[0].Special || got[0].Plus != 0 {
		t.Fatal("assigned equipment lost its initialization contract", got)
	}
}

/*
================
TestUniformBelowRejectsWithoutDiscardingReward
================
*/
func TestUniformBelowRejectsWithoutDiscardingReward(t *testing.T) {
	calls := 0
	value, err := uniformBelow(3, func() (uint32, error) {
		calls++
		if calls <= 4 {
			return 32767, nil
		}
		return 0, nil
	})
	if err != nil || value != 0 || calls != 8 {
		t.Fatalf("rejection boundary: %d/%d/%v", value, calls, err)
	}
	if _, err := uniformBelow(3, func() (uint32, error) { return 32768, nil }); err == nil {
		t.Fatal("out-of-range entropy accepted")
	}
	if _, err := uniformBelow(3, func() (uint32, error) { return 0, errors.New("entropy unavailable") }); err == nil {
		t.Fatal("entropy failure suppressed")
	}
}
