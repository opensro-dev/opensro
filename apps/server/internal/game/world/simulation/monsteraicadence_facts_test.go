/*
===========================================================================

monsteraicadence_facts_test.go - the AI timer check reads facts, not copies

checkAITimerAt used to expand the actor's full Instance (the shared
catalogue rows dominate a lookup; see currentHP) only to read its
hit points and, when the timer bank is first built, its nest tactics
flags. timerFactsAt reads those two facts off the resident row
through the carried slot, and the cadence test pins that the narrow
read answers exactly what the full read answered: the asked actor's
facts under its own slot, another actor's slot, and the no-hint
sentinel; hit points zero for a dead actor; no facts for an unknown
gid (#2029).

===========================================================================
*/

package simulation

import "testing"

/*
================
TestTimerFactsAtReadsResidentRow
================
*/
func TestTimerFactsAtReadsResidentRow(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	state := ops.Monsters.division(monsterTestDivision)
	_, monsterSlot := carriedSlots(t, ops, actor.Gid)

	full, exists := state.instances.lookup(actor.Gid)
	if !exists {
		t.Fatal("fixture actor has no instance row")
	}
	hp, flags, ok := state.instances.timerFactsAt(monsterSlot, actor.Gid)
	if !ok || hp != full.CurrentHP || flags != full.Nest.NativeTacticsFlags {
		t.Fatalf("timerFactsAt = (%d, %d, %v), want (%d, %d, true)",
			hp, flags, ok, full.CurrentHP, full.Nest.NativeTacticsFlags)
	}

	// Another actor's slot: the hint no longer names the asked gid,
	// so the answer is the asked actor's facts, never the owner's.
	mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
	_, mateSlot := carriedSlots(t, ops, mate.Gid)
	hp, flags, ok = state.instances.timerFactsAt(mateSlot, actor.Gid)
	if !ok || hp != full.CurrentHP || flags != full.Nest.NativeTacticsFlags {
		t.Fatalf("timerFactsAt with a foreign slot = (%d, %d, %v), want the asked actor's facts", hp, flags, ok)
	}

	// The no-hint sentinel answers as the gid door does.
	hp, flags, ok = state.instances.timerFactsAt(behaviorNoMonsterSlot, actor.Gid)
	if !ok || hp != full.CurrentHP || flags != full.Nest.NativeTacticsFlags {
		t.Fatalf("timerFactsAt with the sentinel = (%d, %d, %v), want the asked actor's facts", hp, flags, ok)
	}

	if _, _, ok := state.instances.timerFactsAt(0, 999999); ok {
		t.Fatal("timerFactsAt answered for an unknown gid")
	}

	// A dead actor still has facts; its hit points are the verdict.
	dead := full
	dead.CurrentHP = 0
	state.instances.set(actor.Gid, dead)
	hp, _, ok = state.instances.timerFactsAt(monsterSlot, actor.Gid)
	if !ok || hp != 0 {
		t.Fatalf("timerFactsAt for a dead actor = (%d, %v), want (0, true)", hp, ok)
	}
	if ops.Monsters.checkAITimerAt(monsterTestDivision, actor.Gid, monsterSlot, 1, 100000) {
		t.Fatal("checkAITimerAt fired for a dead actor")
	}
}

/*
================
TestCheckAITimerAtMatchesGidDoor

The carried check returns the gid check's verdict for the same
actor, under its own slot, another actor's slot and the sentinel.
The first-bank acquisition timer is armed immediate, so one actor's
bank can only be checked once: each arm runs on a fresh fixture,
where the gid door's verdict is the reference.
================
*/
func TestCheckAITimerAtMatchesGidDoor(t *testing.T) {
	verdict := func(t *testing.T, carried bool, slotOf func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32) bool {
		t.Helper()
		ops, actor, now := activityFixture(t)
		if !carried {
			return ops.Monsters.checkAITimer(monsterTestDivision, actor.Gid, 1, now)
		}
		return ops.Monsters.checkAITimerAt(monsterTestDivision, actor.Gid, slotOf(t, ops, actor.Gid), 1, now)
	}
	own := func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32 {
		_, monsterSlot := carriedSlots(t, ops, gid)
		return monsterSlot
	}
	foreign := func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32 {
		mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
		_, mateSlot := carriedSlots(t, ops, mate.Gid)
		return mateSlot
	}
	sentinel := func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32 {
		return behaviorNoMonsterSlot
	}

	want := verdict(t, false, nil)
	for name, slotOf := range map[string]func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32{
		"own slot": own,
		"foreign":  foreign,
		"sentinel": sentinel,
	} {
		if got := verdict(t, true, slotOf); got != want {
			t.Fatalf("checkAITimerAt (%s) = %v, want %v (the gid door)", name, got, want)
		}
	}
	ops, _, now := activityFixture(t)
	if ops.Monsters.checkAITimerAt(monsterTestDivision, 999999, behaviorNoMonsterSlot, 1, now) {
		t.Fatal("checkAITimerAt fired for an unknown gid")
	}
}

/*
================
TestSelectedAITimerReadyAtMatchesGidDoor

The attack step's carried selected-timer check returns the gid
check's verdict for the same actor, under its own slot, another
actor's slot and the sentinel. The first check creates the
actor's timer bank, so each arm runs on a fresh fixture, where
the gid door's verdict is the reference.
================
*/
func TestSelectedAITimerReadyAtMatchesGidDoor(t *testing.T) {
	verdict := func(t *testing.T, carried bool, slotOf func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32) bool {
		t.Helper()
		ops, actor, now := activityFixture(t)
		if !carried {
			return ops.Monsters.selectedAITimerReady(monsterTestDivision, actor.Gid, now)
		}
		return ops.Monsters.selectedAITimerReadyAt(monsterTestDivision, actor.Gid, slotOf(t, ops, actor.Gid), now)
	}
	own := func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32 {
		_, monsterSlot := carriedSlots(t, ops, gid)
		return monsterSlot
	}
	foreign := func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32 {
		mate := monsterInstanceByRefIn(t, ops.Monsters, 2000, monsterTestRegion)
		_, mateSlot := carriedSlots(t, ops, mate.Gid)
		return mateSlot
	}
	sentinel := func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32 {
		return behaviorNoMonsterSlot
	}

	want := verdict(t, false, nil)
	for name, slotOf := range map[string]func(t *testing.T, ops *MonsterMoverOps, gid uint32) uint32{
		"own slot": own,
		"foreign":  foreign,
		"sentinel": sentinel,
	} {
		if got := verdict(t, true, slotOf); got != want {
			t.Fatalf("selectedAITimerReadyAt (%s) = %v, want %v (the gid door)", name, got, want)
		}
	}
	ops, _, now := activityFixture(t)
	if ops.Monsters.selectedAITimerReadyAt(monsterTestDivision, 999999, behaviorNoMonsterSlot, now) {
		t.Fatal("selectedAITimerReadyAt fired for an unknown gid")
	}
}
