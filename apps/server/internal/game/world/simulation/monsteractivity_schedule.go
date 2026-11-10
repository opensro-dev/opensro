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
	resident, _ := state.instances.hotRowPtr(gid)
	state.behavior.set(gid, s.nextBehaviorDue(state, gid, m, resident, now))
	return slot
}

/*
================
MonsterState.scheduleBehaviorCommitted

scheduleBehaviorSlot for the activity commit's own reschedule
(monsteractivity.go's commitActivityAt): the commit has just
written the mover row under the state lock it still holds, so
the caller hands over the written row's schedule (scheduleOf)
and no mover read remains - post-write, the stored row is the
handed value on both write paths (the cadence path's stored row
differs from it only in the stamp the commit just set, which is
the stamp the schedule carries). The old scheduleSlotAt miss
branch (retire an actor whose row is gone) cannot fire here:
the row exists because the commit wrote it. The resident row
still resolves through the verified monster-slot hint the
commit carried, delegating to the gid resolve on a miss, so
the armed due is scheduleBehaviorSlot's either way. The re-arm
itself lands through the queue slot the caller's snapshot carried
(setAtSlot), verified against the gid and delegating to
behavior.set when the caller holds none, so the commit's arm
resolves no gid through byGID either (#3027).
================
*/
func (s *MonsterState) scheduleBehaviorCommitted(state *divisionMonsterState, gid uint32, m moverSchedule, monsterSlot uint32, queueSlot uint32, now int64) {
	state.forgetDormant(gid)
	if s.tryDormant(state, gid, m, now) {
		return
	}
	resident, hot := state.instances.hotRowPtrAt(monsterSlot, gid)
	if !hot {
		resident, _ = state.instances.hotRowPtr(gid)
	}
	state.behavior.setAtSlot(queueSlot, gid, s.nextBehaviorDue(state, gid, m, resident, now))
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
func (s *MonsterState) nextBehaviorDue(state *divisionMonsterState, gid uint32, m moverSchedule, resident *residentMonster, now int64) int64 {
	at := now + 1
	commandPending := resident != nil && resident.help.HasPending()
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
idle monster can only act at three times: its idle deadline
(advanceIdle's decideIdle); the arrival of a segment it is still
gliding on (a wander that expired mid-flight enters IDLE without
stopping, and the step settles the arrival with its 0xB2F5); and,
for an aggressive nest, its acquisition timer (the sight scan;
deferring to the timer's fire time defers nothing, the check
consumes the timer when read). Everything else that can move it -
damage, help, hostility, a queued tactics event - arrives through
an event door that reschedules it at once, so sleeping until the
earliest of the three changes no outcome. Any fact this read cannot
prove keeps the next tick: a pending idle entry or stuck latch, a
summoned control or a held target (the step's own branches), a
held navigation goal (the step commits its next segment), a corpse
or a detached nest, an active abnormal or motion hold, a zero gate
interval, or an acquisition bank not yet created (its first check
arms the timer; the step that runs next tick creates it).

The activity cadence is not one of those times. The gate that
reads it acts only on a wandering or a pending monster
(runActivityGate, monster.MaySuspendWander): for an idle one it
returns before the cadence is looked at, and nothing stamps the
cadence while the monster idles. This read used to wake on it all
the same, and because an idle row's cadence is never stamped, a
monster whose one-to-two-second cadence had lapsed was stepped
every tick for the rest of its two-to-six-second idle, to return
nil each time.

The aggressive read mirrors monster.ResolveTactics' nest branch,
which is what the step resolves in production. An ops whose
TacticsFor replaces that resolver (fixtures only) can make the step
aggressive where the tables say passive; the scheduler sits below
the ops and cannot see it, so while a replacement is installed
(tacticsReplaced) the cadence wake is kept as the conservative
bound it always was for those fixtures.
================
*/
func (s *MonsterState) idleBehaviorDue(state *divisionMonsterState, gid uint32, m moverSchedule, resident *residentMonster, now int64) (int64, bool) {
	if m.idleEntryPending || m.stuck || m.controlMode == monster.ControlSummoned || m.targetGID != 0 || m.goalHeld {
		return 0, false
	}
	if resident == nil || resident.currentHP == 0 || resident.nestDetached || resident.sparse&sparseAbnormal != 0 {
		return 0, false
	}
	if resident.motion.StateAt(now) != 0 || m.activity.Interval == 0 {
		return 0, false
	}
	at := m.deadlineMs + 1
	if at < now+1 {
		at = now + 1
	}
	if m.segmentHeld {
		// The settle falls on the first step at or past the arrival;
		// a matured segment not yet settled is due at once.
		due := m.arriveMs
		if due < now+1 {
			due = now + 1
		}
		if due < at {
			at = due
		}
	}
	if s.tacticsReplaced {
		if elapsed := uint32(now) - m.activity.LastCheck; elapsed <= uint32(m.activity.Interval) {
			if due := now + int64(uint32(m.activity.Interval)-elapsed) + 1; due < at {
				at = due
			}
		} else {
			at = now + 1
		}
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
MonsterState.noteTacticsReplaced

Whether the running leg's ops resolves tactics through a replacement
of monster.ResolveTactics (fixtures only). Written by the leg before
it schedules anything; idleBehaviorDue reads it under the same lock.
================
*/
func (s *MonsterState) noteTacticsReplaced(replaced bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tacticsReplaced = replaced
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
	resident, hot := state.instances.hotRowPtrAt(ref.monsterSlot, gid)
	if !hot {
		resident, _ = state.instances.hotRowPtr(gid)
	}
	state.behavior.setAt(*ref, s.nextBehaviorDue(state, gid, m, resident, now))
}
