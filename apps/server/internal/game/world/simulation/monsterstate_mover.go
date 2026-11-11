/*
===========================================================================

monsterstate_mover.go - simulation monster state mover ownership

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

// Mover returns a value snapshot of an instance's movement state.
/*
================
Mover
================
*/
func (s *MonsterState) Mover(divisionID string, gid uint32) (monster.MoverState, bool) {
	return s.MoverAt(divisionID, gid, behaviorNoMoverSlot, behaviorNoMonsterSlot)
}

/*
================
moverStuckAt

MoverAt(...).Stuck() without building the MoverState. resolveStuck asks
it for every actor at the end of every step and the answer is almost
always no; the gid door it used was 12% of the seeded leg's samples
(#2133). Same population, same existence probe and the same carried
slots as MoverAt; an actor with no row yet answers no, because the
spawn mover MoverAt would synthesize starts unstuck. The full row is
validated by resolveStuck's own Mover read, which runs only on a yes.
================
*/
func (s *MonsterState) moverStuckAt(divisionID string, gid uint32, slot, monsterSlot uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObjectAt(divisionID, gid, monsterSlot)
	if !state.instances.containsAt(monsterSlot, gid) {
		return false
	}
	stuck, _ := state.movers.stuckAt(slot, gid)
	return stuck
}

/*
================
MoverAt

Mover through the slots the behaviour batch carried for the actor
(#2133): the existence probe and the row read go through the
verified hints and fall back to the gid resolve whenever a hint no
longer names the gid, so the value is Mover's either way. The
synthesized spawn mover of an actor with no row yet reads its
instance by gid, as Mover does: that is the spawn window, not the
step's steady state.
================
*/
func (s *MonsterState) MoverAt(divisionID string, gid uint32, slot, monsterSlot uint32) (monster.MoverState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObjectAt(divisionID, gid, monsterSlot)
	if !state.instances.containsAt(monsterSlot, gid) {
		return monster.MoverState{}, false
	}
	if mover, ok := state.movers.lookupAt(slot, gid); ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return mover, true
	}
	if mover, ok := state.movers.lookup(gid); ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return mover, true
	}
	mover := monster.NewSpawnMover(state.instances.get(gid), s.nowMillis())
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	return mover, true
}

/*
================
livePose

livePose reads one actor's live pose without expanding or validating a
whole mover value - the storage read vanishInSafeZone falls back to
when the step moved the actor off its snapshot mover. A gid the mover
storage cannot place reads through the same
synthesized spawn mover Mover would build, so the pose is Mover's.
================
*/
func (s *MonsterState) livePose(divisionID string, gid uint32, nowMs int64) (monster.Pose, bool) {
	return s.livePoseAt(divisionID, gid, behaviorNoMoverSlot, behaviorNoMonsterSlot, nowMs)
}

/*
================
livePoseAt

livePose through the slots the behaviour batch carried for the
actor (#2133): the population, the mover row and the instance the
spawn synthesis reads go through the verified hints and fall back
to the gid resolve whenever a hint no longer names the gid, so
the pose is livePose's either way.
================
*/
func (s *MonsterState) livePoseAt(divisionID string, gid uint32, slot, monsterSlot uint32, nowMs int64) (monster.Pose, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObjectAt(divisionID, gid, monsterSlot)
	if pose, ok := state.movers.livePoseAtSlot(slot, gid, nowMs, nil); ok {
		return pose, true
	}
	instance, ok := state.instances.lookupAt(monsterSlot, gid)
	if !ok {
		instance, ok = state.instances.lookup(gid)
	}
	if !ok {
		return monster.Pose{}, false
	}
	return monster.NewSpawnMover(instance, s.nowMillis()).LivePoseAt(nowMs, nil), true
}

// CommitMover replaces an instance's mover value. Whole-value commits keep
// mutation behind the simulation authority boundary. True means this value
// was accepted, not merely that its GID exists. Frame-producing planners must
// use CommitMoverFrames; attack planners must obtain admission before damage.
/*
================
CommitMover
================
*/
func (s *MonsterState) CommitMover(divisionID string, gid uint32, mover monster.MoverState) bool {
	if err := mover.Validate(); err != nil {
		panic(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	return s.commitMoverLocked(state, gid, mover)
}

// Caller holds s.mu; FOLLOW commits use this same state/entry-effect owner.
/*
================
commitMoverLocked
================
*/
func (s *MonsterState) commitMoverLocked(state *divisionMonsterState, gid uint32, mover monster.MoverState) bool {
	return s.commitMoverChecked(state, gid, behaviorNoMoverSlot, mover, 0, false)
}

/*
================
commitMoverChecked

commitMoverLocked with the leg's generation guard (#2137 slice 1):
when guarded, the commit lands only while the stored row still sits
at baseGen - the generation the planner's snapshot read. A slower
lane's set advances the row, the stale commit drops, and the actor
replans at its next due; nothing of the newer value is overwritten.
Returns the row's generation after the attempt (the new generation
on acceptance, the current one on a generation drop).

The guards and the opponents bookkeeping read five scalars of the
actor (motion state, max HP, current HP, the two opponent records)
plus the previous row's retaliation revision and mode. A hot actor
answers all of them off the resident rows, so the commit expands
neither the Instance nor the previous MoverState (#3027); only a
cold actor, or an opponents change on a hot one, pays lookup's
expansion. The instance write-back the commit always ran rewrote
the value the guards had just read whenever neither opponents
branch moved a record, so it runs only on a change now, writing
the same value it wrote before; a cold actor's write-back still
always runs, keeping its wake into the hot tier.
================
*/
func (s *MonsterState) commitMoverChecked(state *divisionMonsterState, gid uint32, slot uint32, mover monster.MoverState, baseGen uint64, guarded bool) bool {
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	row, hot := state.instances.hotRow(gid)
	var instance monster.Instance
	var opponents [2]monster.Opponent
	if hot {
		if row.motion.StateAt(s.nowMillis()) != 0 || (row.ref.Value().MaxHP > 0 && row.currentHP == 0) {
			return false
		}
		opponents = row.opponents
	} else {
		var ok bool
		instance, ok = state.instances.lookup(gid)
		if !ok || instance.Motion.StateAt(s.nowMillis()) != 0 || (instance.Ref.MaxHP > 0 && instance.CurrentHP == 0) {
			return false
		}
		opponents = instance.Opponents
	}
	if state.movers == nil {
		state.movers = newMoverStorage(nil)
	}
	if guarded && state.movers.generationAt(slot, gid) != baseGen {
		return false
	}
	// Packet-side damage can arm retaliation between a tick's snapshot read
	// and commit. Reject only a plan from an older retaliation revision.
	previousRev, previousMode, hadPrevious := state.movers.retaliationAt(slot, gid)
	if hadPrevious && previousRev > mover.RetaliationRevision() {
		return false
	}
	// Entry effects belong to the accepted transition, never its planner.
	// A stale FOLLOW plan must not reset a timer while retaliation owns BATTLE.
	if mover.Mode() == monster.MoverFollowing && (!hadPrevious || previousMode != monster.MoverFollowing) {
		s.beginFollowCadenceLocked(state, gid)
	}
	state.movers.set(gid, mover)
	state.syncApproachActor(gid, mover)
	state.behavior.set(gid, 0)
	opponentsChanged := false
	if mover.TargetGID() == 0 && mover.Mode() != monster.MoverPending && mover.Mode() != monster.MoverWandering && mover.Mode() != monster.MoverFollowing {
		// Releasing target ownership (the retail server's target-change message
		// with a zero target) clears both records.
		if opponents != ([2]monster.Opponent{}) {
			opponents = [2]monster.Opponent{}
			opponentsChanged = true
		}
	} else if mover.TargetGID() != 0 && opponents[0].GID == 0 {
		// Local acquisition can establish a primary without a preceding hit.
		opponents[0] = monster.Opponent{GID: mover.TargetGID(), LastHitMs: uint32(s.nowMillis())}
		opponentsChanged = true
	}
	if !hot {
		instance.Opponents = opponents
		state.instances.set(gid, instance)
	} else if opponentsChanged {
		if current, ok := state.instances.lookup(gid); ok {
			current.Opponents = opponents
			state.instances.set(gid, current)
		}
	}
	return true
}

// CommitMoverFrames is the publication boundary for movement-only decisions.
// Building frames is pure; only an accepted owner transaction can release them
// to the tick's pusher. Do not call CommitMover and then return unchecked frames.
/*
================
CommitMoverFrames
================
*/
func (s *MonsterState) CommitMoverFrames(divisionID string, gid uint32, mover monster.MoverState, frames []Frame) []Frame {
	if !s.CommitMover(divisionID, gid, mover) {
		return nil
	}
	return frames
}

/*
================
CommitMoverGen

CommitMover guarded by the mover row's commit generation: the
commit lands only while the row still sits at baseGen, the
generation the caller's snapshot read (#2137 slice 1). Returns the
row's generation after the attempt alongside acceptance, so a
step committing several times in a row re-bases on its own accepted
commits while an intervening slower-lane set drops the next one.
================
*/
func (s *MonsterState) CommitMoverGen(divisionID string, gid uint32, mover monster.MoverState, baseGen uint64) (uint64, bool) {
	return s.CommitMoverGenAt(divisionID, gid, behaviorNoMoverSlot, behaviorNoMonsterSlot, mover, baseGen)
}

/*
================
CommitMoverGenAt

CommitMoverGen through the slots the step's snapshot carried for
the actor (#2133): the population resolve, the commit's guard
reads and the generation answers go through the verified hints
and fall back to the gid resolve whenever a hint no longer names
the gid, so the commit and its returned generation are
CommitMoverGen's either way. A mover commit runs per acting actor
per leg; its gid probes were the store map walk this removes
(#3027).
================
*/
func (s *MonsterState) CommitMoverGenAt(divisionID string, gid uint32, slot, monsterSlot uint32, mover monster.MoverState, baseGen uint64) (uint64, bool) {
	if err := mover.Validate(); err != nil {
		panic(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObjectAt(divisionID, gid, monsterSlot)
	if !s.commitMoverChecked(state, gid, slot, mover, baseGen, true) {
		return state.movers.generationAt(slot, gid), false
	}
	return state.movers.generationAt(slot, gid), true
}

/*
================
CommitMoverFramesGen

CommitMoverFrames with CommitMoverGen's generation guard: frames
are released only for a commit that landed on its base generation.
================
*/
func (s *MonsterState) CommitMoverFramesGen(divisionID string, gid uint32, mover monster.MoverState, baseGen uint64, frames []Frame) []Frame {
	if _, ok := s.CommitMoverGen(divisionID, gid, mover, baseGen); !ok {
		return nil
	}
	return frames
}

/*
================
CommitMoverFramesGenAt

CommitMoverFramesGen through the slots the step's snapshot
carried, as CommitMoverGenAt.
================
*/
func (s *MonsterState) CommitMoverFramesGenAt(divisionID string, gid uint32, slot, monsterSlot uint32, mover monster.MoverState, baseGen uint64, frames []Frame) []Frame {
	if _, ok := s.CommitMoverGenAt(divisionID, gid, slot, monsterSlot, mover, baseGen); !ok {
		return nil
	}
	return frames
}

/*
================
MoverGeneration

One actor's mover value with the commit generation it was read at:
the re-read a step makes after a transaction of its own (an idle
entry, the activity gate) has already advanced the row.
================
*/
func (s *MonsterState) MoverGeneration(divisionID string, gid uint32) (monster.MoverState, uint64, bool) {
	return s.MoverGenerationAt(divisionID, gid, behaviorNoMoverSlot, behaviorNoMonsterSlot)
}

/*
================
MoverGenerationAt

MoverGeneration through the slots the behaviour batch carried for
the actor (#2133): the existence probe, the row read and the
generation read go through the verified hints and fall back to the
gid resolve whenever a hint no longer names the gid, so the value
and the generation are MoverGeneration's either way.
generationAt answers for whichever row the read found: the hinted
slot's row when the hint held, the gid's row when it did not.
================
*/
func (s *MonsterState) MoverGenerationAt(divisionID string, gid uint32, slot, monsterSlot uint32) (monster.MoverState, uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObjectAt(divisionID, gid, monsterSlot)
	if !state.instances.containsAt(monsterSlot, gid) {
		return monster.MoverState{}, 0, false
	}
	if mover, ok := state.movers.lookupAt(slot, gid); ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return mover, state.movers.generationAt(slot, gid), true
	}
	if mover, ok := state.movers.lookup(gid); ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return mover, state.movers.generationAt(slot, gid), true
	}
	mover := monster.NewSpawnMover(state.instances.get(gid), s.nowMillis())
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	return mover, state.movers.generationAt(slot, gid), true
}

/*
================
moverGenerationAt

The mover row's commit generation alone, through the slots the
behaviour batch carried for the actor (#2133): the witness
vanishInSafeZone weighs to tell whether the step moved the actor
off the mover it was snapshot at. A hint that no longer names the
gid falls back to the gid's generation, and an actor the
population no longer holds answers not-ok, so either doubt sends
the caller to the stored pose.
================
*/
func (s *MonsterState) moverGenerationAt(divisionID string, gid uint32, slot, monsterSlot uint32) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObjectAt(divisionID, gid, monsterSlot)
	if !state.instances.containsAt(monsterSlot, gid) {
		return 0, false
	}
	return state.movers.generationAt(slot, gid), true
}

// ArmRetaliation makes a surviving monster pursue the player who damaged it.
// The state lock keeps this edge coherent with whole-value mover commits.
/*
================
ArmRetaliation
================
*/
func (s *MonsterState) ArmRetaliation(divisionID string, gid, attackerGID uint32) bool {
	if attackerGID == 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 || instance.Ref.Structure {
		return false
	}
	if state.movers == nil {
		state.movers = newMoverStorage(nil)
	}
	mover, ok := state.movers.lookup(gid)
	if !ok {
		mover = monster.NewSpawnMover(instance, s.nowMillis())
	}
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	// The retail server retains the first two distinct hit identities. Repeated
	// hits keep their slot and a third attacker does not evict either record.
	for index, opponent := range instance.RememberedOpponents() {
		if opponent == attackerGID {
			break
		}
		if opponent == 0 {
			instance.Opponents[index] = monster.Opponent{GID: attackerGID, LastHitMs: uint32(s.nowMillis())}
			break
		}
	}
	state.instances.set(gid, instance)
	if mover.TargetGID() == attackerGID && mover.Retaliating() &&
		(mover.Mode() == monster.MoverChasing || mover.Mode() == monster.MoverAttacking) {
		return true
	}

	if err := mover.Transition(monster.MoverEventRetaliationArmed, attackerGID); err != nil {
		panic(err)
	}
	state.movers.set(gid, mover)
	state.syncApproachActor(gid, mover)
	state.behavior.set(gid, 0)
	return true
}
