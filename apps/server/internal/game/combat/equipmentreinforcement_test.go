/*
===========================================================================

equipmentreinforcement_test.go - reinforcement family and lifetime coverage

Exercise the public parameter projection so login, item changes, breakage and
combat all consume the same equipment coefficients instead of local patches.

===========================================================================
*/
package combat

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"
)

/*
================
reinforcementCharacter

Keep the reference and persistent item identity aligned for every family case.
================
*/
func reinforcementCharacter(ref *enterworld.ItemRef, slot int64) *domain.Character {
	return &domain.Character{
		Level: pointer(1), Strength: pointer(100), Intellect: pointer(100),
		MissionInventory: []domain.InventoryRow{{Slot: slot, RefObjID: ref.RefObjID,
			Codename: ref.Codename, TypeFlags: ref.TypeFlags(), VarianceBits: "0", Durability: 1}},
	}
}

/*
================
TestWeaponReinforcementReplacesUnarmedCoefficients

Every weapon family uses the same keeper insertion. Equipment coefficients
replace source zero; independent STR/INT modifiers still flow through the graph.
================
*/
func TestWeaponReinforcementReplacesUnarmedCoefficients(t *testing.T) {
	const weaponSlot = 6
	const firstBagSlot = 13
	const weaponKinds = 16
	for kind := int64(1); kind <= weaponKinds; kind++ {
		t.Run(fmt.Sprintf("weapon-%d", kind), func(t *testing.T) {
			ref := &enterworld.ItemRef{RefObjID: 1, Codename: "WEAPON", TypeIDs: [4]int64{3, 1, 6, kind}, Combat: &enterworld.ItemCombatRef{
				PhysicalReinforcement: enterworld.ItemAttackRange{Minimum: enterworld.ItemStatRange{Min: .75, Max: 1}, Maximum: enterworld.ItemStatRange{Min: 1, Max: 1.25}},
				MagicalReinforcement:  enterworld.ItemAttackRange{Minimum: enterworld.ItemStatRange{Min: 1.5, Max: 1.75}, Maximum: enterworld.ItemStatRange{Min: 2, Max: 2.25}},
			}}
			c := reinforcementCharacter(ref, weaponSlot)
			catalogs := Catalogs{Items: itemRefs{ref.Codename: ref}}
			for _, state := range []struct {
				name             string
				slot, durability int64
				equipped         bool
			}{
				{"equipped", weaponSlot, 1, true}, {"broken", weaponSlot, 0, false}, {"repaired", weaponSlot, 1, true},
				{"bag", firstBagSlot, 1, false}, {"reequipped", weaponSlot, 1, true},
			} {
				c.MissionInventory[0].Slot, c.MissionInventory[0].Durability = state.slot, state.durability
				stats, _, err := PlayerStatsWithModifiers(c.Snapshot(), catalogs, []paramkeeper.Write{{Parameter: 1, Source: 4096, Value: 100}}, nil)
				if err != nil {
					t.Fatal(err)
				}
				physical, magical := float32(30.5999985), float32(49)
				if state.equipped {
					physical, magical = 75, 150
				}
				if p, _ := stats.Param(34); p != physical {
					t.Fatalf("%s physical coefficient %g, want %g", state.name, p, physical)
				}
				if m, _ := stats.Param(35); m != magical {
					t.Fatalf("%s magical coefficient %g, want %g", state.name, m, magical)
				}
				if state.equipped && (stats.PhysicalAttackMin != 150 || stats.PhysicalAttackMax != 200 || stats.MagicalAttackMin != 150 || stats.MagicalAttackMax != 200) {
					t.Fatalf("%s derived attacks %+v", state.name, stats)
				}
			}
		})
	}
}

/*
================
TestArmorAndShieldReinforcementUseTheirOwnParameters

Cover Chinese and European armor parts and both shield subtypes. Independent
piece parameters prevent an equip-order-dependent whole-armor replacement.
================
*/
func TestArmorAndShieldReinforcementUseTheirOwnParameters(t *testing.T) {
	for _, family := range []int64{1, 2, 3, 9, 10, 11, 4} {
		parts := int64(6)
		if family == 4 {
			parts = 2
		}
		for part := int64(1); part <= parts; part++ {
			ref := &enterworld.ItemRef{RefObjID: 1, Codename: "PROTECTOR", TypeIDs: [4]int64{3, 1, family, part}, Combat: &enterworld.ItemCombatRef{
				PhysicalDefenseReinforcement: enterworld.ItemStatRange{Min: .125, Max: .25},
				MagicalDefenseReinforcement:  enterworld.ItemStatRange{Min: .25, Max: .5},
			}}
			slot := part - 1
			physical, magical := uint16(37+part), uint16(43+part)
			if family == 4 {
				slot = 7
				physical, magical = 58, 59
			}
			c := reinforcementCharacter(ref, slot)
			stats, _, err := PlayerStats(c.Snapshot(), Catalogs{Items: itemRefs{ref.Codename: ref}})
			if err != nil {
				t.Fatal(err)
			}
			p, _ := stats.Param(physical)
			m, _ := stats.Param(magical)
			if p != 12.5 || m != 25 {
				t.Fatalf("family %d part %d: coefficients %g/%g, want 12.5/25", family, part, p, m)
			}
			c.MissionInventory[0].Durability = 0
			broken, _, err := PlayerStats(c.Snapshot(), Catalogs{Items: itemRefs{ref.Codename: ref}})
			if err != nil {
				t.Fatal(err)
			}
			if stats.PhysicalDefense <= broken.PhysicalDefense || stats.MagicalDefense <= broken.MagicalDefense {
				t.Fatalf("family %d part %d reinforcement did not reach defenses", family, part)
			}
		}
	}
}

/*
================
TestReinforcementVarianceLanesAreIndependent
================
*/
func TestReinforcementVarianceLanesAreIndependent(t *testing.T) {
	ref := &enterworld.ItemRef{TypeIDs: [4]int64{3, 1, 6, 2}, Combat: &enterworld.ItemCombatRef{
		PhysicalReinforcement: enterworld.ItemAttackRange{Minimum: enterworld.ItemStatRange{Min: .5, Max: 1}},
		MagicalReinforcement:  enterworld.ItemAttackRange{Minimum: enterworld.ItemStatRange{Min: 1.5, Max: 2}},
	}}
	for _, tc := range []struct {
		bits              uint64
		physical, magical float32
	}{
		{0, 50, 150}, {31 << 5, 100, 150}, {31 << 10, 50, 200},
	} {
		writes := equipmentReinforcementWrites(ref, tc.bits)
		if writes[0].Value != tc.physical || writes[2].Value != tc.magical {
			t.Fatalf("variance %#x: %+v", tc.bits, writes)
		}
	}
}

/*
================
TestReportedSpearUsesPublishedReinforcement

Pin BUG-041's named weapon through the production text reader, including the
float32 permille conversion omitted by the old typed combat reference.
================
*/
func TestReportedSpearUsesPublishedReinforcement(t *testing.T) {
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	ref, ok := items.ItemRefByCodename("ITEM_CH_SPEAR_06_C")
	if !ok || ref.Combat == nil {
		t.Fatal("reported spear is missing its combat reference")
	}
	c := reinforcementCharacter(ref, 6)
	c.Level = pointer(50)
	c.Strength = pointer(69)
	c.Intellect = pointer(216)
	c.MissionInventory[0].Plus = 30
	stats, _, err := PlayerStats(c.Snapshot(), Catalogs{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	for parameter, expected := range map[uint16]float32{34: float32(float64(float32(.816)) * 100), 36: float32(float64(float32(.971)) * 100), 35: float32(float64(float32(1.399)) * 100), 37: float32(float64(float32(1.71)) * 100)} {
		got, exists := stats.Param(parameter)
		if !exists || got != expected {
			t.Fatalf("spear parameter %d=%g/%v, want %g", parameter, got, exists, expected)
		}
	}
	t.Logf("reported +30 spear: physical %.3f..%.3f, magical %.3f..%.3f", stats.PhysicalAttackMin, stats.PhysicalAttackMax, stats.MagicalAttackMin, stats.MagicalAttackMax)
}

/*
================
TestArmorReinforcementComposesAcrossEquipmentAndEffects

A complete set replaces all six base piece coefficients; the shield remains
an independent input. Snapshot order cannot make one piece own the whole set.
================
*/
func TestArmorReinforcementComposesAcrossEquipmentAndEffects(t *testing.T) {
	c := &domain.Character{Level: pointer(1), Strength: pointer(100), Intellect: pointer(100)}
	refs := itemRefs{}
	for part := int64(1); part <= 7; part++ {
		family, kind, slot := int64(3), part, part-1
		if part == 7 {
			family, kind, slot = 4, 1, 7
		}
		ref := &enterworld.ItemRef{RefObjID: uint32(part), Codename: fmt.Sprintf("PART_%d", part), TypeIDs: [4]int64{3, 1, family, kind}, Combat: &enterworld.ItemCombatRef{
			PhysicalDefenseReinforcement: enterworld.ItemStatRange{Min: .125, Max: .125},
			MagicalDefenseReinforcement:  enterworld.ItemStatRange{Min: .25, Max: .25},
		}}
		refs[ref.Codename] = ref
		c.MissionInventory = append(c.MissionInventory, reinforcementCharacter(ref, slot).MissionInventory[0])
	}
	for order := 0; order < 2; order++ {
		stats, _, err := PlayerStatsWithModifiers(c.Snapshot(), Catalogs{Items: refs}, []paramkeeper.Write{{Parameter: 5, Source: 4096, Value: 50}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if stats.PhysicalDefense != 137.5 || stats.MagicalDefense != 175 {
			t.Fatalf("order %d defenses %g/%g, want 137.5/175", order, stats.PhysicalDefense, stats.MagicalDefense)
		}
		for left, right := 0, len(c.MissionInventory)-1; left < right; left, right = left+1, right-1 {
			c.MissionInventory[left], c.MissionInventory[right] = c.MissionInventory[right], c.MissionInventory[left]
		}
	}
}
