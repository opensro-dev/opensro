/*
===========================================================================

petgear_test.go - the pet gear derived drop, on and off

The expansion is port-only: while SRO_ATTACK_PET_EXPANSION is off, no item
carries a pet gear source and droptables.json carries no rule; while it is
on, every pet gear piece of the shipped bundle names the degree it drops
with and the factor's share of those drops.

===========================================================================
*/
package catalogue

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
petGearRefs

One normal collar, one sealed collar and one plain weapon.
================
*/
func petGearRefs() []*enterworld.ItemRef {
	return []*enterworld.ItemRef{
		{RefObjID: 30101, Codename: "ITEM_COS_PET_COLLAR_01", Name: "Rawhide Collar"},
		{RefObjID: 30121, Codename: "ITEM_COS_PET_COLLAR_01_MOON", Name: "Moon Rawhide Collar"},
		{RefObjID: 1, Codename: "ITEM_CH_MACE_01", Name: "Mace"},
	}
}

/*
================
itemByID
================
*/
func itemByID(items []Item, id uint32) *Item {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

/*
================
TestPetGearSourcesFollowTheExpansion
================
*/
func TestPetGearSourcesFollowTheExpansion(t *testing.T) {
	t.Setenv(companion.EnvAttackPetExpansion, "on")
	items := BuildItems(ItemSource{Refs: petGearRefs(), PetGearFactor: 0.12})
	collar := itemByID(items, 30101)
	if collar == nil || collar.PetGear == nil {
		t.Fatalf("collar = %+v, want a pet gear source", collar)
	}
	if collar.PetGear.Degree != 1 || collar.PetGear.Seal != "" {
		t.Errorf("collar source = %+v, want degree 1 unsealed", collar.PetGear)
	}
	if math.Abs(collar.PetGear.Chance-0.04) > 1e-9 {
		t.Errorf("collar chance = %v, want 0.04 (0.12 over the degree's 3 pieces)", collar.PetGear.Chance)
	}
	sealed := itemByID(items, 30121)
	if sealed == nil || sealed.PetGear == nil {
		t.Fatalf("sealed collar = %+v, want a pet gear source", sealed)
	}
	if sealed.PetGear.Degree != 1 || sealed.PetGear.Seal != "moon" {
		t.Errorf("sealed source = %+v, want degree 1 moon", sealed.PetGear)
	}
	if mace := itemByID(items, 1); mace == nil || mace.PetGear != nil {
		t.Errorf("mace = %+v, want no pet gear source", mace)
	}
}

/*
================
TestPetGearSourcesStayOffWithTheExpansion
================
*/
func TestPetGearSourcesStayOffWithTheExpansion(t *testing.T) {
	t.Setenv(companion.EnvAttackPetExpansion, "off")
	items := BuildItems(ItemSource{Refs: petGearRefs(), PetGearFactor: 0.12})
	for _, item := range items {
		if item.PetGear != nil {
			t.Errorf("item %d carries %v with the expansion off", item.ID, item.PetGear)
		}
	}

	t.Setenv(companion.EnvAttackPetExpansion, "on")
	items = BuildItems(ItemSource{Refs: petGearRefs(), PetGearFactor: 0})
	for _, item := range items {
		if item.PetGear != nil {
			t.Errorf("item %d carries %v at a zero factor", item.ID, item.PetGear)
		}
	}
}

/*
================
TestPetGearRule
================
*/
func TestPetGearRule(t *testing.T) {
	t.Setenv(companion.EnvAttackPetExpansion, "off")
	if rule := BuildPetGearRule(0.12, func(string) uint32 { return 1 }); rule != nil {
		t.Fatalf("rule = %+v with the expansion off, want nil", rule)
	}

	t.Setenv(companion.EnvAttackPetExpansion, "on")
	if rule := BuildPetGearRule(0, func(string) uint32 { return 1 }); rule != nil {
		t.Fatalf("rule = %+v at a zero factor, want nil", rule)
	}

	// Every design codename resolves to its design id but one: that piece
	// leaves its degree.
	missing := ""
	if degree := companion.D.Gear[len(companion.D.Gear)-1]; len(degree.Items) > 0 {
		missing = degree.Items[0].Codename
	}
	rule := BuildPetGearRule(0.12, func(codename string) uint32 {
		if codename == missing {
			return 0
		}
		for _, degree := range companion.D.Gear {
			for _, item := range degree.Items {
				if item.Codename == codename {
					return item.ID
				}
			}
		}
		return 0
	})
	if rule == nil {
		t.Fatal("rule is nil with the expansion on")
	}
	if rule.Factor != 0.12 || len(rule.Degrees) != len(companion.D.Gear) {
		t.Fatalf("rule = %+v, want factor 0.12 over %d degrees", rule, len(companion.D.Gear))
	}
	for _, want := range companion.D.Gear {
		row := rule.Degrees[want.Degree-1]
		if row.Degree != want.Degree {
			t.Fatalf("row = %+v, want degree %d", row, want.Degree)
		}
		wantItems := len(want.Items)
		for _, item := range want.Items {
			if item.Codename == missing {
				wantItems--
			}
		}
		if len(row.Items) != wantItems {
			t.Errorf("degree %d carries %d pieces, want %d", want.Degree, len(row.Items), wantItems)
		}
		for _, id := range row.Items {
			if degree, _, ok := companion.GearByID(id); !ok || degree.Degree != want.Degree {
				t.Errorf("degree %d lists id %d, which resolves to degree %v", want.Degree, id, degree.Degree)
			}
		}
	}
}

/*
================
TestShippedPetGearRowsCarryTheirDerivedSource
================
*/
func TestShippedPetGearRowsCarryTheirDerivedSource(t *testing.T) {
	tier.Long(t, "shipped pet gear rows carry their derived source")
	t.Setenv(companion.EnvAttackPetExpansion, "on")
	dir := gamedatatest.TextdataDir(t)
	source := enterworld.NewTextdataItems(dir)
	var refs []*enterworld.ItemRef
	for _, command := range source.ItemCommandReferences() {
		if ref, ok := source.ItemRefByCodename(command.Codename); ok {
			refs = append(refs, ref)
		}
	}
	items := BuildItems(ItemSource{Refs: refs, Usable: action.ItemUseSupported, PetGearFactor: 0.3})
	byID := map[uint32]*Item{}
	sourced := 0
	for i := range items {
		byID[items[i].ID] = &items[i]
		if items[i].PetGear != nil {
			sourced++
			if items[i].ID < 30100 || items[i].ID > 30399 {
				t.Errorf("item %d outside the pet gear range carries %+v", items[i].ID, items[i].PetGear)
			}
		}
	}
	// Every design piece and its three sealed variants is a shipped row,
	// and every shipped row names its degree and tier.
	want := 0
	for _, degree := range companion.D.Gear {
		for _, piece := range degree.Items {
			for tierIndex := 0; tierIndex <= 3; tierIndex++ {
				want++
				id := piece.ID + uint32(tierIndex*10)
				row := byID[id]
				if row == nil || row.PetGear == nil {
					t.Errorf("item %d has no row with a pet gear source", id)
					continue
				}
				if row.PetGear.Degree != degree.Degree {
					t.Errorf("item %d names degree %d, want %d", id, row.PetGear.Degree, degree.Degree)
				}
				seal := ""
				if tierIndex > 0 {
					seal = sealNames[tierIndex-1]
				}
				if row.PetGear.Seal != seal {
					t.Errorf("item %d names seal %q, want %q", id, row.PetGear.Seal, seal)
				}
				if math.Abs(row.PetGear.Chance-0.1) > 1e-9 {
					t.Errorf("item %d chance = %v, want 0.1 (0.3 over 3 pieces)", id, row.PetGear.Chance)
				}
			}
		}
	}
	if sourced != want {
		t.Errorf("%d items carry a pet gear source, want %d", sourced, want)
	}
}
