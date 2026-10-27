/*
===========================================================================

tickdoor_writes_test.go - tick sweeps that write persisted fields (#1836)

The division lock serializes the action tick, but it neither excludes a
reader under the store's read door nor marks the record dirty. A sweep that
writes a saved field outside a character door loses the write at the next
restart and races every Snapshot. These tests run the sweeps through the
real store and prove the write committed and was taken under the door.

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
	"opensro.online/server/internal/game/item/wire"
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
TestFieldRetireRunsInsideTheCharacterDoor

A recipient who steps out of a Harmony field loses its instance; the
teardown writes the live record, so it must enter the leaver's tick door
and no one else's.
================
*/
func TestFieldRetireRunsInsideTheCharacterDoor(t *testing.T) {
	rt, clock, c, row := harmonyFixture(t)
	radius := float64(row.TimedEffect.Field.Radius)
	mate := nearbyCharacter(rt, c, 12, "harmony-mate", radius-1)
	deps := rt.deps.(*enterworld.Deps)
	var doors []string
	deps.UpdateCharacterDeferred = func(character *enterworld.Character, label string, update func() bool) bool {
		if label == "skill-field-retire" {
			doors = append(doors, character.Name)
		}
		return update()
	}

	releaseHarmony(t, rt, clock, c, row)
	fieldPass(rt, clock)
	if !hasSkillEffect(rt, mate.Name, row.ID) || len(doors) != 0 {
		t.Fatalf("admission: mate holds %v, retire doors %v", hasSkillEffect(rt, mate.Name, row.ID), doors)
	}

	moveCharacter(rt, mate, c, radius+1)
	fieldPass(rt, clock)
	if hasSkillEffect(rt, mate.Name, row.ID) {
		t.Fatal("leaving the field did not end the instance")
	}
	if len(doors) != 1 || doors[0] != mate.Name {
		t.Fatalf("retire doors %v, want exactly the leaver's", doors)
	}
}

/*
================
TestCombatTrapRetireRunsInsideTheCharacterDoor

Both retirements of a planter's trap buff tear down the saved record. The
link-group cap retires inside the replant's own release door (a handler
reaches it, so the door is immediate), and the object tick retires through
the planter's tick door.
================
*/
func TestCombatTrapRetireRunsInsideTheCharacterDoor(t *testing.T) {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	if !rt.Monsters.Defeat(testDivision, gid, clock.Now()) {
		t.Fatal("could not clear the trigger monster")
	}
	deps := rt.deps.(*enterworld.Deps)
	immediate := deps.UpdateCharacter
	buffsAtRelease := -1
	deps.UpdateCharacter = func(character *enterworld.Character, label string, update func() bool) bool {
		var committed bool
		if immediate != nil {
			committed = immediate(character, label, update)
		} else {
			committed = update()
		}
		if label == "release-combat-trap" {
			buffsAtRelease = len(rt.effects.Snapshot(testDivision, c.Name))
		}
		return committed
	}
	var tickDoors []string
	deps.UpdateCharacterDeferred = func(character *enterworld.Character, label string, update func() bool) bool {
		if label == "combat-trap-retire" {
			tickDoors = append(tickDoors, character.Name)
		}
		return update()
	}

	plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	clock.Advance(time.Duration(skill.CoolTimeMs+1) * time.Millisecond)
	rt.drainSkillFinalizes(clock.NowMs())
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	if buffsAtRelease != 1 {
		t.Fatalf("%d buffs when the replant's door closed, want the replaced trap's buff retired inside it", buffsAtRelease)
	}

	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn.X += float64(skill.CombatTrap.OwnerDistance) + 1
	})
	rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("the trap's buff survived its planter walking away")
	}
	if len(tickDoors) != 1 || tickDoors[0] != c.Name {
		t.Fatalf("tick retire doors %v, want exactly the planter's", tickDoors)
	}
}

/*
================
TestCureOnAMateMarksBothRecordsInTheDoor

A cure on a party mate writes the mate's saved abnormal state, so the
caster's recovery door must cover the mate too, or the store never marks
the mate dirty and a restart loses the cure.
================
*/
func TestCureOnAMateMarksBothRecordsInTheDoor(t *testing.T) {
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10077)
	caster.Intellect = testInt64(80)
	caster.CurrentMP = nil
	now := clock.NowMs()
	door := installStoreDoor(t, rt)
	mate := addDoorMate(t, rt, caster, door, now, monster.Gid)
	rt.clearSkillFinalizes(testDivision, caster.Name)
	deps := rt.deps.(*enterworld.Deps)
	locked := deps.UpdateCharacters
	var covered []*enterworld.Character
	deps.UpdateCharacters = func(characters []*enterworld.Character, label string, update func() bool) bool {
		if label == "skill-self-recovery" {
			covered = append(covered, characters...)
		}
		return locked(characters, label, update)
	}

	result := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{
		ActionId: row.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(mate),
	}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	if len(covered) != 2 || covered[0] != caster || covered[1] != mate {
		t.Fatalf("recovery door covered %d records, want the caster then the cured mate", len(covered))
	}
}
