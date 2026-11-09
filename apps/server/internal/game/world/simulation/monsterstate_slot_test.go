/*
===========================================================================

monsterstate_slot_test.go - the carried-slot doors answer as the gid doors do

#2133 dense slots: the behaviour batch carries an actor's mover slot
and monster slot to dispatch, and the step's doors (Mover,
MoverGeneration, commitActivity, checkAITimer, livePose, the AI
event drain) read through those hints instead of resolving the gid
through the storage maps again.
A hint is only a hint: a slot recycled onto another actor, a cold
row, a missing mover row and the no-hint sentinel must all answer
exactly what the gid door answers, and a commit addressed with
another actor's slots must land on its own actor or not at all.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

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
	if committed, cadenceOnly := ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, before, after, now+1001); !committed || !cadenceOnly {
		t.Fatal("cadence-only commitActivityAt rejected or misread")
	}
	got, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if got != after {
		t.Fatalf("stored mover differs from cadence-only commitActivityAt:\n got %+v\nwant %+v", got, after)
	}
	// The consumed before can never commit twice, hint or not.
	if committed, _ := ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, before, after, now+1001); committed {
		t.Fatal("stale cadence commit accepted through carried slots")
	}
	moved := after
	moved.Pose.X += 25
	if committed, cadenceOnly := ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, after, moved, now+1001); !committed || cadenceOnly {
		t.Fatal("pose-carrying commitActivityAt rejected or misread as cadence-only")
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
	if committed, _ := ops.Monsters.commitActivityAt(monsterTestDivision, actor.Gid, mateMoverSlot, mateMonsterSlot, got, moved, now+1001); !committed {
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
TestCommitActivityAtArmsTheGidDoorsDue

The commit's reschedule resolves the mover and resident rows
through the slots the commit carried (scheduleBehaviorCommitted);
the due it arms must be the gid door's. The actor rests idle with
its gate cadence nearest, so the due reads the resident row: a
commit addressed with another actor's slots delegates both reads
to the gid resolves and still arms the gate due, where a dropped
hotRowAt fallback would arm the next tick from a zero resident.
================
*/
func TestCommitActivityAtArmsTheGidDoorsDue(t *testing.T) {
	const now int64 = 100000
	s, d := idleScheduleFixture(t)
	resting := monster.NestRow{Respawn: true}
	actor := idleScheduleActor(t, s, d, 7, resting, 2000, now+5000, now)
	mate := idleScheduleActor(t, s, d, 8, resting, 2000, now+5000, now)
	slotsOf := func(gid uint32) (uint32, uint32) {
		moverSlot, _ := d.movers.slotOf(gid)
		_, monsterSlot, _ := d.instances.hotRowSlot(gid)
		return moverSlot, monsterSlot
	}
	moverSlot, monsterSlot := slotsOf(actor.Gid)
	mateMoverSlot, mateMonsterSlot := slotsOf(mate.Gid)

	commit := func(slot, monsterSlot uint32) {
		t.Helper()
		before, _ := s.Mover("sleep", actor.Gid)
		if committed, cadenceOnly := s.commitActivityAt("sleep", actor.Gid, slot, monsterSlot, before, before, now); !committed || !cadenceOnly {
			t.Fatal("cadence-only commitActivityAt rejected or misread")
		}
		if got, want := scheduleDue(t, d, actor.Gid), now+2001; got != want {
			t.Fatalf("carried reschedule armed %d, want the gate due %d", got, want)
		}
	}
	commit(moverSlot, monsterSlot)
	commit(mateMoverSlot, mateMonsterSlot)
}

/*
================
TestCarriedStorageReadsMatchGidReads

The batch build's carried reads answer as the gid reads do: the
monster row read through its noted hot slot is the gid door's row,
and the mover schedule read through its noted slot is the gid
door's schedule from the same slot. A slot naming another actor,
the no-slot sentinel and a missing actor all miss or delegate to
the gid resolve - never the slot owner's row (#2133).
================
*/
func TestCarriedStorageReadsMatchGidReads(t *testing.T) {
	_, d := queueBenchmarkState(3)
	for gid := uint32(1); gid <= 3; gid++ {
		_, monsterSlot, hot := d.instances.hotRowSlot(gid)
		if !hot {
			t.Fatalf("gid %d has no hot row", gid)
		}
		row, ok := d.instances.hotRowAt(monsterSlot, gid)
		if !ok || row.value(gid) != d.instances.get(gid) {
			t.Fatalf("hotRowAt(%d, %d) = %v, %v; gid read %v", monsterSlot, gid, row.value(gid), ok, d.instances.get(gid))
		}
		if _, ok := d.instances.hotRowAt(monsterSlot, gid+1); ok {
			t.Fatalf("hotRowAt(%d, %d) read the slot owner's row", monsterSlot, gid+1)
		}
		if _, ok := d.instances.hotRowAt(behaviorNoMonsterSlot, gid); ok {
			t.Fatalf("hotRowAt with the sentinel read gid %d", gid)
		}
		want, wantSlot, wantOK := d.movers.scheduleSlot(gid)
		got, gotSlot, gotOK := d.movers.scheduleSlotAt(wantSlot, gid)
		if gotOK != wantOK || gotSlot != wantSlot || got != want {
			t.Fatalf("scheduleSlotAt(%d, %d) = %+v, %d, %v; gid read %+v, %d, %v",
				wantSlot, gid, got, gotSlot, gotOK, want, wantSlot, wantOK)
		}
		// Another actor's slot delegates: the answer is gid's own.
		other := gid%3 + 1
		_, otherSlot, _ := d.movers.scheduleSlot(other)
		got, gotSlot, gotOK = d.movers.scheduleSlotAt(otherSlot, gid)
		if gotOK != wantOK || gotSlot != wantSlot || got != want {
			t.Fatalf("scheduleSlotAt(%d, %d) = %+v, %d, %v; want gid delegation %+v, %d, %v",
				otherSlot, gid, got, gotSlot, gotOK, want, wantSlot, wantOK)
		}
		got, gotSlot, gotOK = d.movers.scheduleSlotAt(behaviorNoMoverSlot, gid)
		if gotOK != wantOK || gotSlot != wantSlot || got != want {
			t.Fatalf("scheduleSlotAt(sentinel, %d) = %+v, %d, %v; want %+v, %d, %v",
				gid, got, gotSlot, gotOK, want, wantSlot, wantOK)
		}
	}
	if _, _, ok := d.movers.scheduleSlotAt(0, 999); ok {
		t.Fatal("scheduleSlotAt read a missing actor")
	}
}

/*
================
TestBehaviorBuildAfterRowSwap

Two batch builds with a despawn between them: removing gid 2 swaps
the last monster row into its hot slot and the last mover row into
its mover slot, so the survivors' carried slots from the first
build name moved rows. The second build must still batch exactly
the survivors, each snapshot equal to the gid doors' reads for its
own actor - a carried slot is verified, never trusted (#2133).
================
*/
func TestBehaviorBuildAfterRowSwap(t *testing.T) {
	s, d := queueBenchmarkState(5)
	for gid := uint32(1); gid <= 5; gid++ {
		row := d.instances.get(gid)
		row.CurrentHP = gid * 10
		d.instances.set(gid, row)
	}
	first := s.behaviorBatches(0)
	if len(first) != 1 || len(first[0].actors) != 5 {
		t.Fatalf("first build batched %v, want 5 actors", first)
	}
	s.releaseBehaviorBatches(first)
	d.instances.remove(2)
	d.movers.remove(2)
	second := s.behaviorBatches(1)
	if len(second) != 1 || len(second[0].actors) != 4 {
		t.Fatalf("second build batched %v, want the 4 survivors", second)
	}
	key := second[0].key
	seen := make(map[uint32]bool)
	for _, ref := range second[0].actors {
		if ref.gid == 2 || seen[ref.gid] {
			t.Fatalf("second build batched gid %d (seen %v)", ref.gid, seen)
		}
		seen[ref.gid] = true
		snapshot := s.behaviorActorMover(key, ref)
		want, wantOK := s.behaviorActor(key, ref.gid)
		if !snapshot.ok || !wantOK || snapshot.instance != want || snapshot.instance.CurrentHP != ref.gid*10 {
			t.Fatalf("gid %d snapshot %+v, want the gid read %+v", ref.gid, snapshot.instance, want)
		}
		moverSlot, _ := d.movers.slotOf(ref.gid)
		_, monsterSlot, _ := d.instances.hotRowSlot(ref.gid)
		if snapshot.slot != moverSlot || snapshot.monsterSlot != monsterSlot {
			t.Fatalf("gid %d snapshot carries slots (%d, %d), storages report (%d, %d)",
				ref.gid, snapshot.slot, snapshot.monsterSlot, moverSlot, monsterSlot)
		}
	}
	s.releaseBehaviorBatches(second)
	if third := s.behaviorBatches(2); len(third) != 1 || len(third[0].actors) != 4 {
		t.Fatalf("third build batched %v, want the 4 survivors again", third)
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

/*
================
TestBehaviorActorLiveAtAnswersAsGidProbe

The dispatch's existence probe through the monster slot the batch
carried answers exactly what the gid probe answers: live for the
actor its own ref names, live for a live actor behind a crossed
hint (the slot names another actor, so the gid resolve decides),
and dead for an absent gid behind a live actor's slot or no hint.
================
*/
func TestBehaviorActorLiveAtAnswersAsGidProbe(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	_, mateSlot := carriedSlots(t, ops, mate.Gid)
	var key populationKey
	var own behaviorActorRef
	found := false
	for _, batch := range ops.Monsters.behaviorBatches(10000) {
		for _, ref := range batch.actors {
			if ref.gid == actor.Gid {
				key, own, found = batch.key, ref, true
			}
		}
	}
	if !found {
		t.Fatal("fixture actor never came due in a behaviour batch")
	}
	if !ops.Monsters.behaviorActorLiveAt(key, own) {
		t.Fatal("carried probe lost its actor")
	}
	if !ops.Monsters.behaviorActorLiveAt(key, behaviorActorRef{gid: actor.Gid, monsterSlot: behaviorNoMonsterSlot}) {
		t.Fatal("slotless probe lost its actor")
	}
	// A crossed hint names the mate's row: the gid resolve decides.
	if !ops.Monsters.behaviorActorLiveAt(key, behaviorActorRef{gid: actor.Gid, monsterSlot: mateSlot}) {
		t.Fatal("crossed probe lost its actor")
	}
	if ops.Monsters.behaviorActorLiveAt(key, behaviorActorRef{gid: 999999, monsterSlot: own.monsterSlot}) {
		t.Fatal("probe invented an absent actor behind a live slot")
	}
	if ops.Monsters.behaviorActorLiveAt(key, behaviorActorRef{gid: 999999, monsterSlot: behaviorNoMonsterSlot}) {
		t.Fatal("slotless probe invented an absent actor")
	}
	if !ops.Monsters.behaviorActorLive(key, actor.Gid) || ops.Monsters.behaviorActorLive(key, 999999) {
		t.Fatal("gid probe disagrees with the carried probe")
	}
}

/*
================
TestSlotLivePoseMatchesGidDoor

livePoseAt through the carried slots answers livePose's pose: on
the row the mover storage holds, on the synthesized spawn pose of
an actor with no mover row yet, and not at all for a missing gid.
Another actor's slots and the no-hint sentinel answer by gid.
================
*/
func TestSlotLivePoseMatchesGidDoor(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	moverSlot, monsterSlot := carriedSlots(t, ops, actor.Gid)
	now := int64(100000)

	want, wantOK := ops.Monsters.livePose(monsterTestDivision, actor.Gid, now)
	got, gotOK := ops.Monsters.livePoseAt(monsterTestDivision, actor.Gid, moverSlot, monsterSlot, now)
	if gotOK != wantOK || got != want {
		t.Fatalf("livePoseAt differs from livePose:\n got %+v, %v\nwant %+v, %v", got, gotOK, want, wantOK)
	}
	got, gotOK = ops.Monsters.livePoseAt(monsterTestDivision, actor.Gid, behaviorNoMoverSlot, behaviorNoMonsterSlot, now)
	if gotOK != wantOK || got != want {
		t.Fatal("livePoseAt with sentinel slots differs from livePose")
	}
	// Another actor's slots: the gid resolve decides.
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	mateMoverSlot, mateMonsterSlot := carriedSlots(t, ops, mate.Gid)
	got, gotOK = ops.Monsters.livePoseAt(monsterTestDivision, actor.Gid, mateMoverSlot, mateMonsterSlot, now)
	if gotOK != wantOK || got != want {
		t.Fatal("livePoseAt with the mate's slots read the wrong actor")
	}
	// The mate has no mover row: both doors synthesize the spawn pose.
	want, wantOK = ops.Monsters.livePose(monsterTestDivision, mate.Gid, now)
	got, gotOK = ops.Monsters.livePoseAt(monsterTestDivision, mate.Gid, mateMoverSlot, mateMonsterSlot, now)
	if gotOK != wantOK || got != want {
		t.Fatalf("synthesized livePoseAt differs:\n got %+v, %v\nwant %+v, %v", got, gotOK, want, wantOK)
	}
	if _, ok := ops.Monsters.livePoseAt(monsterTestDivision, 999999, moverSlot, monsterSlot, now); ok {
		t.Fatal("livePoseAt invented a pose for a missing actor")
	}
}

/*
================
TestTakeAIEventsAtMatchesGidDoor

The step's event drain through the carried monster slot peeks and
takes exactly what the gid doors do: the peek copies without
consuming, the take drains the queue once, and a foreign slot
still drains the asked actor's queue.
================
*/
func TestTakeAIEventsAtMatchesGidDoor(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	_, monsterSlot := carriedSlots(t, ops, actor.Gid)
	state := ops.Monsters.division(monsterTestDivision)
	want := []monsterAIEvent{{Event: 1, Kind: 2, Source: 7}, {Event: 3, Kind: 4, Source: 8}}
	if state.aiEvents == nil {
		state.aiEvents = make(map[uint32][]monsterAIEvent)
	}
	state.aiEvents[actor.Gid] = append([]monsterAIEvent(nil), want...)

	peeked := ops.Monsters.peekAIEventsAt(monsterTestDivision, actor.Gid, monsterSlot)
	if len(peeked) != len(want) || peeked[0] != want[0] || peeked[1] != want[1] {
		t.Fatalf("peekAIEventsAt = %v, want %v", peeked, want)
	}
	if got := ops.Monsters.peekAIEvents(monsterTestDivision, actor.Gid); len(got) != len(want) {
		t.Fatalf("peekAIEventsAt consumed the queue: gid peek now %v", got)
	}
	// Another actor's slot: the gid resolve decides, the queue is
	// still the asked actor's.
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	_, mateSlot := carriedSlots(t, ops, mate.Gid)
	got := ops.Monsters.takeAIEventsAt(monsterTestDivision, actor.Gid, mateSlot)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("takeAIEventsAt with the mate's slot = %v, want %v", got, want)
	}
	if again := ops.Monsters.takeAIEvents(monsterTestDivision, actor.Gid); len(again) != 0 {
		t.Fatalf("takeAIEventsAt did not drain the queue: gid take now %v", again)
	}
	if again := ops.Monsters.takeAIEventsAt(monsterTestDivision, 999999, monsterSlot); len(again) != 0 {
		t.Fatalf("takeAIEventsAt invented events for a missing actor: %v", again)
	}
}
