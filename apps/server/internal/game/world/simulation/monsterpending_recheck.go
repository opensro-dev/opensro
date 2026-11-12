/*
===========================================================================

monsterpending_recheck.go - the suspended monster's activity re-check,
answered for a whole batch at once

A suspended (PENDING) monster asks one question every one to two
seconds: has a player come into my message block? Nearly every time
the answer is no, and the monster's whole step is then one write -
the cadence stamp - and one re-arm. The live profile of 2026-11-10
(105 players, 61 ms a tick) found most of the 2,700 actors a leg
steps to be exactly these: each paid the 2.7 KB snapshot fill, its
own entry into the division lock and the step's prologue to reach
that one write.

The lane here answers the question for every such actor of a batch
in one pass, before the batch's snapshot is built, and takes the
settled ones out of the batch. It writes what the step writes,
through the doors the step uses (the cadence stamp and the
commit's own reschedule), so the stored rows, the armed dues and
the published frames are the full step's. An actor the lane cannot
prove quiet stays in the batch and steps exactly as before: the
lane holds no second copy of any rule, only the proof that none of
them can fire.

What the proof reads, and the step line each fact disarms:

  - the mover is stored compact (moverRecord.pending): PENDING or
    SPAWNING with no segment, no navigation intent, no target and
    no controller (monster.PendingSnapshot), so the gate's arrival
    and continuation tails are nil;
  - it is not spawning, not stuck, and its cadence is due (a
    cadence not yet due belongs to the step's own early return);
  - the instance is alive, holds no motion state, no pending help
    command, no queued tactics event, no transient effect row, and
    is no training dummy (the step skips those before any write);
  - its nest is detached or on its own plane, so the cross-plane
    detach is a no-op on its face;
  - it does not stand in a safe zone (the step's exit would
    defeat it);
  - the leg's census counts no admitted player into its block, or
    cannot resolve the block: the gate's own two "stay suspended"
    answers. A counted block is the resume, which is the step's.

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

/*
================
pendingRecheck

One due suspended actor as the lane weighs it: where it sits in the
batch, the verified slots its rows were read through, the commit
generation and cadence the proof read, and the pose its block
resolves from (the commit refuses when any of them has moved).
================
*/
type pendingRecheck struct {
	index       int
	gid         uint32
	slot        uint32
	monsterSlot uint32
	queueSlot   uint32
	gen         uint64
	activity    monster.ActivityCadence
	pose        monster.Pose
	settle      bool
}

/*
================
moverStorage.pendingRecheckAt

The mover half of the lane's proof, read off the stored record
without expanding it. The slot is verified against the gid and
resolved by gid on a miss, as every carried read is (#2133). ok is
false for any row the lane must leave to the step: a live record
(it holds a segment, a goal, a target or a mode the compact form
refuses), a spawning or stuck one, a cadence that was never armed
or is not yet due.
================
*/
func (s *moverStorage) pendingRecheckAt(slot uint32, gid uint32, now uint32) (uint32, uint64, monster.ActivityCadence, monster.Pose, bool) {
	if s == nil {
		return 0, 0, monster.ActivityCadence{}, monster.Pose{}, false
	}
	if int(slot) >= len(s.slots) || s.slots[slot].gid != gid {
		resolved, found := s.slotOf(gid)
		if !found {
			return 0, 0, monster.ActivityCadence{}, monster.Pose{}, false
		}
		slot = resolved
	}
	held := &s.slots[slot]
	pending := held.rec.pending
	if pending == nil || pending.spawning || pending.activity.Interval == 0 {
		return 0, 0, monster.ActivityCadence{}, monster.Pose{}, false
	}
	// ActivityCadence.Due's own test, unsigned as the gate's clock is.
	if now-pending.activity.LastCheck <= uint32(pending.activity.Interval) {
		return 0, 0, monster.ActivityCadence{}, monster.Pose{}, false
	}
	if pending.stuck {
		return 0, 0, monster.ActivityCadence{}, monster.Pose{}, false
	}
	return slot, held.gen, pending.activity, pending.pose, true
}

/*
================
monsterStorage.quietAt

The instance half of the lane's proof, read off the hot row in
place. region is where the mover stands. ok is false when any
step line ahead of the gate could act on the actor, and for an
actor that is not resident.
================
*/
func (s *monsterStorage) quietAt(slot uint32, gid uint32, now int64, region uint16) (uint32, bool) {
	if slot >= uint32(len(s.hot)) || s.hot[slot].gid != gid {
		resolved, found := s.hotSlotOf(gid)
		if !found {
			return 0, false
		}
		slot = resolved
	}
	held := &s.hot[slot]
	row := &held.row
	if held.trainingDummy || row.sparse != 0 || row.help.HasPending() || row.motion.StateAt(now) != 0 {
		return 0, false
	}
	if row.currentHP == 0 && held.maxHP > 0 {
		return 0, false
	}
	// detachCrossPlaneNest's own face test: only the indoor bit of
	// the two regions is weighed.
	if !row.nestDetached && (held.nestRegion^region)&0x8000 != 0 {
		return 0, false
	}
	return slot, true
}

/*
================
MonsterState.pendingRecheckCandidates

The batch's due actors the rows prove quiet, in batch order, appended
to out. One locked pass; the census half of the proof is the
caller's, outside this lock.
================
*/
func (s *MonsterState) pendingRecheckCandidates(key populationKey, refs []behaviorActorRef, now int64, out []pendingRecheck) []pendingRecheck {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		return out
	}
	for index, ref := range refs {
		slot, gen, activity, pose, pending := state.movers.pendingRecheckAt(ref.slot, ref.gid, uint32(now))
		if !pending {
			continue
		}
		monsterSlot, quiet := state.instances.quietAt(ref.monsterSlot, ref.gid, now, pose.RegionID)
		if !quiet || len(state.aiEvents[ref.gid]) != 0 {
			continue
		}
		out = append(out, pendingRecheck{index: index, gid: ref.gid, slot: slot, monsterSlot: monsterSlot,
			queueSlot: ref.queueSlot, gen: gen, activity: activity, pose: pose})
	}
	return out
}

/*
================
MonsterState.settlePendingRechecks

The gate's cadence commit for every candidate the census settled:
the stamp and the reschedule commitActivityAt's cadence-only branch
makes. The candidate list was read in an earlier locked pass, so
each proof is made again here, whole, in the same critical section
as its write: the mover half must read back the generation, cadence
and pose the census weighed, and the instance half and the event
queue must still be quiet. A candidate that fails any of it is
refused and keeps its place in the batch (settle is cleared), where
the step meets it on its leg-start snapshot as it always has.
Returns how many settled.
================
*/
func (s *MonsterState) settlePendingRechecks(key populationKey, candidates []pendingRecheck, now int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	settled := 0
	for i := range candidates {
		candidate := &candidates[i]
		if !candidate.settle {
			continue
		}
		if state == nil || !state.pendingRecheckHolds(candidate, now) {
			candidate.settle = false
			continue
		}
		activity := candidate.activity
		activity.LastCheck = uint32(now)
		state.movers.setActivityAt(candidate.slot, candidate.gid, activity)
		// The written row's schedule, read back off the record the
		// stamp just landed on: a pending row's mode, cadence and
		// region (scheduleRow), which is scheduleOf of its expansion.
		schedule, _, _ := state.movers.scheduleSlotAt(candidate.slot, candidate.gid)
		s.scheduleBehaviorCommitted(state, candidate.gid, schedule, candidate.monsterSlot, candidate.queueSlot, now)
		settled++
	}
	return settled
}

/*
================
divisionMonsterState.pendingRecheckHolds

Whether the rows still prove what the candidate was read as. The
mover half is the generation guard the commit doors use, not a
second run of the candidate probe: an unchanged generation says
no whole-value write touched the row (setRow is their only door
and advances it), so the record's form, its spawning and stuck
bits and its interned row are still the ones the candidate read,
and the cadence and pose compares cover the two writes that leave
the generation alone (stampActivity, catchUpActivity) - with the
cadence unchanged at the same leg clock, the due test answers as
it did. The instance half is re-probed whole: help, motion and
life land there from other actors' turns without touching the
mover row.
================
*/
func (state *divisionMonsterState) pendingRecheckHolds(candidate *pendingRecheck, now int64) bool {
	movers := state.movers
	if movers == nil || int(candidate.slot) >= len(movers.slots) || movers.slots[candidate.slot].gid != candidate.gid {
		return false
	}
	held := &movers.slots[candidate.slot]
	if held.gen != candidate.gen {
		return false
	}
	pending := held.rec.pending
	if pending == nil || pending.activity != candidate.activity || pending.pose != candidate.pose {
		return false
	}
	monsterSlot, quiet := state.instances.quietAt(candidate.monsterSlot, candidate.gid, now, candidate.pose.RegionID)
	return quiet && monsterSlot == candidate.monsterSlot && len(state.aiEvents[candidate.gid]) == 0
}

/*
================
MonsterMoverOps.recheckPendingBatch

Run the lane over one batch and return the refs that still need
their step, in their order. The candidates are read first, outside
the division's operation lock; a batch that has any then makes one
entry into that lock, where each of these actors took its own
before, and weighs and commits them all inside it. It is the same
sequence of commits as if the settled actors had stepped first,
back to back. A batch with no candidate enters no lock here at all.
The census is the leg's, already asked for this batch's world.
================
*/
func (ops *MonsterMoverOps) recheckPendingBatch(batch behaviorBatch, nowMs int64, stats *legStats) []behaviorActorRef {
	if ops.activity == nil || ops.stepPendingRechecks || len(batch.actors) == 0 {
		return batch.actors
	}
	candidates := ops.Monsters.pendingRecheckCandidates(batch.key, batch.actors, nowMs, ops.recheckScratch[:0])
	ops.recheckScratch = candidates
	if len(candidates) == 0 {
		return batch.actors
	}
	if ops.RunAction == nil {
		return ops.settlePendingBatch(batch, candidates, nowMs, stats)
	}
	remaining := batch.actors
	ops.RunAction(batch.key.division, func(MonsterAttackOperation) {
		remaining = ops.settlePendingBatch(batch, candidates, nowMs, stats)
	})
	return remaining
}

/*
================
MonsterMoverOps.settlePendingBatch

The lane's census verdict and commit for one batch's candidates,
with the division's operation lock held when the leg runs under
one. A candidate in a safe zone is left for the step's exit to
defeat; one whose block the census counts is left for the step to
resume. The rest stay suspended: the gate's own answer for an
uncounted block and for a block or a world it cannot resolve.
================
*/
func (ops *MonsterMoverOps) settlePendingBatch(batch behaviorBatch, candidates []pendingRecheck, nowMs int64, stats *legStats) []behaviorActorRef {
	unresolved := ops.activity.unresolved[ops.activity.world]
	weighed := 0
	for i := range candidates {
		candidate := &candidates[i]
		if SafeZoneRegion(candidate.pose.RegionID) {
			continue
		}
		block, ok := ops.activityBlockFor(candidate.gid, poseToSpawn(candidate.pose))
		if ok && !unresolved && ops.activity.countedHere(block) {
			continue
		}
		candidate.settle = true
		weighed++
	}
	if weighed == 0 || ops.Monsters.settlePendingRechecks(batch.key, candidates, nowMs) == 0 {
		return batch.actors
	}
	// Take the settled refs out in place: candidates are in batch
	// order, so one forward walk pairs them with their refs.
	remaining := batch.actors[:0]
	next := 0
	for index, ref := range batch.actors {
		if next < len(candidates) && candidates[next].index == index {
			settled := candidates[next].settle
			next++
			if settled {
				stats.noteRechecked()
				if ops.noteResumed(batch.key, ref.gid) {
					stats.resumed++
				}
				continue
			}
		}
		remaining = append(remaining, ref)
	}
	return remaining
}
