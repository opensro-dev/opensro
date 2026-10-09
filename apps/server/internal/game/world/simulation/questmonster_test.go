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
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000, RadiusSpan: 20}
	stale := request
	stale.Population.Generation++
	if state.SpawnQuestMonster(stale) {
		t.Fatal("stale generation spawned a guardian")
	}
	if !state.SpawnQuestMonster(request) {
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
	if state.SpawnQuestMonster(request) {
		t.Fatal("indoor guardian admitted")
	}
}

/*
================
TestQuestMonstersLeaveOnTheirTimers

The spawn base arms the Ivy guardian's 300 s, and a quest's own timer
(Hidden Treasure 5's guardian) removes a monster the base never times.
================
*/
func TestQuestMonstersLeaveOnTheirTimers(t *testing.T) {
	const guardian, ong = "MOB_QT_02_PUNISHER_CLON", "MOB_QT_01_ONG"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: guardian, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
		2: {RefObjID: 2, Codename: ong, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("quest")
	lease, exists := state.PopulationLease("quest", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	at := Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}
	if !state.SpawnQuestMonster(QuestMonsterSpawn{Division: "quest", Population: lease, Codename: guardian,
		Position: at, NowMs: 1000, RadiusSpan: 20}) {
		t.Fatal("guardian admission failed")
	}
	if !state.SpawnQuestMonster(QuestMonsterSpawn{Division: "quest", Population: lease, Codename: ong,
		Position: at, NowMs: 2000, RadiusMin: 20, RadiusSpan: 80, LifetimeMs: 300000}) {
		t.Fatal("treasure guardian admission failed")
	}
	if left := state.ExpireMonsterLifetimes(300999); left != 0 {
		t.Fatalf("%d quest monsters left before their timers", left)
	}
	if left := state.ExpireMonsterLifetimes(301000); left != 1 {
		t.Fatalf("the Ivy guardian's spawn-base timer removed %d monsters", left)
	}
	if left := state.ExpireMonsterLifetimes(302000); left != 1 {
		t.Fatalf("the quest timer removed %d monsters", left)
	}
	if len(state.MaterializedInstances("quest")) != 0 {
		t.Fatal("a timed quest monster stayed in the world")
	}
}
