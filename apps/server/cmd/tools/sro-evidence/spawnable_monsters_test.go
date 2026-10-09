/*
===========================================================================

spawnable_monsters_test.go - the bake roster carries the summon closure

The unique encounters' _L2/_L3 variants have no npcpos anchor, yet the
GameWorld seeds them into the refObjSnapshot; the roster the bake reads
must hold them too, or the client admits an entity it cannot draw (#369).

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
TestSpawnableRosterHoldsTheSummonClosure
================
*/
func TestSpawnableRosterHoldsTheSummonClosure(t *testing.T) {
	licensed.RequireGameData(t)
	dir, err := gamedata.ResolveTextdataDir()
	if err != nil {
		t.Fatal(err)
	}
	refs, err := spawnableMonsterRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	exported := map[uint32]string{}
	for _, ref := range refs {
		exported[ref.RefObjID] = ref.Codename
	}
	template, err := enterworld.WithMonsterSummonReferences(monster.LoadTemplate(dir), enterworld.NewTextdataSkills(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(template.SummonRefs) == 0 {
		t.Fatal("the shipped data has no unique summon closure")
	}
	for _, id := range template.SummonRefs {
		if _, ok := exported[id]; !ok {
			t.Fatalf("summon reference %d (%s) is not in the bake roster", id, template.Refs[id].Codename)
		}
	}
	for _, codename := range []string{"MOB_CH_TIGERWOMAN_L2", "MOB_CH_TIGERWOMAN_L3", "MOB_KK_ISYUTARU_L3", "MOB_AM_IVY_L3"} {
		found := false
		for _, name := range exported {
			found = found || name == codename
		}
		if !found {
			t.Fatalf("%s is not in the bake roster", codename)
		}
	}
	for _, ref := range template.SpawnableRefs() {
		if _, ok := exported[ref.RefObjID]; !ok {
			t.Fatalf("runtime reference %s is not in the bake roster", ref.Codename)
		}
	}
	if len(exported) != len(template.SpawnableRefs()) {
		t.Fatalf("bake roster %d references, runtime %d", len(exported), len(template.SpawnableRefs()))
	}
}
