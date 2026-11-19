/*
===========================================================================

monster_dormancy_cave_test.go - cave monsters sleep and wake by message block

A cave floor is one region id for the whole dungeon, so cave dormancy keys
sleepers by the native 320-unit message block under them and wakes the
blocks within CaveWakeBlocks of a player (monster_dormancy.go). These tests
install a synthetic 10x10-block grid on one cave region and check which
monsters sleep and which wake.

===========================================================================
*/

package simulation

import (
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

const (
	testCaveRegion uint16 = 0x8004
	// testCaveBlocks is the synthetic grid's edge in message blocks.
	testCaveBlocks = 10
	testBlockWidth = 320.0
)

/*
================
caveSleepWorld

A population with dormancy and dormant storage on and the clock past the
first grace window, plus an optional grid on testCaveRegion.
================
*/
type caveSleepWorld struct {
	s *MonsterState
	d *divisionMonsterState
}

func newCaveSleepWorld(t *testing.T, withGrid bool) caveSleepWorld {
	t.Helper()
	grids := map[uint16]worldgeom.DungeonMessageGrid{}
	if withGrid {
		grids[testCaveRegion] = worldgeom.DungeonMessageGrid{Max: [3]float64{testCaveBlocks * testBlockWidth, 100, testCaveBlocks * testBlockWidth}}
	}
	t.Cleanup(worldgeom.InstallDungeonMessageGrids(grids))
	s := NewMonsterState(monster.Template{})
	s.EnableRegionDormancy()
	if err := s.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.StartDivision("cave")
	d := s.divs["cave"]
	d.movers = newMoverStorage(nil)
	s.prepareDormancy(100000, "cave", nil)
	return caveSleepWorld{s: s, d: d}
}

/*
================
caveSleepWorld.settle

Places a settled, unhurt respawning monster at the centre of block (bx, bz)
and asks the scheduler for its next turn at now.
================
*/
func (w caveSleepWorld) settle(gid uint32, bx, bz float64, now int64) {
	actor := monster.Instance{Gid: gid, Ref: monster.MonsterRef{MaxHP: 100}, Nest: monster.NestRow{Respawn: true}, CurrentHP: 100}
	pose := monster.Pose{RegionID: testCaveRegion, X: (bx + 0.5) * testBlockWidth, Z: (bz + 0.5) * testBlockWidth}
	mover := monster.PendingMover{Pose: pose, Activity: monster.ActivityCadence{Interval: 1500, LastCheck: uint32(now - 1000)}}.Expand()
	w.d.instances.set(gid, actor)
	w.d.movers.set(gid, mover)
	w.d.behavior.set(gid, 0)
	w.s.scheduleBehavior(w.d, gid, now)
}

/*
================
caveSleepWorld.arrive

One player at (x, z) on the cave floor at now.
================
*/
func (w caveSleepWorld) arrive(x, z float64, now int64) {
	session := SessionSnapshot{DivisionID: "cave", Population: w.d.lease}
	session.World.Spawn = Spawn{RegionID: testCaveRegion, X: x, Z: z}
	w.s.prepareDormancy(now, "cave", []SessionSnapshot{session})
}

func (w caveSleepWorld) asleep(gid uint32) bool {
	_, sleeping := w.d.dormant[gid]
	return sleeping
}

/*
================
TestCaveDormancyWakesOnlyTheBlocksNearAPlayer

An empty floor sleeps. A player in block (1,1) wakes the monsters up to
three blocks out (block 4 on x: past the 3x3 a viewer sees, by more than
the largest cave sight range) and leaves those four or more out asleep;
the woken ones then stay awake while the player is there.
================
*/
func TestCaveDormancyWakesOnlyTheBlocksNearAPlayer(t *testing.T) {
	w := newCaveSleepWorld(t, true)
	const now = 200000
	near, edge, beyond, far := uint32(1), uint32(2), uint32(3), uint32(4)
	w.settle(near, 1, 1, now)
	w.settle(edge, 4, 1, now)
	w.settle(beyond, 5, 1, now)
	w.settle(far, 8, 8, now)
	for _, gid := range []uint32{near, edge, beyond, far} {
		if !w.asleep(gid) {
			t.Fatalf("monster %d on an empty cave floor did not sleep", gid)
		}
	}

	w.arrive(1.5*testBlockWidth, 1.5*testBlockWidth, now+1000)
	for _, gid := range []uint32{near, edge} {
		if w.asleep(gid) {
			t.Errorf("monster %d within three blocks of the player is still asleep", gid)
		}
	}
	for _, gid := range []uint32{beyond, far} {
		if !w.asleep(gid) {
			t.Errorf("monster %d four or more blocks from the player woke", gid)
		}
	}

	w.s.scheduleBehavior(w.d, near, now+2000)
	w.s.scheduleBehavior(w.d, edge, now+2000)
	if w.asleep(near) || w.asleep(edge) {
		t.Error("a monster near a player fell asleep again")
	}
}

/*
================
TestCaveWithoutABlockNeverSleeps

A cave without an installed grid, and a monster outside the grid's bounds,
stay awake: there is no block to key them by.
================
*/
func TestCaveWithoutABlockNeverSleeps(t *testing.T) {
	noGrid := newCaveSleepWorld(t, false)
	noGrid.settle(1, 1, 1, 200000)
	if noGrid.asleep(1) {
		t.Error("a cave without a message grid slept")
	}

	gridded := newCaveSleepWorld(t, true)
	gridded.settle(2, testCaveBlocks+3, 1, 200000)
	if gridded.asleep(2) {
		t.Error("a monster outside the cave's grid slept")
	}
}

/*
================
TestCavePlayerOffTheGridKeepsTheFloorAwake

A player on the floor but outside its grid (no block of their own) wakes
every sleeping cell of the floor and keeps the whole floor awake, as a cave
behaved when it was one block.
================
*/
func TestCavePlayerOffTheGridKeepsTheFloorAwake(t *testing.T) {
	w := newCaveSleepWorld(t, true)
	const now = 200000
	w.settle(1, 1, 1, now)
	w.settle(2, 8, 8, now)
	if !w.asleep(1) || !w.asleep(2) {
		t.Fatal("the empty cave floor did not sleep")
	}
	w.arrive(-500, -500, now+1000)
	if w.asleep(1) || w.asleep(2) {
		t.Fatal("a player off the grid left part of the floor asleep")
	}
	w.s.scheduleBehavior(w.d, 2, now+2000)
	if w.asleep(2) {
		t.Error("the floor slept while a player stood on it off the grid")
	}
}

/*
================
TestCaveWoundedMonsterNeverSleeps

The ordinary sleep filters still hold in a cave: a wounded monster stays
awake on an empty floor.
================
*/
func TestCaveWoundedMonsterNeverSleeps(t *testing.T) {
	w := newCaveSleepWorld(t, true)
	const now = 200000
	w.settle(1, 1, 1, now)
	w.s.prepareDormancy(now, "cave", nil)
	actor, _ := w.d.instances.lookup(1)
	w.d.forgetDormant(1)
	w.d.instances.wake(1)
	actor.CurrentHP = 50
	w.d.instances.set(1, actor)
	w.s.scheduleBehavior(w.d, 1, now+1000)
	if w.asleep(1) {
		t.Error("a wounded cave monster slept")
	}
}
