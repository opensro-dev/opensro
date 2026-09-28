/*
===========================================================================

levelrecovery_test.go - installed recovery modifiers at the level boundary.

Exercises the production adapter with the actual effect registry so the
progression hook cannot silently drop Panic or Combustion contributions.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestLevelRecoveryUsesInstalledReductions

Distinct HP and MP reductions expose swapped parameters and an unconditional
full-heal implementation. Another division must not inherit these effects.
================
*/
func TestLevelRecoveryUsesInstalledReductions(t *testing.T) {
	runtime, _, character, _ := newCombatTestRuntime(t, 100000)
	maximum, err := runtime.PlayerBaseStats(testDivision, character)
	if err != nil {
		t.Fatal(err)
	}
	modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{
		{Parameter: 0x8f, Value: 50},
		{Parameter: 0x90, Value: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.effects.Apply(statuseffect.Effect{
		DivisionID: testDivision, CharacterName: character.Name,
		SkillID: 99, SkillGroup: 99, InstanceToken: 900, Modifiers: modifiers,
	}) {
		t.Fatal("could not install recovery reductions")
	}
	character.CurrentHP, character.CurrentMP = testInt64(17), testInt64(9)
	if err := runtime.RecoverLevelVitals(testDivision, character); err != nil {
		t.Fatal(err)
	}
	wantHP := int64(17) + (int64(maximum.MaxHP)-17)/2
	if *character.CurrentHP != wantHP || *character.CurrentMP != 9 {
		t.Fatalf("reduced recovery = %d/%d, want %d/9", *character.CurrentHP, *character.CurrentMP, wantHP)
	}
	if err := runtime.RecoverLevelVitals("another-division", character); err != nil {
		t.Fatal(err)
	}
	if *character.CurrentHP != int64(maximum.MaxHP) || *character.CurrentMP != int64(maximum.MaxMP) {
		t.Fatal("recovery reduction escaped its owning division")
	}
	character.CurrentHP = testInt64(0)
	character.CurrentMP = testInt64(9)
	if err := runtime.RecoverLevelVitals(testDivision, character); err != nil {
		t.Fatal(err)
	}
	if *character.CurrentHP != 0 || *character.CurrentMP != 9 {
		t.Fatal("level recovery revived a dead character")
	}
}
