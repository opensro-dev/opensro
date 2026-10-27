/*
===========================================================================

cos_companion_test.go - companion lookup order, duplicates and cost (#1820 I)

CompanionByGID walks the records Companions returns without building the
slice. These tests pin what the walk must keep: the canonical order, the
refusal of a GID that two summoned records claim, and no allocation.

===========================================================================
*/
package domain

import "testing"

/*
================
companionTestCharacter

One record in every place a companion can live, each summoned with its own
GID: the active record, two bag summoners, a mercenary, a captured monster
and a brood member.
================
*/
func companionTestCharacter() *Character {
	return &Character{
		ActiveCOS: &CharacterCOS{GID: 1, Summoned: true},
		MissionInventory: []InventoryRow{
			{Slot: 13},
			{Slot: 14, Summon: &CharacterCOS{GID: 2, Summoned: true}},
			{Slot: 15, Summon: &CharacterCOS{GID: 3, Summoned: true}},
		},
		Mercenaries: []*CharacterCOS{nil, {GID: 4, Summoned: true}},
		CapturedCOS: &CharacterCOS{GID: 5, Summoned: true},
		Brood:       []*CharacterCOS{{GID: 6, Summoned: true}},
	}
}

/*
================
TestCompanionsKeepTheCanonicalOrder
================
*/
func TestCompanionsKeepTheCanonicalOrder(t *testing.T) {
	pets := companionTestCharacter().Companions()
	if len(pets) != 6 {
		t.Fatalf("companions = %d, want 6", len(pets))
	}
	for i, pet := range pets {
		if pet.GID != uint32(i+1) {
			t.Fatalf("companion %d has GID %d, want %d", i, pet.GID, i+1)
		}
	}
}

/*
================
TestCompanionByGIDFindsEveryPlaceAndRefusesDuplicates
================
*/
func TestCompanionByGIDFindsEveryPlaceAndRefusesDuplicates(t *testing.T) {
	c := companionTestCharacter()
	for gid := uint32(1); gid <= 6; gid++ {
		if pet := c.CompanionByGID(gid); pet == nil || pet.GID != gid {
			t.Fatalf("GID %d not found", gid)
		}
	}
	if c.CompanionByGID(0) != nil || c.CompanionByGID(7) != nil {
		t.Fatal("GID 0 or an unknown GID resolved")
	}
	// A dormant record keeps its GID but owns no world actor.
	c.Brood[0].Summoned = false
	if c.CompanionByGID(6) != nil {
		t.Fatal("a dormant record resolved")
	}
	// Two summoned records claiming one GID authorize neither.
	c.CapturedCOS.GID = 2
	if c.CompanionByGID(2) != nil {
		t.Fatal("a duplicated GID resolved to one of its records")
	}
}

/*
================
TestCompanionByGIDDoesNotAllocate
================
*/
func TestCompanionByGIDDoesNotAllocate(t *testing.T) {
	c := companionTestCharacter()
	if allocs := testing.AllocsPerRun(100, func() { c.CompanionByGID(6) }); allocs != 0 {
		t.Fatalf("CompanionByGID allocated %.0f times per call", allocs)
	}
}
