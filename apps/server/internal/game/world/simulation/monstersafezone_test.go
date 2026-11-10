/*
===========================================================================

monstersafezone_test.go - monsters vanish when they step into a town

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestSafeZoneRegionReadsTheBattlefieldTable

0x62A6 is a battlefield, 0x62A7 is not; an unknown region is not a town.
Region 25000 (0x61A8) lies inside Jangan.
================
*/
func TestSafeZoneRegionReadsTheBattlefieldTable(t *testing.T) {
	for _, tc := range []struct {
		region uint16
		safe   bool
	}{{0x62a6, false}, {0x62a7, true}, {25000, true}, {0, false}} {
		if got := SafeZoneRegion(tc.region); got != tc.safe {
			t.Fatalf("SafeZoneRegion(%#x) = %v, want %v", tc.region, got, tc.safe)
		}
	}
}

/*
================
TestMonsterInTownVanishes

In the retail server, a monster whose region is a town is set to life
state 3 and leaves; its nest schedules a respawn. Outside a town it stays.
================
*/
func TestMonsterInTownVanishes(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	now := time.Now().UnixMilli()
	snapshot := safeZoneSnapshot(t, ops, instance)
	ops.vanishInSafeZone(monsterTestDivision, snapshot, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); !ok {
		t.Fatal("a monster outside every town vanished")
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.Pose.RegionID = 25000 // inside Jangan
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	// The snapshot predates the commit: the stale mover outside
	// the town must not spare the monster the step moved in.
	ops.vanishInSafeZone(monsterTestDivision, snapshot, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); ok {
		t.Fatal("a monster standing in Jangan stayed")
	}
	if _, ok := ops.Monsters.Get(monsterTestDivision, instance.Gid); ok {
		t.Fatal("the vanished monster is still registered")
	}
}

/*
================
safeZoneSnapshot

The behaviour snapshot vanishInSafeZone weighs: the fixture
actor's mover with the generation it was read at, through the
slots the batch would have carried for it.
================
*/
func safeZoneSnapshot(t *testing.T, ops *MonsterMoverOps, instance monster.Instance) *behaviorActorSnapshot {
	t.Helper()
	moverSlot, monsterSlot := carriedSlots(t, ops, instance.Gid)
	mover, gen, ok := ops.Monsters.MoverGenerationAt(monsterTestDivision, instance.Gid, moverSlot, monsterSlot)
	if !ok {
		t.Fatal("the fixture actor has no mover")
	}
	return &behaviorActorSnapshot{instance: instance, mover: mover, moverGen: gen, slot: moverSlot, monsterSlot: monsterSlot, ok: true}
}

/*
================
TestMonsterVanishWeighsTheSnapshotMover

An actor the step left on its snapshot mover is weighed by that
mover: a snapshot already standing in a town vanishes with no
commit in between. A snapshot the storage has since moved out of
the town cannot kill it: the moved generation sends the weigh to
the stored pose.
================
*/
func TestMonsterVanishWeighsTheSnapshotMover(t *testing.T) {
	now := time.Now().UnixMilli()

	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.Pose.RegionID = 25000 // inside Jangan
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	snapshot := safeZoneSnapshot(t, ops, instance)
	ops.vanishInSafeZone(monsterTestDivision, snapshot, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); ok {
		t.Fatal("a monster snapshotted inside Jangan stayed")
	}

	ops, instance = monsterLegFixture(t, passiveTactics())
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	outside := mover.Pose.RegionID
	mover.Pose.RegionID = 25000 // inside Jangan
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	snapshot = safeZoneSnapshot(t, ops, instance)
	mover.Pose.RegionID = outside
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	ops.vanishInSafeZone(monsterTestDivision, snapshot, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); !ok {
		t.Fatal("a monster the storage moved out of Jangan vanished on its stale snapshot")
	}
}
