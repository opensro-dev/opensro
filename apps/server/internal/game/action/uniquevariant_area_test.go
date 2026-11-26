/*
===========================================================================

uniquevariant_area_test.go - a refused copy costs no area target slot

Pins the selection half of SRO_STR_INT_UNIQUES: a copy the caster's build
may not damage is dropped where area victims are chosen, so it never takes
the slot of a victim the caster could have hit. The damage half is pinned
by TestImpactPlanningRefusesACopyOfTheOtherBuild.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestAreaSelectionKeepsItsCountWithARefusedCopy

Two ordinary monsters and one STR copy stand in reach, and the skill takes
two targets. A magical caster must get both ordinary monsters: if the copy
were still counted at selection, it would take one slot and leave only one.
================
*/
func TestAreaSelectionKeepsItsCountWithARefusedCopy(t *testing.T) {
	rt, clock, c, primary := newCombatTestRuntime(t, 100_000)
	base := primary.Ref
	copyRef := base
	copyRef.RefObjID = base.RefObjID + 9000
	copyRef.Codename = base.Codename + "_STR"
	copyRef.MonsterType = 3
	copyRef.UniqueBuild = monster.UniqueBuildSTR

	var nests []monster.NestRow
	for _, dx := range []float64{0, 5} {
		spawn := primary.Spawn
		spawn.X += dx
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	copySpawn := primary.Spawn
	copySpawn.X += 8
	copySpawn.RefObjID = copyRef.RefObjID
	nests = append(nests, monster.NestRow{SpawnPoint: copySpawn, RetailEvidence: true, MaxCount: 1})

	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{base.RefObjID: base, copyRef.RefObjID: copyRef}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	targets := rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
	if len(targets) != 3 {
		t.Fatalf("live monsters = %d, want 3", len(targets))
	}

	// A magical caster: the STR copy refuses it, the ordinary monsters do not.
	c.Strength, c.Intellect = testInt64(100), testInt64(2000)
	area := enterworld.SkillOffensiveArea{Shape: 2, Radius: 10, MaxTargets: 2, Select: 24}
	got := areaTestVictims(rt, c, targets[0], area, 0, clock.Now().UnixMilli())
	if len(got) != 2 {
		t.Fatalf("victims = %d, want 2: a refused copy took a target slot", len(got))
	}
	for _, victim := range got {
		if victim.Ref.UniqueBuild != monster.UniqueBuildAny {
			t.Fatal("a refused copy was selected")
		}
	}
}
