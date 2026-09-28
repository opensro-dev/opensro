/*
===========================================================================

equipmentresources_test.go - equipment resource lifetime and composition

Exercise the public combat projection so a bonus cannot work only in an
equipment-change handler while login, breakage or effect snapshots omit it.

===========================================================================
*/

package combat

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestEquipmentResourceFamilyAndLifetime

Native's intrinsic MP contribution belongs only to usable, equipped harps.
Reconstruct the snapshot after each transition, as login and actions do.
================
*/
func TestEquipmentResourceFamilyAndLifetime(t *testing.T) {
	const weaponSlot = 6
	const firstBagSlot = 13
	const nativeWeaponSubclasses = 15
	for kind := int64(1); kind <= nativeWeaponSubclasses; kind++ {
		t.Run(fmt.Sprintf("weapon-%d", kind), func(t *testing.T) {
			ref := &enterworld.ItemRef{
				RefObjID: 1, Codename: "CATALOG_WEAPON", TypeIDs: [4]int64{3, 1, 6, kind},
				Combat: &enterworld.ItemCombatRef{},
			}
			catalogs := Catalogs{Items: itemRefs{ref.Codename: ref}}
			character := &domain.Character{
				Level: pointer(1), Strength: pointer(20), Intellect: pointer(20),
				MissionInventory: []domain.InventoryRow{{
					Slot: weaponSlot, RefObjID: ref.RefObjID, Codename: ref.Codename,
					TypeFlags: ref.TypeFlags(), VarianceBits: "0", Durability: 1,
				}},
			}
			wantMP := uint32(200)
			if kind == 14 {
				wantMP = 300
			}
			for _, state := range []struct {
				name       string
				slot       int64
				durability int64
				maximumMP  uint32
			}{
				{"equipped", weaponSlot, 1, wantMP},
				{"broken", weaponSlot, 0, 200},
				{"repaired", weaponSlot, 1, wantMP},
				{"bag", firstBagSlot, 1, 200},
				{"reequipped", weaponSlot, 1, wantMP},
			} {
				character.MissionInventory[0].Slot = state.slot
				character.MissionInventory[0].Durability = state.durability
				stats, err := PlayerBaseStats(character.Snapshot(), catalogs)
				if err != nil {
					t.Fatalf("%s: %v", state.name, err)
				}
				if stats.MaxMP != state.maximumMP || stats.MaxHP != 200 {
					t.Fatalf("%s: HP/MP %d/%d, want 200/%d", state.name, stats.MaxHP, stats.MaxMP, state.maximumMP)
				}
			}
		})
	}
}

/*
================
TestEquipmentResourceComposesWithIndependentModifiers

A second percentage source adds to the harp percentage before scaling the
flat MP total. Repeated snapshots must not accumulate the same item twice.
================
*/
func TestEquipmentResourceComposesWithIndependentModifiers(t *testing.T) {
	ref := &enterworld.ItemRef{
		RefObjID: 1, Codename: "CATALOG_HARP", TypeIDs: [4]int64{3, 1, 6, 14},
		Combat: &enterworld.ItemCombatRef{},
	}
	character := &domain.Character{
		Level: pointer(1), Strength: pointer(20), Intellect: pointer(20),
		MissionInventory: []domain.InventoryRow{{
			Slot: 6, RefObjID: 1, Codename: ref.Codename, VarianceBits: "0", Durability: 1,
		}},
	}
	catalogs := Catalogs{Items: itemRefs{ref.Codename: ref}}
	modifiers := []paramkeeper.Write{
		{Parameter: 4, Source: 4096, Value: 100},
		{Parameter: 4, Source: 4097, Channel: paramkeeper.PercentSum, Value: 25},
	}
	for attempt := 0; attempt < 3; attempt++ {
		stats, err := PlayerBaseStatsWithModifiers(character.Snapshot(), catalogs, modifiers, nil)
		if err != nil {
			t.Fatal(err)
		}
		if stats.MaxMP != 525 {
			t.Fatalf("snapshot %d: max MP %d, want (200+100)*1.75=525", attempt, stats.MaxMP)
		}
	}
}
