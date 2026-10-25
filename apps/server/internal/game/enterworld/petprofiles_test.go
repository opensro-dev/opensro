/*
===========================================================================

petprofiles_test.go - explicit expansion catalog profiles preserve native off

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/companion"
)

/*
================
TestPetProfilesOnlyChangeAttackPetsAfterExplicitApplication
================
*/
func TestPetProfilesOnlyChangeAttackPetsAfterExplicitApplication(t *testing.T) {
	bear := &CharacterRef{Codename: "COS_P_BEAR_040", Level: 40, TidWord: 3 << 11, MaxHP: 1000}
	bear.Parameters.PhysicalDefense, bear.Parameters.MagicalDefense = 100, 100
	horse := &CharacterRef{Codename: "COS_T_HORSE1", TidWord: 1 << 11, MaxHP: 1000}
	items := &TextdataItems{charactersByCodename: map[string]*CharacterRef{bear.Codename: bear, horse.Codename: horse}, summonableCharacters: []CharacterRef{*bear, *horse}}
	items.once.Do(func() {})
	if bear.MaxHP != 1000 {
		t.Fatal("native catalog changed before opt-in")
	}
	items.ApplyAttackPetProfiles()
	if bear.MaxHP != 390 || bear.Parameters.MaxHP != 390 || bear.Parameters.PhysicalDefense != 100 || bear.Parameters.MagicalDefense != 100 || items.summonableCharacters[0].MaxHP != 390 || horse.MaxHP != 1000 {
		t.Fatal("profile did not consistently update attack-pet references only")
	}
}

/*
================
TestPetExpansionGateKeepsNativeHealthOff

Startup goes through ApplyAttackPetExpansion: every value but "on" leaves
the bear's native MaxHP, and "on" applies the profile.
================
*/
func TestPetExpansionGateKeepsNativeHealthOff(t *testing.T) {
	catalog := func() (*TextdataItems, *CharacterRef) {
		bear := &CharacterRef{Codename: "COS_P_BEAR_040", Level: 40, TidWord: 3 << 11, MaxHP: 1000}
		items := &TextdataItems{charactersByCodename: map[string]*CharacterRef{bear.Codename: bear}, summonableCharacters: []CharacterRef{*bear}}
		items.once.Do(func() {})
		return items, bear
	}
	for _, value := range []string{"", "off", "true", "1"} {
		t.Setenv(companion.EnvAttackPetExpansion, value)
		items, bear := catalog()
		if items.ApplyAttackPetExpansion() || bear.MaxHP != 1000 || items.summonableCharacters[0].MaxHP != 1000 {
			t.Fatalf("%q changed the native catalog: MaxHP %d", value, bear.MaxHP)
		}
	}
	t.Setenv(companion.EnvAttackPetExpansion, "on")
	items, bear := catalog()
	if !items.ApplyAttackPetExpansion() || bear.MaxHP != 390 {
		t.Fatalf("explicit opt-in left MaxHP %d, want 390", bear.MaxHP)
	}
}
