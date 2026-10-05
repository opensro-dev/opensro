/*
===========================================================================

monstersquad_test.go - the squad limit and the fixed-query uniques

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const squadTestTarget = uint32(0x7701)

/*
================
squadFixture

Seven idle monsters around one spot, and a player inside their sight.
================
*/
func squadFixture(t *testing.T) (*MonsterMoverOps, []monster.Instance, playerPose) {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100, BodyRadius: 6, MaxHP: 54},
	}
	var nests []monster.NestRow
	for i := 0; i < monster.SquadMemberLimit+1; i++ {
		nests = append(nests, monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: monsterTestRegion, X: 1000 + float64(i), Y: 20, Z: 1000}})
	}
	registry := NewMonsterState(monster.TemplateFromParts(refs, nests))
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(time.Now().UnixMilli())
	actors := registry.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	if len(actors) != monster.SquadMemberLimit+1 {
		t.Fatalf("fixture spawned %d monsters", len(actors))
	}
	ops := &MonsterMoverOps{Monsters: registry, TacticsFor: fixedTactics(aggressiveTactics()), Rand: func() float64 { return 0.5 }}
	target := playerPose{Gid: squadTestTarget, Pose: Spawn{RegionID: monsterTestRegion, X: 1010, Y: 20, Z: 1000}, BodyRadius: 4}
	return ops, actors, target
}

/*
================
squadEngage

Commits gid as holding target, the way an admitted acquisition does.
================
*/
func squadEngage(t *testing.T, ops *MonsterMoverOps, gid, target uint32) {
	t.Helper()
	mover, _ := ops.Monsters.Mover(monsterTestDivision, gid)
	if mover.Mode() == monster.MoverSpawning {
		mustMoverTransition(&mover, monster.MoverEventSpawnHoldElapsed, 0)
	}
	mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target)
	if !ops.Monsters.CommitMover(monsterTestDivision, gid, mover) {
		t.Fatal("engage commit refused")
	}
}

/*
================
TestSightAcquisitionRespectsTheSquadLimit

Six holders fill the squad (5EB2E0). An unforced seventh finds the player
and takes no target (53D8A0 refuses, 53FFE0 fails); the fixed-query and
flag-forced rows join anyway, and a freed place admits the seventh again.
================
*/
func TestSightAcquisitionRespectsTheSquadLimit(t *testing.T) {
	ops, actors, target := squadFixture(t)
	for _, actor := range actors[:monster.SquadMemberLimit] {
		squadEngage(t, ops, actor.Gid, target.Gid)
	}
	last := actors[monster.SquadMemberLimit]
	from := monster.Pose{RegionID: last.Spawn.RegionID, X: last.Spawn.X, Y: last.Spawn.Y, Z: last.Spawn.Z}
	tactics := aggressiveTactics()
	if _, ok := ops.acquireSightTarget(monsterTestDivision, last, from, []playerPose{target}, tactics); ok {
		t.Fatal("an unforced seventh joined a full squad")
	}
	uruchi := last
	uruchi.Nest.HasControls, uruchi.Nest.Controls.ID = true, 0x3F
	if got, ok := ops.acquireSightTarget(monsterTestDivision, uruchi, from, []playerPose{target}, tactics); !ok || got.Gid != target.Gid {
		t.Fatal("the fixed query did not force its target")
	}
	caravan := last
	caravan.Nest.HasControls, caravan.Nest.Controls.Flags = true, 0x21E
	if _, ok := ops.acquireSightTarget(monsterTestDivision, caravan, from, []playerPose{target}, tactics); !ok {
		t.Fatal("a flag-4 row did not force its target")
	}
	// A flag-4 row loses the fixed query even with a fixed-query ID.
	caravan.Nest.Controls.ID = 0x3F
	if caravan.Nest.Controls.FixedQuery() {
		t.Fatal("flag 4 kept the fixed query")
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, actors[0].Gid)
	mustMoverTransition(&mover, monster.MoverEventTargetLost, 0)
	ops.Monsters.CommitMover(monsterTestDivision, actors[0].Gid, mover)
	if _, ok := ops.acquireSightTarget(monsterTestDivision, last, from, []playerPose{target}, tactics); !ok {
		t.Fatal("a freed place did not admit the seventh")
	}
}

/*
================
TestFixedQueryUniqueSwitchesToSecondaryOpponentInReach

548340: with the target beyond body radius + 15 and the secondary opponent
inside it, Uruchi's tactics retarget the secondary; an ordinary row, a
secondary out of reach, or a target in reach keep the target.
================
*/
func TestFixedQueryUniqueSwitchesToSecondaryOpponentInReach(t *testing.T) {
	ops, actors, _ := squadFixture(t)
	actor := actors[0]
	actor.Nest.HasControls, actor.Nest.Controls.ID = true, 0x3F
	const far, near = uint32(0x7801), uint32(0x7802)
	actor.Opponents[1].GID = near
	squadEngage(t, ops, actor.Gid, far)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	live := mover.LivePoseAt(0, nil)
	reach := float64(actor.AttackReachRadius())
	at := func(gid uint32, distance float64) playerPose {
		return playerPose{Gid: gid, Pose: Spawn{RegionID: live.RegionID, X: live.X + distance, Y: live.Y, Z: live.Z}, BodyRadius: 4}
	}
	ordinary := actor
	ordinary.Nest.Controls.ID = 1
	if ops.switchToSecondaryOpponent(monsterTestDivision, ordinary, mover, at(far, reach+5), []playerPose{at(far, reach+5), at(near, 3)}, live, 1000) {
		t.Fatal("an ordinary row ran 548340")
	}
	if ops.switchToSecondaryOpponent(monsterTestDivision, actor, mover, at(far, reach-1), []playerPose{at(far, reach-1), at(near, 3)}, live, 1000) {
		t.Fatal("a target in reach was switched")
	}
	if ops.switchToSecondaryOpponent(monsterTestDivision, actor, mover, at(far, reach+5), []playerPose{at(far, reach+5), at(near, reach)}, live, 1000) {
		t.Fatal("a secondary at the reach edge was taken")
	}
	if !ops.switchToSecondaryOpponent(monsterTestDivision, actor, mover, at(far, reach+5), []playerPose{at(far, reach+5), at(near, 3)}, live, 1000) {
		t.Fatal("the secondary in reach was not taken")
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.TargetGID() != near {
		t.Fatalf("target %#x after the switch", after.TargetGID())
	}
}

/*
================
TestFlagRowsRedirectBetweenTraderAndVehicle

5481A0: a flag-4 row striking a trader turns on the trader's transport when
it is at least 15 closer, and from a companion back to its owner; a thief
(job 2) keeps the flag-4 row's aim but loses the flag-0x80 row's.
================
*/
func TestFlagRowsRedirectBetweenTraderAndVehicle(t *testing.T) {
	ops, actors, _ := squadFixture(t)
	actor := actors[0]
	actor.Nest.HasControls, actor.Nest.Controls.Flags = true, 0x21E
	const owner, vehicle = uint32(0x7901), uint32(0x7902)
	squadEngage(t, ops, actor.Gid, owner)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	live := mover.LivePoseAt(0, nil)
	at := func(gid uint32, distance float64) playerPose {
		return playerPose{Gid: gid, Pose: Spawn{RegionID: live.RegionID, X: live.X + distance, Y: live.Y, Z: live.Z}, BodyRadius: 4}
	}
	trader := at(owner, 40)
	trader.JobState = 1
	transport := at(vehicle, 25.5)
	transport.OwnerGid, transport.Band = owner, 2
	if ops.redirectToVehicle(monsterTestDivision, actor, mover, trader, []playerPose{trader, transport}, live, 1000) {
		t.Fatal("a vehicle 14.5 closer was taken")
	}
	transport = at(vehicle, 25)
	transport.OwnerGid, transport.Band = owner, 2
	thief := trader
	thief.JobState = 2
	if ops.redirectToVehicle(monsterTestDivision, actor, mover, thief, []playerPose{thief, transport}, live, 1000) {
		t.Fatal("a flag-4 row redirected from a thief")
	}
	if !ops.redirectToVehicle(monsterTestDivision, actor, mover, trader, []playerPose{trader, transport}, live, 1000) {
		t.Fatal("a vehicle 15 closer was not taken")
	}
	mover, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if mover.TargetGID() != vehicle {
		t.Fatalf("target %#x after the redirect", mover.TargetGID())
	}
	near := at(owner, 5)
	far := at(vehicle, 30)
	far.OwnerGid, far.Band = owner, 2
	if !ops.redirectToVehicle(monsterTestDivision, actor, mover, far, []playerPose{near, far}, live, 1000) {
		t.Fatal("a companion target did not give way to its nearer owner")
	}
	hunterRow := actor
	hunterRow.Nest.Controls.Flags = 0x29A &^ 0x4
	mover, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if !ops.redirectToVehicle(monsterTestDivision, hunterRow, mover, thief, []playerPose{thief, transport}, live, 1000) {
		t.Fatal("a flag-0x80 row kept aiming at a thief beside its vehicle")
	}
}
