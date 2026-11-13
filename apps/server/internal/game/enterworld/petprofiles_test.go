/*
===========================================================================

petprofiles_test.go - explicit expansion catalog profiles preserve native off

===========================================================================
*/
package enterworld

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
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
	if bear.MaxHP != companion.BaseHealth(bear.Codename, bear.Level, 1000) || bear.Parameters.PhysicalDefense != 100 || bear.Parameters.MagicalDefense != 100 || items.summonableCharacters[0].MaxHP != bear.MaxHP || horse.MaxHP != 1000 {
		t.Fatal("profile missed authored health or changed transport health")
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
	if !items.ApplyAttackPetExpansion() || bear.MaxHP != companion.BaseHealth(bear.Codename, bear.Level, 1000) {
		t.Fatalf("explicit opt-in left MaxHP %d, want authored health", bear.MaxHP)
	}
}

/*
================
TestShippedAttackPetHealthNativeOffAndAuthoredOn

Native rows stay untouched without opt-in; enabled rows use the same curve
for summon, level advance, healing and combat.
================
*/
func TestShippedAttackPetHealthNativeOffAndAuthoredOn(t *testing.T) {
	clientdata.RequireGameData(t)
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	items.once.Do(items.load)
	want := map[uint8]uint32{1: 360, 50: 10260, 90: 39434}
	// One loader, two passes: native first, then the expansion projection.
	for pass := 0; pass < 2; pass++ {
		for _, species := range []string{"COS_P_BEAR", "COS_P_WOLF", "COS_P_PENGUIN", "COS_P_RAVEN"} {
			for level, hp := range want {
				codename := fmt.Sprintf("%s_%03d", species, level)
				ref := items.charactersByCodename[codename]
				if ref == nil {
					t.Fatalf("pass %d: %s missing from shipped characterdata", pass, codename)
				}
				if pass == 1 {
					hp = companion.BaseHealth(codename, level, hp)
				}
				if ref.Level != level || ref.MaxHP != hp {
					t.Fatalf("pass %d: %s is level %d with MaxHP %d, want level %d with the native %d", pass, codename, ref.Level, ref.MaxHP, level, hp)
				}
			}
		}
		items.ApplyAttackPetProfiles()
	}
}
