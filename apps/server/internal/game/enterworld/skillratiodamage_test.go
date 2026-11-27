/*
===========================================================================

skillratiodamage_test.go - the shipped pdm2 rows and their standing areas

The pdm2 word exists in the shipped table only on the fortress
structure skills: percent by tier (towers 5/8/11/14, the fort stone
5/9/13/17/22), each with the 5-second pulse and the kind-3 standing
area its pulse works in.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"strings"
	"testing"
)

/*
================
TestShippedRatioDamageRowsAreTheStructureSkills

Every shipped pdm2 row, and no other row: the four tower families and
the fort stone, tier by tier, each carrying the pulse, the wall
bypass and the kind-3 area (towers 250 / 24 victims, the stone
300 / 64), never an attack block.
================
*/
func TestShippedRatioDamageRowsAreTheStructureSkills(t *testing.T) {
	source := sharedShippedSkills(t)
	want := map[string]uint32{}
	for _, family := range []string{"LIGHTNING_TOWER", "BLAZE_TOWER", "FREEZING_TOWER", "DARK_TOWER"} {
		for tier, percent := range []uint32{5, 8, 11, 14} {
			want[fmt.Sprintf("SKILL_FORT_STRUCTURE_%s_%02d", family, tier+2)] = percent
		}
	}
	for tier, percent := range []uint32{5, 9, 13, 17, 22} {
		want[fmt.Sprintf("SKILL_FORT_STRUCTURE_FORT_STONE_%02d", tier+1)] = percent
	}
	found := map[string]uint32{}
	for id := uint32(1); id < 45000; id++ {
		row, ok := source.SkillByID(id)
		if !ok || !row.RatioDamage.Present {
			continue
		}
		found[row.Codename] = row.RatioDamage.Percent
		if !row.RatioDamage.Present || row.Attack.Present || !row.WallBypass ||
			!row.Abnormal.PulsePresent || row.Abnormal.Pulse != 5000 {
			t.Fatalf("%s: ratio row shape %+v pulse %d/%v wall %v", row.Codename, row.RatioDamage,
				row.Abnormal.Pulse, row.Abnormal.PulsePresent, row.WallBypass)
		}
		area := row.Abnormal.PersistentArea
		wantRadius, wantMax := uint32(250), uint32(24)
		switch {
		case strings.HasPrefix(row.Codename, "SKILL_FORT_STRUCTURE_FORT_STONE"):
			wantRadius, wantMax = 300, 64
		case strings.HasPrefix(row.Codename, "SKILL_FORT_STRUCTURE_FREEZING_TOWER"):
			wantMax = 18 // the freezing family caps its standing area lower
		}
		if !area.Present || area.Kind != 3 || area.Shape != 1 || area.Radius != wantRadius ||
			area.MaxTargets != wantMax || area.Reduction != 0 || area.Select != 24 {
			t.Fatalf("%s: standing area %+v", row.Codename, area)
		}
	}
	if len(found) != len(want) {
		t.Fatalf("pdm2 rows = %v, want %v", found, want)
	}
	for code, percent := range want {
		if found[code] != percent {
			t.Fatalf("%s percent = %d, want %d (all %v)", code, found[code], percent, found)
		}
	}
}

/*
================
TestShippedStructureAuraRowsCarryTheStandingArea

The holder-side structure skills (no pdm2, no pulse) author the same
kind-3 slot for their aura reach, with the ally select word: the flag
and the defensive position at 250, the headquarters at 330.
================
*/
func TestShippedStructureAuraRowsCarryTheStandingArea(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, tc := range []struct {
		code   string
		radius uint32
	}{
		{"SKILL_FORT_STRUCTURE_AGGRESSIVE_FLAG", 250},
		{"SKILL_FORT_STRUCTURE_DEFENSIVE_POSITION_05", 250},
		{"SKILL_FORT_STRUCTURE_HEADQUARTERS", 330},
	} {
		row, ok := source.SkillByCodename(tc.code)
		if !ok {
			t.Fatalf("missing %s", tc.code)
		}
		area := row.Abnormal.PersistentArea
		if row.RatioDamage.Present || row.Abnormal.PulsePresent ||
			!area.Present || area.Kind != 3 || area.Radius != tc.radius || area.Select != 32 {
			t.Fatalf("%s: ratio %v pulse %v area %+v", tc.code, row.RatioDamage, row.Abnormal.PulsePresent, area)
		}
	}
}
