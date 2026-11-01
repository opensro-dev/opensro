/*
===========================================================================

tickdoor_writes_test.go - tick sweeps that write character records (#1836)

The division lock serializes the action tick, but it neither excludes a
reader under the store's read door nor marks the record dirty. A sweep that
writes a record outside a character door races every Snapshot, and loses a
persisted field at the next restart. The pet-window test runs the real
store (commit, reboot, -race readers); the others watch every door
(doorwatch_test.go) and fail when a write escapes one.

===========================================================================
*/

package action

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	tickDoorPetItem     = 24001
	tickDoorPetCodename = "ITEM_MALL_PET_SKILL_COLD"
	tickDoorReaders     = 2
)

/*
================
TestPetSkillWindowRetireCommitsThroughTheTickDoor

A spent window must be gone after a restart, the retire tick must commit
inside the tick batch, and a tick with nothing spent must not commit. The
readers copy the windows under ReadState while the sweep runs; under -race
an out-of-door compaction reports a data race.
================
*/
func TestPetSkillWindowRetireCommitsThroughTheTickDoor(t *testing.T) {
	seed := testCharacter()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacterDeferred = d.authority.UpdateCharacterDeferred
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	endMs := d.clock.NowMs() + 1000
	d.authority.MutateCharacter(d.character, "seed-pet-window", func() {
		d.character.PetSkillWindows = []domain.PetSkillWindow{
			{ItemRefObjID: tickDoorPetItem, Codename: tickDoorPetCodename, EndUnixMs: endMs},
		}
	})
	d.rt.TrackPetSkillWindows(testDivision, d.character.Name)

	sweep := func(nowMs int64) uint64 {
		before := d.authority.Health().Commits
		d.authority.BeginTickBatch()
		d.rt.advancePetSkillWindows(nowMs)
		d.authority.FlushTickBatch()
		return d.authority.Health().Commits - before
	}
	if commits := sweep(endMs - 1); commits != 0 {
		t.Fatalf("a sweep with no spent window committed %d times", commits)
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range tickDoorReaders {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				d.authority.ReadState(func() {
					_ = append([]domain.PetSkillWindow(nil), d.character.PetSkillWindows...)
				})
			}
		}()
	}
	commits := sweep(endMs)
	close(stop)
	readers.Wait()
	if commits != 1 {
		t.Fatalf("the retire tick committed %d times, want one batched commit", commits)
	}
	if health := d.authority.Health(); health.FailedWrites != 0 {
		t.Fatalf("store health after the retire: %+v", health)
	}

	reopened := d.reboot(t)
	if got := reopened.character.PetSkillWindows; len(got) != 0 {
		t.Fatalf("persisted windows %+v, want the spent row gone after a restart", got)
	}
}

/*
================
TestFieldRetireWritesTheLeaverInsideItsDoor

A recipient who steps out of a Harmony field loses its instance. The
teardown changes the leaver's record, and every such change must
fall inside a door that covers the leaver.
================
*/
func TestFieldRetireWritesTheLeaverInsideItsDoor(t *testing.T) {
	rt, clock, c, row := harmonyFixture(t)
	radius := float64(row.TimedEffect.Field.Radius)
	mate := nearbyCharacter(rt, c, 12, "harmony-mate", radius-1)
	watch := watchDoors(t, rt, mate)

	releaseHarmony(t, rt, clock, c, row)
	fieldPass(rt, clock)
	if !hasSkillEffect(rt, mate.Name, row.ID) {
		t.Fatal("the mate was not admitted")
	}
	watch.finish()

	moveCharacter(rt, mate, c, radius+1)
	fieldPass(rt, clock)
	if hasSkillEffect(rt, mate.Name, row.ID) {
		t.Fatal("leaving the field did not end the instance")
	}
	if covered := watch.finish(); covered[mate] == 0 {
		t.Fatal("the retire changed nothing in the leaver's record, so this test proves nothing")
	}
}

/*
================
TestCombatTrapRetireWritesThePlanterInsideItsDoor

Both retirements of a planter's trap buff change its record (the body
status owner): the
link-group cap when a replant replaces the older trap, and the object tick
when the planter walks away. Each change must fall inside a door that
covers the planter.
================
*/
func TestCombatTrapRetireWritesThePlanterInsideItsDoor(t *testing.T) {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	if !rt.Monsters.Defeat(testDivision, gid, clock.Now()) {
		t.Fatal("could not clear the trigger monster")
	}
	plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	clock.Advance(time.Duration(skill.CoolTimeMs+1) * time.Millisecond)
	rt.drainSkillFinalizes(clock.NowMs())
	watch := watchDoors(t, rt, c)

	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	if buffs := rt.effects.Snapshot(testDivision, c.Name); len(buffs) != 1 {
		t.Fatalf("%d buffs after the replant, want the replaced trap's retired", len(buffs))
	}
	watch.finish()

	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn.X += float64(skill.CombatTrap.OwnerDistance) + 1
	})
	rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("the trap's buff survived its planter walking away")
	}
	if covered := watch.finish(); covered[c] == 0 {
		t.Fatal("the tick retire changed nothing in the planter's record, so this test proves nothing")
	}
}
