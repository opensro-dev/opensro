/*
===========================================================================

creatable_monsters_test.go - the bake roster is every creatable monster

The GameWorld's populated set (nests and the unique summon closure) and
every script spawn must be inside the roster the bake reads, or the client
admits an entity it cannot draw (#369).

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestCreatableRosterHoldsEveryRuntimeMonster
================
*/
func TestCreatableRosterHoldsEveryRuntimeMonster(t *testing.T) {
	licensed.RequireGameData(t)
	dir, err := gamedata.ResolveTextdataDir()
	if err != nil {
		t.Fatal(err)
	}
	template := monster.LoadTemplate(dir)
	creatable := map[uint32]bool{}
	for _, ref := range template.CreatableRefs() {
		if ref.Structure {
			t.Fatalf("fortress structure %s in the monster roster", ref.Codename)
		}
		creatable[ref.RefObjID] = true
	}
	withSummons, err := enterworld.WithMonsterSummonReferences(template, enterworld.NewTextdataSkills(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range withSummons.SpawnableRefs() {
		if !creatable[ref.RefObjID] {
			t.Fatalf("populated monster %s is not creatable", ref.Codename)
		}
	}
	// Script spawns and GM-only rows: quest guardians, STRONG and level variants.
	byName := map[string]bool{}
	for _, ref := range template.CreatableRefs() {
		byName[ref.Codename] = true
	}
	for _, codename := range []string{"MOB_QT_01_ONG", "MOB_QT_02_PUNISHER_CLON", "MOB_QT_01_LADON", "MOB_CH_STRONG_TIGER", "MOB_CH_TIGERWOMAN_L2"} {
		if !byName[codename] {
			t.Fatalf("%s is not in the creatable roster", codename)
		}
	}
}
