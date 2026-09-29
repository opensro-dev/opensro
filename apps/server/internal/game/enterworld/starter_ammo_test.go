/*
===========================================================================

starter_ammo_test.go - starter ammunition and shields are inventory rows

===========================================================================
*/

package enterworld

import "testing"

/*
================
creationCharacter

A created character of the given race, sex and weapon choice, no armor.
================
*/
func creationCharacter(race, gender, weapon int64) *Character {
	model := "CHAR_CH_MAN_ADVENTURER"
	switch {
	case race == RaceEurope && gender == GenderMale:
		model = "CHAR_EU_MAN_ADVENTURER"
	case race == RaceEurope:
		model = "CHAR_EU_WOMAN_ADVENTURER"
	case gender == GenderFemale:
		model = "CHAR_CH_WOMAN_ADVENTURER"
	}
	return &Character{
		RaceIndex:      i64(race),
		Gender:         i64(gender),
		ModelCodename:  model,
		WeaponSelected: true,
		WeaponIndex:    i64(weapon),
	}
}

/*
================
TestStarterRangedAmmunitionIsSeededOnce
================
*/
func TestStarterRangedAmmunitionIsSeededOnce(t *testing.T) {
	for _, tc := range []struct {
		name           string
		race, weapon   int64
		codename, ammo string
		kind           int64
	}{
		{"chinaBow", RaceChina, 5, "ITEM_CH_BOW_01_A_DEF", "ITEM_ETC_AMMO_ARROW_01_DEF", 6},
		{"europeCrossbow", RaceEurope, 5, "ITEM_EU_CROSSBOW_01_A_DEF", "ITEM_ETC_AMMO_BOLT_01_DEF", 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := fakeItems{
				tc.codename: &ItemRef{RefObjID: 100, Codename: tc.codename, TypeIDs: [4]int64{3, 1, 6, tc.kind}},
				tc.ammo:     &ItemRef{RefObjID: 200, Codename: tc.ammo, TypeIDs: [4]int64{3, 3, 4, 1}},
			}
			created := creationCharacter(tc.race, GenderMale, tc.weapon)
			roster := ResolveEquipRoster(created, created.ModelCodename, items, true)
			character := &Character{}
			rows := EnsureMissionInventory(character, roster)
			if len(rows) != 2 || rows[1].Slot != 7 || rows[1].Codename != tc.ammo || rows[1].StackCount != 250 {
				t.Fatalf("starter inventory: %+v", rows)
			}
			character.MissionInventory[1].StackCount = 17
			if got := EnsureMissionInventory(character, roster); len(got) != 2 || got[1].StackCount != 17 {
				t.Fatalf("relogin replenished ammunition: %+v", got)
			}
		})
	}
}

/*
================
TestStarterShieldIsInventoryNotOnlyPreview
================
*/
func TestStarterShieldIsInventoryNotOnlyPreview(t *testing.T) {
	for _, race := range []int64{RaceChina, RaceEurope} {
		for _, gender := range []int64{GenderMale, GenderFemale} {
			prefix := "CH"
			weaponChoice := int64(1)
			if race == RaceEurope {
				prefix = "EU"
				weaponChoice = 2
			}
			weapon := "ITEM_" + prefix + "_SWORD_01_A_DEF"
			shield := "ITEM_" + prefix + "_SHIELD_01_A_DEF"
			items := fakeItems{
				weapon: &ItemRef{RefObjID: 100, Codename: weapon, TypeIDs: [4]int64{3, 1, 6, 2}},
				shield: &ItemRef{RefObjID: 200, Codename: shield, TypeIDs: [4]int64{3, 1, 4, 1}},
			}
			created := creationCharacter(race, gender, weaponChoice)
			roster := ResolveEquipRoster(created, created.ModelCodename, items, true)
			c := &Character{}
			rows := EnsureMissionInventory(c, roster)
			if len(rows) != 2 || rows[1].Slot != 7 || rows[1].Codename != shield || rows[1].StackCount != 1 {
				t.Fatalf("%s/%d: missing starter shield: %+v", prefix, gender, rows)
			}
			c.MissionInventory = c.MissionInventory[:1]
			if rows := EnsureMissionInventory(c, roster); len(rows) != 1 {
				t.Fatalf("%s/%d: removed shield replenished: %+v", prefix, gender, rows)
			}
		}
	}
}
