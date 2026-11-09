/*
===========================================================================

monsteractivity_schedule.go - when a monster's next behaviour step falls due

The scheduling doors that arm the behaviour queue
(monsteractivity_queue.go): the gid door the event lanes call
(scheduleBehavior), the carried-slot doors the batch build and the
activity commit re-arm through (scheduleBehaviorCarried,
scheduleBehaviorCommitted), and the due computation they share
(nextBehaviorDue and its idle/acquisition reads). The doors differ
only in how the mover and resident rows are resolved - by gid, or
through verified slot hints that delegate to the gid resolve on a
miss - so the armed due is the same either way, and the family
stays in one file where a due-rule change sees all of them.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
)

/*
================
MonsterState.scheduleBehavior
================
*/
func (s *MonsterState) scheduleBehavior(state *divisionMonsterState, gid uint32, now int64) {
	s.scheduleBehaviorSlot(state, gid, now)
}

/*
================
MonsterState.wakeBehavior

Re-arm an actor for the next leg, the event doors' set(gid, 0)
through the division door: a dropped pooled plan replans at its
next due, and the far idle due the batch build armed from the
leg-start row (#2136) must not strand that replan.
================
*/
func (s *MonsterState) wakeBehavior(division string, gid uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.populationForObject(division, gid); state != nil {
		state.behavior.set(gid, 0)
	}
}

/*
================
MonsterState.scheduleBehaviorSlot

scheduleBehavior, also returning the mover slot the read resolved.
The batch build carries the slot to the dispatch read
(behaviorActorMover), which verifies it against the gid before use
and falls back to the gid resolve when a despawn recycled it
(#2135). behaviorNoMoverSlot means the actor had no mover row: it
still joins the batch, and its mover is synthesized at the read,
exactly as before the carry.
================
*/
func (s *MonsterState) scheduleBehaviorSlot(state *divisionMonsterState, gid uint32, now int64) uint32 {
	state.forgetDormant(gid)
	m, slot, ok := state.movers.scheduleSlot(gid)
	if !ok {
		state.behavior.remove(gid)
		return behaviorNoMoverSlot
	}
	if s.tryDormant(state, gid, m, now) {
		return slot
	}
	resident, _ := state.instances.hotRow(gid)
	state.behavior.set(gid, s.nextBehaviorDue(state, gid, m, resident, now))
	return slot
}

/*
================
MonsterState.scheduleBehaviorCommitted

scheduleBehaviorSlot for the activity commit's own reschedule
(monsteractivity.go's commitActivityAt): the commit has just read
and written the mover row through the slot it carries, and holds
the actor's monster slot too, so the re-arm resolves both rows
through the verified hints instead of the storages' gid maps -
scheduleSlotAt and hotRowAt delegate to the gid resolve when a
hint no longer names the actor, so the armed due is
scheduleBehaviorSlot's either way. The commit holds no queue
slot, so the re-arm itself still lands through behavior.set.
================
*/
func (s *MonsterState) scheduleBehaviorCommitted(state *divisionMonsterState, gid uint32, slot, monsterSlot uint32, now int64) {
	state.forgetDormant(gid)
	m, _, ok := state.movers.scheduleSlotAt(slot, gid)
	if !ok {
		state.behavior.remove(gid)
		return
	}
	if s.tryDormant(state, gid, m, now) {
		return
	}
	resident, hot := state.instances.hotRowAt(monsterSlot, gid)
	if !hot {
		resident, _ = state.instances.hotRow(gid)
	}
	state.behavior.set(gid, s.nextBehaviorDue(state, gid, m, resident, now))
}

/*
================
MonsterState.nextBehaviorDue

When the actor's next behaviour step falls due: the next tick, or
the pending row's cadence phase when it is armed and no help
command pends - the activity cadence's own deadline, brought
forward to the arrival when the mover lands sooner. An idle row
arms at its real next due instead (idleBehaviorDue, #2136). The
gid door and the batch build's carried door share it, so both arm
the same due for the same rows.
================
*/
func (s *MonsterState) nextBehaviorDue(state *divisionMonsterState, gid uint32, m moverSchedule, resident residentMonster, now int64) int64 {
	at := now + 1
	commandPending := resident.help.HasPending()
	if m.mode == monster.MoverPending && m.activity.Interval != 0 && !commandPending {
		elapsed := uint32(now) - m.activity.LastCheck
		if elapsed <= uint32(m.activity.Interval) {
			at = now + int64(uint32(m.activity.Interval)-elapsed) + 1
		}
		if m.arriveMs > now && m.arriveMs < at {
			at = m.arriveMs
		}
	}
	if m.mode == monster.MoverIdle && !commandPending {
		if due, gated := s.idleBehaviorDue(state, gid, m, resident, now); gated {
			at = due
		}
	}
	return at
}

/*
================
MonsterState.idleBehaviorDue

When an idle monster next needs its step, and whether that time is
knowable from the rows the scheduler already reads. The step of an
idle monster at rest can only act at three times: its idle deadline
(advanceIdle's decideIdle), its activity-gate cadence (the gate's
Due test, in the pending branch's own phase arithmetic) and, for an
aggressive nest, its acquisition timer (the sight scan; deferring
to the timer's fire time defers nothing, the check consumes the
timer when read). Everything else that can move it - damage, help,
hostility, a queued tactics event - arrives through an event door
that reschedules it at once, so sleeping until the earliest of the
three changes no outcome. Any fact this read cannot prove keeps
the next tick: a pending idle entry or stuck latch, a summoned
control or a held target (the step's own branches), a corpse or a
detached nest, an active abnormal or motion hold, a zero gate
interval, or an acquisition bank not yet created (its first check
arms the timer; the step that runs next tick creates it).

The aggressive read mirrors monster.ResolveTactics' nest branch;
the ops' TacticsFor override is a test seam the scheduler sits
below, so a nest the tables call passive keeps the gate cadence.
================
*/
func (s *MonsterState) idleBehaviorDue(state *divisionMonsterState, gid uint32, m moverSchedule, resident residentMonster, now int64) (int64, bool) {
	if m.idleEntryPending || m.stuck || m.controlMode == monster.ControlSummoned || m.targetGID != 0 {
		return 0, false
	}
	if resident.currentHP == 0 || resident.nestDetached || resident.sparse&sparseAbnormal != 0 {
		return 0, false
	}
	if resident.motion.StateAt(now) != 0 || m.activity.Interval == 0 {
		return 0, false
	}
	at := m.deadlineMs + 1
	if at < now+1 {
		at = now + 1
	}
	if elapsed := uint32(now) - m.activity.LastCheck; elapsed <= uint32(m.activity.Interval) {
		if due := now + int64(uint32(m.activity.Interval)-elapsed) + 1; due < at {
			at = due
		}
	} else {
		at = now + 1
	}
	nest := resident.nest.Value()
	if (nest.PolicyPinned || nest.RetailEvidence || nest.HasControls) && nest.Aggressive {
		due, known := s.acquisitionDue(state, gid, now)
		if !known {
			return 0, false
		}
		if due < at {
			at = due
		}
	}
	return at, true
}

/*
================
MonsterState.acquisitionDue

When the actor's acquisition timer next fires, read off its bank
without consuming it: an armed-immediate entry fires at the first
check, so the next tick; otherwise the first millisecond whose
uint32 clock reaches LastCheckMs + IntervalMs, the fire test's own
arithmetic. False when the bank does not exist yet.
================
*/
func (s *MonsterState) acquisitionDue(state *divisionMonsterState, gid uint32, now int64) (int64, bool) {
	timers := state.aiTimer(gid)
	if timers == nil {
		return 0, false
	}
	entry := timers.GetTimer(monster.TimerIDAcquisition)
	if entry.ArmedImmediate {
		return now + 1, true
	}
	if delta := entry.LastCheckMs + entry.IntervalMs - uint32(now); delta <= 1<<31 {
		return now + int64(delta), true
	}
	return now + 1, true
}

/*
================
MonsterState.scheduleBehaviorCarried

scheduleBehaviorSlot for one ref out of the batch build (#2133):
the mover schedule reads through the mover slot the ref carried
(scheduleSlotAt), the resident row through its monster slot, and
the re-arm lands through the ref's queue slot (setAt), noting the
resolved storage slots back for the next leg's drain. Every hint
is verified against the gid before use and delegates to the gid
resolve on a miss, so the armed due and the ref's slots are
scheduleBehaviorSlot's either way: ref.slot ends as the mover slot
the read resolved, behaviorNoMoverSlot when the actor has no mover
row and still joins the batch, exactly as the build stamped it
before the carry.
================
*/
func (s *MonsterState) scheduleBehaviorCarried(state *divisionMonsterState, ref *behaviorActorRef, now int64) {
	gid := ref.gid
	state.forgetDormant(gid)
	m, slot, ok := state.movers.scheduleSlotAt(ref.slot, gid)
	if !ok {
		state.behavior.removeAt(ref.queueSlot, gid)
		ref.slot = behaviorNoMoverSlot
		return
	}
	ref.slot = slot
	if s.tryDormant(state, gid, m, now) {
		return
	}
	resident, hot := state.instances.hotRowAt(ref.monsterSlot, gid)
	if !hot {
		resident, _ = state.instances.hotRow(gid)
	}
	state.behavior.setAt(*ref, s.nextBehaviorDue(state, gid, m, resident, now))
}
