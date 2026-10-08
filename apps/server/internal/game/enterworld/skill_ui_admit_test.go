/*
===========================================================================

skill_ui_admit_test.go - published skill admission inputs for client prediction

Named shipped rows pin the projection independently of its mapping logic.

===========================================================================
*/
package enterworld

import (
	"reflect"
	"testing"
)

/*
================
TestSkillUiAdmitMirrorsTheServerGates

Every player row publishes its 58D8F0 inputs and monster rows none. Named
shipped rows pin what each gate publishes, so a wrong mapping fails here
rather than mispredicting a press (BUG-066, BR-261007-0624). No shipped
player row carries nmf, the low-HP reqc bit or the stealth-strike bit;
press-admission.test.mjs covers those gates on the client.
================
*/
func TestSkillUiAdmitMirrorsTheServerGates(t *testing.T) {
	skills := sharedShippedSkills(t)
	byCodename := map[string]*SkillUiAdmit{}
	for _, row := range skills.SpawnSkillRows() {
		source, ok := skills.SkillByID(row.ID)
		if !ok || row.UI == nil {
			continue
		}
		admit := row.UI.Admit
		if !playerSkillCodename(source.Codename) {
			if admit != nil {
				t.Fatalf("monster skill %d (%s) published admission inputs", row.ID, source.Codename)
			}
			continue
		}
		if admit == nil {
			t.Fatalf("player skill %d (%s) published no admission inputs", row.ID, source.Codename)
		}
		byCodename[source.Codename] = admit
	}
	anyWeapon := [2]uint8{0xff, 0xff}
	for codename, want := range map[string]SkillUiAdmit{
		// reqc bit 0 wants a knocked-down target, which only the server sees.
		"SKILL_CH_SWORD_DOWNATTACK_A_01": {ServerOnly: true, WeaponKinds: [2]uint8{2, 3}},
		// A bow row needs arrows in the shield socket.
		"SKILL_CH_BOW_BASE_01": {WeaponKinds: [2]uint8{6, 0xff}, Ammunition: true},
		// tel3 is refused while rooted.
		"SKILL_CH_LIGHTNING_GYEONGGONG_B_01": {Teleports: true, WeaponKinds: anyWeapon},
		// A shield (reqi kind 4, TID4 1) in the secondary socket.
		"SKILL_CH_SWORD_SHIELD_A_01": {WeaponKinds: anyWeapon, Reqi: &SkillUiReqi{Pairs: [][2]uint32{{4, 1}}}},
		// 10 % of maximum HP, with any of three primary weapons.
		"SKILL_EU_WARRIOR_FRENZYA_TOUNT_AREA_A_01": {
			WeaponKinds: anyWeapon, HPPercent: 10,
			Reqi: &SkillUiReqi{Pairs: [][2]uint32{{6, 7}, {6, 8}, {6, 9}}},
		},
		// A hide gate without a trap is refused to a berserk caster; stealth
		// (hide mode 1) is also refused in battle, which only the server sees.
		"SKILL_EU_ROG_STEALTHA_HIDING_A_01": {
			ServerOnly: true, Berserk: true, WeaponKinds: [2]uint8{13, 12},
			Reqi: &SkillUiReqi{Pairs: [][2]uint32{{6, 12}, {6, 13}}},
		},
	} {
		got, ok := byCodename[codename]
		if !ok {
			t.Fatalf("%s published no admission inputs", codename)
		}
		if !reflect.DeepEqual(*got, want) {
			t.Fatalf("%s admission inputs = %+v (reqi %+v), want %+v (reqi %+v)", codename, *got, got.Reqi, want, want.Reqi)
		}
	}
}
