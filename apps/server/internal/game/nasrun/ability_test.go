/*
===========================================================================

ability_test.go - tests for ability.go

The expected rows are the iSRO-R shard's (2025-07)
_RefAbilityByItemOptLevel joined to its skill and magic-option tables by
codename, read for #3456.

===========================================================================
*/
package nasrun

import (
	"reflect"
	"testing"
)

/*
================
TestSpiritAbilityFollowsTheOfficialTiers
================
*/
func TestSpiritAbilityFollowsTheOfficialTiers(t *testing.T) {
	for _, tc := range []struct {
		codename string
		plus     int64
		want     Ability
	}{
		{"ITEM_MALL_AVATAR_M_NASRUN", 0, Ability{[]string{"SKILL_MALL_NASRUN_ECCENTRICDEMON_01"}, 15, 15, 0}},
		{"ITEM_MALL_AVATAR_W_NASRUN", 2, Ability{[]string{"SKILL_MALL_NASRUN_ECCENTRICDEMON_01"}, 17, 17, 0}},
		{"ITEM_MALL_AVATAR_M_NASRUN", 3, Ability{[]string{"SKILL_MALL_NASRUN_THEDEVILCONFUSION_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01"}, 18, 18, 1}},
		{"ITEM_MALL_AVATAR_M_NASRUN_YELLOW", 5, Ability{[]string{"SKILL_MALL_NASRUN_THEDEVILCONFUSION_YELLOW_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01"}, 20, 20, 1}},
		{"ITEM_MALL_AVATAR_W_NASRUN_BLUE", 6, Ability{[]string{"SKILL_MALL_NASRUN_DESTROYTHEDEVIL_BLUE_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02"}, 21, 21, 2}},
		{"ITEM_MALL_AVATAR_M_NASRUN_BLUE", 0, Ability{[]string{"SKILL_MALL_NASRUN_ECCENTRICDEMON_BLUE_01"}, 15, 15, 0}},
		{"ITEM_MALL_AVATAR_M_NASRUN_YELLOW", 10, Ability{[]string{"SKILL_MALL_NASRUN_DESTROYTHEDEVIL_YELLOW_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02"}, 25, 25, 2}},
		// The Unique: the red skills, five more percent, one more block.
		{"ITEM_MALL_AVATAR_M_NASRUN_UNIQUE", 0, Ability{[]string{"SKILL_MALL_NASRUN_ECCENTRICDEMON_01"}, 20, 20, 1}},
		{"ITEM_MALL_AVATAR_W_NASRUN_UNIQUE", 4, Ability{[]string{"SKILL_MALL_NASRUN_THEDEVILCONFUSION_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01"}, 24, 24, 2}},
		{"ITEM_MALL_AVATAR_M_NASRUN_UNIQUE", 6, Ability{[]string{"SKILL_MALL_NASRUN_DESTROYTHEDEVIL_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02"}, 26, 26, 3}},
	} {
		got, ok := SpiritAbility(tc.codename, tc.plus)
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s +%d = %+v (%v), want %+v", tc.codename, tc.plus, got, ok, tc.want)
		}
	}
}

/*
================
TestSpiritAbilityRefusesWhatHasNoRow
================
*/
func TestSpiritAbilityRefusesWhatHasNoRow(t *testing.T) {
	for _, tc := range []struct {
		codename string
		plus     int64
	}{
		{"ITEM_MALL_AVATAR_M_NASRUN", -1},
		{"ITEM_MALL_AVATAR_M_NASRUN", MaxAbilityPlus + 1},
		// The event pair and the later Basic/Red copies are not imported.
		{"ITEM_EVENT_AVATAR_M_NASRUN", 0},
		{"ITEM_MALL_AVATAR_M_NASRUN_RED", 0},
		{"ITEM_MALL_NASRUN_EXTENSION", 0},
		{"ITEM_MALL_AVATAR_M_NASRUNX", 0},
	} {
		if got, ok := SpiritAbility(tc.codename, tc.plus); ok {
			t.Errorf("%s +%d granted %+v", tc.codename, tc.plus, got)
		}
	}
}

/*
================
TestAbilitySkillsNamesEveryGrant

Every skill some Spirit grants is in AbilitySkills, which the import
stages.
================
*/
func TestAbilitySkillsNamesEveryGrant(t *testing.T) {
	all := map[string]bool{}
	for _, s := range AbilitySkills() {
		all[s] = true
	}
	if len(all) != 11 {
		t.Fatalf("%d skills, want 11", len(all))
	}
	for _, colour := range []string{"", "_YELLOW", "_BLUE", "_UNIQUE"} {
		for plus := int64(0); plus <= MaxAbilityPlus; plus++ {
			a, _ := SpiritAbility("ITEM_MALL_AVATAR_M_NASRUN"+colour, plus)
			for _, s := range a.Skills {
				if !all[s] {
					t.Fatalf("%s +%d grants %s, not in AbilitySkills", colour, plus, s)
				}
			}
		}
	}
}
