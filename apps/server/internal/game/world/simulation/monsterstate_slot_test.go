/*
===========================================================================

monsterstate_slot_test.go - the carried-slot doors answer as the gid doors do

#2133 dense slots: the behaviour batch carries an actor's mover slot
and monster slot to dispatch, and the step's doors (Mover,
MoverGeneration, commitActivity, checkAITimer) read through those
hints instead of resolving the gid through the storage maps again.
A hint is only a hint: a slot recycled onto another actor, a cold
row, a missing mover row and the no-hint sentinel must all answer
exactly what the gid door answers, and a commit addressed with
another actor's slots must land on its own actor or not at all.

===========================================================================
*/

package simulation

import "testing"

/*
================
carriedSlots

The slots the storages hold for gid, as a batch build would stamp
them: the mover row's dense slot and the monster row's hot slot.
================
*/
func carriedSlots(t *testing.T, ops *MonsterMoverOps, gid uint32) (uint32, uint32) {
	t.Helper()
	state := ops.Monsters.division(monsterTestDivision)
	moverSlot, moverKnown := state.movers.slotOf(gid)
	_, monsterSlot, hotKnown := state.instances.hotRowSlot(gid)
	if !hotKnown {
		t.Fatalf("gid %d has no hot row to carry", gid)
	}
	if !moverKnown {
		return behaviorNoMoverSlot, monsterSlot
	}
	return moverSlot, monsterSlot
}

/*
================
TestSlotDoorsMatchGidDoors

Every carried-slot door answers with the gid door's values: the
stored mover read, the generation read (including the synthesized
spawn mover of an actor with no mover row yet), and the AI timer
(checkAITimerAt fires the same bank the gid door shares).
================
*/
func TestSlotDoorsMatchGidDoors(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	ops.Monsters.SetRandomSource(func() float64 { return 0 })
	moverSlot, monsterSlot := carriedSlots(t, ops, actor.Gid)

	wantMover, wantOK := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	gotMover, gotOK := ops.Monsters.MoverAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot)
	if gotOK != wantOK || gotMover != wantMover {
		t.Fatalf("MoverAt differs from Mover:\n got %+v, %v\nwant %+v, %v", gotMover, gotOK, wantMover, wantOK)
	}
	wantMover, wantGen, wantOK := ops.Monsters.MoverGeneration(monsterTestDivision, actor.Gid)
	gotMover, gotGen, gotOK := ops.Monsters.MoverGenerationAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot)
	if gotOK != wantOK || gotGen != wantGen || gotMover != wantMover {
		t.Fatalf("MoverGenerationAt differs from MoverGeneration:\n got %+v, %d, %v\nwant %+v, %d, %v",
			gotMover, gotGen, gotOK, wantMover, wantGen, wantOK)
	}
	// No hint at all reads exactly as the gid door (the hint is an
	// optimisation, never a second answer).
	gotMover, gotGen, gotOK = ops.Monsters.MoverGenerationAt(
		monsterTestDivision, actor.Gid, behaviorNoMoverSlot, behaviorNoMonsterSlot)
	if gotOK != wantOK || gotGen != wantGen || gotMover != wantMover {
		t.Fatal("MoverGenerationAt with sentinel slots differs from MoverGeneration")
	}
	// The stationary nest mate has no mover row: both doors build
	// the same synthesized spawn mover at generation zero.
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	_, mateMonsterSlot := carriedSlots(t, ops, mate.Gid)
	wantMover, wantGen, wantOK = ops.Monsters.MoverGeneration(monsterTestDivision, mate.Gid)
	gotMover, gotGen, gotOK = ops.Monsters.MoverGenerationAt(monsterTestDivision, mate.Gid, behaviorNoMoverSlot, mateMonsterSlot)
	if !wantOK || gotOK != wantOK || gotGen != wantGen || gotMover != wantMover {
		t.Fatalf("synthesized MoverGenerationAt differs:\n got %+v, %d, %v\nwant %+v, %d, %v",
			gotMover, gotGen, gotOK, wantMover, wantGen, wantOK)
	}
	// The first-bank timers are armed immediate: the first check
	// fires (the native startup quirk ai_time_manager.go keeps),
	// so the doors cannot be compared back to back on one bank -
	// the carried door must fire the same timer the gid door then
	// finds already consumed.
	if !ops.Monsters.checkAITimerAt(monsterTestDivision, actor.Gid, monsterSlot, 0, 100000) {
		t.Fatal("checkAITimerAt did not fire the armed first-bank timer")
	}
	if ops.Monsters.checkAITimer(monsterTestDivision, actor.Gid, 0, 100000) {
		t.Fatal("checkAITimerAt fired a different timer bank than checkAITimer")
	}
}

/*
================
TestSlotDoorWithAnotherActorsSlots

A carried slot names whatever actor sits in it at read time, and
the swap on remove can put a different actor there. Asked for one
gid with another actor's slots, the doors must resolve by gid and
answer with the asked actor's rows - never the slot owner's.
================
*/
func TestSlotDoorWithAnotherActorsSlots(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	mateMover, _ := ops.Monsters.Mover(monsterTestDivision, mate.Gid)
	if !ops.Monsters.CommitMover(monsterTestDivision, mate.Gid, mateMover) {
		t.Fatal("mate mover commit refused")
	}
	actorMoverSlot, actorMonsterSlot := carriedSlots(t, ops, actor.Gid)
	mateMoverSlot, mateMonsterSlot := carriedSlots(t, ops, mate.Gid)

	want, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	got, ok := ops.Monsters.MoverAt(monsterTestDivision, actor.Gid, mateMoverSlot, mateMonsterSlot)
	if !ok || got != want || got.Pose == mateMover.Pose {
		t.Fatalf("MoverAt with the mate's slots read the wrong actor:\n got %+v, %v\nwant %+v", got, ok, want)
	}
	wantMover, wantGen, _ := ops.Monsters.MoverGeneration(monsterTestDivision, mate.Gid)
	gotMover, gotGen, gotOK := ops.Monsters.MoverGenerationAt(monsterTestDivision, mate.Gid, actorMoverSlot, actorMonsterSlot)
	if !gotOK || gotGen != wantGen || gotMover != wantMover {
		t.Fatalf("MoverGenerationAt with the actor's slots read the wrong actor:\n got %+v, %d, %v\nwant %+v, %d",
			gotMover, gotGen, gotOK, wantMover, wantGen)
	}
}

/*
================
TestCommitActivityAtMatchesGidDoor

The activity commit through carried slots lands the same values as
the gid door: a cadence-only commit stamps in place, a fuller
commit writes the row, a stale before is refused, and a commit
addressed with another actor's slots still lands on its own actor
and leaves the slot owner's row untouched.
================
*/
func TestCommitActivityAtMatchesGidDoor(t *testing.T) {
	ops, actor, now := activityFixture(t)
	moverSlot, monsterSlot := carriedSlots(t, ops, actor.Gid)
	before, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	after := before
	if !after.Activity.Due(uint32(now + 1001)) {
		t.Fatal("fixture cadence did not fire")
	}
	if !ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, before, after, now+1001) {
		t.Fatal("cadence-only commitActivityAt rejected")
	}
	got, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if got != after {
		t.Fatalf("stored mover differs from cadence-only commitActivityAt:\n got %+v\nwant %+v", got, after)
	}
	// The consumed before can never commit twice, hint or not.
	if ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, before, after, now+1001) {
		t.Fatal("stale cadence commit accepted through carried slots")
	}
	moved := after
	moved.Pose.X += 25
	if !ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, after, moved, now+1001) {
		t.Fatal("pose-carrying commitActivityAt rejected")
	}
	got, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if got != moved {
		t.Fatal("pose change did not land through carried slots")
	}
	// Another actor's slots: the write must still find its own
	// actor (by gid) and leave the slot owner's row alone.
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	mateBefore, _ := ops.Monsters.Mover(monsterTestDivision, mate.Gid)
	mateMoverSlot, mateMonsterSlot := carriedSlots(t, ops, mate.Gid)
	moved.Pose.X += 25
	if !ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, mateMoverSlot, mateMonsterSlot, got, moved, now+1001) {
		t.Fatal("commitActivityAt with foreign slots did not fall back to its actor")
	}
	got, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if got != moved {
		t.Fatal("foreign-slot commit landed on the wrong actor or not at all")
	}
	mateAfter, _ := ops.Monsters.Mover(monsterTestDivision, mate.Gid)
	if mateAfter != mateBefore {
		t.Fatal("foreign-slot commit touched the slot owner's row")
	}
}

/*
================
TestBehaviorActorSnapshotCarriesSlots

The snapshot the leg dispatches on carries the mover and monster
slots the batch resolved for it: the same slots the storages report
for the actor, so the step's carried doors read the rows the batch
read.
================
*/
func TestBehaviorActorSnapshotCarriesSlots(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	moverSlot, monsterSlot := carriedSlots(t, ops, actor.Gid)
	batches := ops.Monsters.behaviorBatches(10000)
	for _, batch := range batches {
		for _, ref := range batch.actors {
			if ref.gid != actor.Gid {
				continue
			}
			snapshot := ops.Monsters.behaviorActorMover(batch.key, ref)
			if !snapshot.ok {
				t.Fatal("snapshot lost the fixture actor")
			}
			if snapshot.slot != moverSlot || snapshot.monsterSlot != monsterSlot {
				t.Fatalf("snapshot carries slots (%d, %d), storages report (%d, %d)",
					snapshot.slot, snapshot.monsterSlot, moverSlot, monsterSlot)
			}
			return
		}
	}
	t.Fatal("fixture actor never came due in a behaviour batch")
}
