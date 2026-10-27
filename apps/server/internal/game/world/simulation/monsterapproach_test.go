/*
===========================================================================

monsterapproach_test.go - the +0x114 pursuit admission for dynamic spawns

A spawn that resolves its tactics from the spawn table alone has no
FleeType and no BattleStyle, and native installs the pursuit callback for
it as well, unconditionally at installer entry. The port must admit those
spawns, and must not admit a nest that only looks like one.

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
TestOrdinarySquadApproachAdmitsDetachedPolicySpawns

The retail server's tactics installer stores its direction-gated pursuit
callback into slot +0x114 in an unconditional run at function entry.
A dynamic spawn carries neither a TacticsControls row nor a summoner, so
the callback belongs to it.
================
*/
func TestOrdinarySquadApproachAdmitsDetachedPolicySpawns(t *testing.T) {
	questSpawn := monster.Instance{
		NestDetached: true,
		Nest:         monster.NestRow{PolicyPinned: true},
	}
	for name, actor := range map[string]monster.Instance{
		"scripted spawn": questSpawn,
		"GM spawn":       questSpawn,
	} {
		if !ordinarySquadApproach(actor) {
			t.Fatalf("%s excluded from ordinarySquadApproach (+0x114)", name)
		}
	}
}

/*
================
TestOrdinarySquadApproachAdmitsAnAuthoredAreaNestAfterItsSlotIsReleased

A resource-authored area nest is policy-pinned and holds its slot, so it
stays out while it holds it. Releasing that slot (through
detachCrossPlaneNest) leaves the actor detached and policy-pinned, and the
predicate then admits it. Native installs the callback for every tactics
instance at its spawn, so admitting the actor after the release is the
closer of the two approximations. The flip is deliberate, and this test
pins it on the real detach path.
================
*/
func TestOrdinarySquadApproachAdmitsAnAuthoredAreaNestAfterItsSlotIsReleased(t *testing.T) {
	registry := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, BodyRadius: 6, MaxHP: 54},
	}, []monster.NestRow{
		{
			SpawnPoint:   monster.SpawnPoint{RefObjID: 1933, RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000},
			PolicyPinned: true,
			MaxCount:     1,
		},
	}))
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(time.Now().UnixMilli())
	actor := monsterInstanceByRef(t, registry, 1933)
	if ordinarySquadApproach(actor) {
		t.Fatal("the authored area nest is admitted while it holds its slot")
	}

	registry.mu.Lock()
	state := registry.populationForObject(monsterTestDivision, actor.Gid)
	if !registry.detachNestLocked(state, actor.Gid, time.Now().UnixMilli()) {
		registry.mu.Unlock()
		t.Fatal("the authored area nest released no slot")
	}
	detached := state.instances.get(actor.Gid)
	registry.mu.Unlock()

	if !detached.NestDetached {
		t.Fatal("the released actor is not detached")
	}
	if !detached.Nest.PolicyPinned {
		t.Fatal("the released actor lost its pinned policy")
	}
	if !ordinarySquadApproach(detached) {
		t.Fatal("the released authored area nest stays out of ordinarySquadApproach")
	}
}

/*
================
TestOrdinarySquadApproachKeepsUnpinnedAndAttachedNestsOut

Native resolves a monster's tactics once, when it is spawned, so a
monster that loses its nest slot later keeps the tactics it came with and
stays admitted. What never resolved a spawn table row stays out:
a nest that was never pinned (the stuck path) and a resource-authored area
nest that still holds its slot.
================
*/
func TestOrdinarySquadApproachKeepsUnpinnedAndAttachedNestsOut(t *testing.T) {
	for name, actor := range map[string]monster.Instance{
		"detached but unpinned nest": {NestDetached: true},
		"attached area nest":         {Nest: monster.NestRow{PolicyPinned: true}},
		"plain instance":             {},
	} {
		if ordinarySquadApproach(actor) {
			t.Fatalf("%s unexpectedly admitted to ordinarySquadApproach", name)
		}
	}
}

/*
================
TestQuestAndGMSpawnsJoinTheOrdinaryPursuit

A quest script and /LOADMONSTER both spawn without a tactics id, so the
server resolves the spawn table row itself. The spawn table rows of the four
quest monsters carry no control block, so the pursuit belongs to them.
================
*/
func TestQuestAndGMSpawnsJoinTheOrdinaryPursuit(t *testing.T) {
	const guardian = "MOB_QT_02_PUNISHER_CLON"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: guardian, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("quest")
	lease, exists := state.PopulationLease("quest", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	if !state.SpawnQuestMonster(QuestMonsterSpawn{Division: "quest", Population: lease, Codename: guardian,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000, RadiusSpan: 20}) {
		t.Fatal("guardian admission failed")
	}
	actors := state.MaterializedInstances("quest")
	if len(actors) != 1 {
		t.Fatalf("quest population %d, want 1", len(actors))
	}
	if actors[0].Nest.HasControls {
		t.Fatal("the guardian's spawn table row grew a control block")
	}
	if !ordinarySquadApproach(actors[0]) {
		t.Fatal("the quest guardian is excluded from the +0x114 pursuit")
	}

	gm, gmLease := gmMonsterState(t, 0)
	if gm.SpawnGMMonsters(GMMonsterSpawn{Division: "gm", Population: gmLease, RefObjID: 1, Count: 1,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000}) != 1 {
		t.Fatal("GM spawn refused")
	}
	loaded := gm.MaterializedInstances("gm")
	if len(loaded) != 1 {
		t.Fatalf("GM population %d, want 1", len(loaded))
	}
	if !ordinarySquadApproach(loaded[0]) {
		t.Fatal("the /LOADMONSTER spawn is excluded from the +0x114 pursuit")
	}
}
