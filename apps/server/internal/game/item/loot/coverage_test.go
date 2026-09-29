/*
===========================================================================

coverage_test.go - production selection witnesses and inferred band boundaries

Every assignment must be reachable with actual 15-bit draws through its owning
selector. Constructing a ground item directly cannot establish that property.

===========================================================================
*/
package loot

import (
	"encoding/json"
	"fmt"
	"testing"
)

/*
================
TestInferredConsumableBandBoundaries
================
*/
func TestInferredConsumableBandBoundaries(t *testing.T) {
	for _, level := range []uint8{1, 19, 20, 39, 40, 59, 60, 79, 80, 89, 90, 110} {
		band := 0
		for _, boundary := range []uint8{20, 40, 60, 80, 90} {
			if level >= boundary {
				band++
			}
		}
		for _, family := range []int{2, 3, 4, 5, 6} {
			got, ok := SelectConsumable(family, level, 0, zeroRoll)
			if !ok {
				t.Fatalf("family %d level %d missing", family, level)
			}
			want, count, threshold := "", uint16(1), uint32(0)
			switch family {
			case 2:
				want, threshold = fmt.Sprintf("ITEM_ETC_HP_POTION_%02d", band+1), 100000
				if band == 5 {
					want = "ITEM_ETC_HP_SPOTION_01"
				}
			case 3:
				want, threshold = fmt.Sprintf("ITEM_ETC_CURE_ALL_%02d", min(band, 3)+1), 19999
			case 4, 5:
				want, threshold = "ITEM_ETC_AMMO_ARROW_01", 50000
				if family == 5 {
					want = "ITEM_ETC_AMMO_BOLT_01"
				}
				count = [...]uint16{20, 50, 70, 100, 150, 250}[band]
			case 6:
				want, threshold = "ITEM_ETC_SCROLL_RETURN_01", 9999
			}
			if got.Codename != want || got.Count != count {
				t.Fatalf("level %d family %d: %+v want %s/%d", level, family, got, want, count)
			}
			if _, ok := SelectConsumable(family, level, threshold, zeroRoll); !ok {
				t.Fatal("inclusive boundary lost", family)
			}
			if _, ok := SelectConsumable(family, level, threshold+1, zeroRoll); ok {
				t.Fatal("upper boundary admitted", family)
			}
		}
	}
}

/*
================
TestEveryConsumableAssignmentHasProductionWitness
================
*/
func TestEveryConsumableAssignmentHasProductionWitness(t *testing.T) {
	for family, catalog := range consumables.families {
		for key, bucket := range catalog.buckets {
			level := uint8(0)
			var classRoll uint32
			for at, classes := range catalog.classes[0] {
				for index, row := range classes {
					if row.group == key.group {
						level = uint8(at + 1)
						if index > 0 {
							classRoll = classes[index-1].threshold + 1
						}
						break
					}
				}
				if level != 0 {
					break
				}
			}
			if level == 0 {
				t.Fatalf("family %d class %d unreachable", family, key.group)
			}
			for index, ref := range bucket.refs {
				var wanted uint32
				if index > 0 {
					wanted = bucket.weights[index-1] + 1
				}
				if wanted > 32767 {
					t.Fatal("assignment unreachable with native draw", ref.Codename)
				}
				calls := 0
				got, ok := SelectConsumable(family, level, classRoll, func() (uint32, error) {
					calls++
					if calls == 1 {
						return wanted, nil
					}
					return 0, nil
				})
				if !ok || got.Codename != ref.Codename || got.Count != ref.Count {
					t.Fatalf("%s witness: %+v/%v", ref.Codename, got, ok)
				}
			}
		}
	}
}

/*
================
TestDistinctGroupOvercommitIsInvalid
================
*/
func TestDistinctGroupOvercommitIsInvalid(t *testing.T) {
	var source consumableSource
	if err := json.Unmarshal(consumablesJSON, &source); err != nil {
		t.Fatal(err)
	}
	source.Groups[1] = []groupDrop{{"A", 0}}
	source.Random = []assignedRandom{{Monster: "M", Group: 1, Distinct: true, Min: 2, Max: 2, Probability: 1}}
	if _, err := compileConsumables(source); err == nil {
		t.Fatal("impossible distinct reward accepted")
	}
}

/*
================
TestEquipmentMagicAndNonRepairProperties
================
*/
func TestEquipmentMagicAndNonRepairProperties(t *testing.T) {
	code := "ITEM_CH_SWORD_03_A"
	options, entered, err := EquipmentMagic(code, zeroRoll)
	if err != nil || !entered || len(options) != 1 || uint16(options[0]) != 13 || options[0]>>32 != 1 {
		t.Fatalf("client degree-3 strength option: %v/%v/%v", options, entered, err)
	}
	option, percent, ok := NonRepairOption(code, len(options))
	if !ok || uint16(option) != 65 || option>>32 != 400 || percent != 400 {
		t.Fatal("nonrepair property mismatch")
	}
	if _, _, ok := NonRepairOption("ITEM_CH_SWORD_03_A_RARE", 0); ok {
		t.Fatal("rare equipment marked nonrepair")
	}
	_, entered, err = EquipmentMagic(code, func() (uint32, error) { return 31, nil })
	if err != nil || entered {
		t.Fatal("normal magic admission upper boundary")
	}
}

/*
================
TestEveryEquipmentAssignmentHasProductionWitness
================
*/
func TestEveryEquipmentAssignmentHasProductionWitness(t *testing.T) {
	for _, bucket := range equipment.buckets {
		for index, ref := range bucket.refs {
			var alternative uint32
			for at, candidate := range bucket.alternatives[ref.Type] {
				if candidate == uint32(index) {
					alternative = uint32(at)
					break
				}
			}
			if !equipmentProductionWitness(ref, alternative) {
				t.Fatal("equipment has no production class/assignment witness", ref.Codename)
			}
		}
	}
}

/*
================
equipmentProductionWitness

Search actual class boundaries and native-sized assignment draws, including
the same-type fallback needed for armor above its class's weapon requirement.
================
*/
func equipmentProductionWitness(ref equipmentRef, alternative uint32) bool {
	kind := 0
	if ref.Rare {
		kind = 1
	}
	for level := max(1, int(ref.Level)); level <= 180; level++ {
		for _, class := range equipment.classes[kind][level-1] {
			group, ok := EquipmentGroup(uint8(level), ref.Rare, class.threshold)
			if !ok {
				continue
			}
			initial := group
			for initial >= 0 && equipment.buckets[equipmentKey{ref.Country, initial, ref.Rare}] == nil {
				initial--
			}
			if initial < 0 {
				continue
			}
			candidates := equipment.buckets[equipmentKey{ref.Country, initial, ref.Rare}]
			for at, candidate := range candidates.refs {
				if candidate.Type != ref.Type {
					continue
				}
				first := uint32(0)
				if at > 0 {
					first = candidates.weights[at-1] + 1
				}
				if first > 32767 {
					continue
				}
				calls := 0
				selected, ok := SelectEquipment(ref.Country, group, ref.Rare, uint8(level), func() (uint32, error) {
					calls++
					if calls == 1 {
						return first, nil
					}
					return alternative, nil
				})
				if ok && selected.Codename == ref.Codename {
					return true
				}
			}
		}
	}
	return false
}
