/*
===========================================================================

questmonster_test.go - scripted guardian population and lifecycle boundaries

Quest actors use the normal spawn-ground and population owners. Failed or
stale admission must not allocate identities or leak actors into another world.

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestQuestGuardianUsesExactPopulationAndDoesNotRespawn
================
*/
func TestQuestGuardianUsesExactPopulationAndDoesNotRespawn(t *testing.T) {
	const code = "MOB_QT_02_PUNISHER_CLON"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: code, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("quest")
	lease, exists := state.PopulationLease("quest", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	request := QuestMonsterSpawn{Division: "quest", Population: lease, Codename: code,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000}
	stale := request
	stale.Population.Generation++
	if state.SpawnQuestGuardian(stale) {
		t.Fatal("stale generation spawned a guardian")
	}
	if !state.SpawnQuestGuardian(request) {
		t.Fatal("valid guardian admission failed")
	}
	actors := state.MaterializedInstances("quest")
	if len(actors) != 1 || !actors[0].NestDetached || actors[0].CurrentHP != 100 || actors[0].Spawn.X != 253 {
		t.Fatal("guardian lost its reference, native pose or detached lifetime", actors)
	}
	if !state.Defeat("quest", actors[0].Gid, time.UnixMilli(1001)) {
		t.Fatal("guardian could not retire through the ordinary population owner")
	}
	state.AdvancePopulation(1000000)
	if len(state.InstancesInRegions("quest", []uint16{0x655e})) != 0 {
		t.Fatal("script guardian acquired a respawning nest")
	}
	request.Position.RegionID |= 0x8000
	if state.SpawnQuestGuardian(request) {
		t.Fatal("indoor guardian admitted")
	}
}
