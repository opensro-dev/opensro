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
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	if !state.instances.contains(gid) {
		return monster.MoverState{}, false
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
whole mover value - the read vanishInSafeZone weighs once per advanced
actor. A gid the mover storage cannot place reads through the same
synthesized spawn mover Mover would build, so the pose is Mover's.
================
*/
func (s *MonsterState) livePose(divisionID string, gid uint32, nowMs int64) (monster.Pose, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	if pose, ok := state.movers.livePoseAt(gid, nowMs, nil); ok {
		return pose, true
	}
	instance, ok := state.instances.lookup(gid)
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
	return s.commitMoverChecked(state, gid, mover, 0, false)
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
================
*/
func (s *MonsterState) commitMoverChecked(state *divisionMonsterState, gid uint32, mover monster.MoverState, baseGen uint64, guarded bool) bool {
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.Motion.StateAt(s.nowMillis()) != 0 || (instance.Ref.MaxHP > 0 && instance.CurrentHP == 0) {
		return false
	}
	if state.movers == nil {
		state.movers = newMoverStorage(nil)
	}
	if guarded && state.movers.generation(gid) != baseGen {
		return false
	}
	// Packet-side damage can arm retaliation between a tick's snapshot read
	// and commit. Reject only a plan from an older retaliation revision.
	previous, hadPrevious := state.movers.lookup(gid)
	if hadPrevious && previous.RetaliationRevision() > mover.RetaliationRevision() {
		return false
	}
	// Entry effects belong to the accepted transition, never its planner.
	// A stale FOLLOW plan must not reset a timer while retaliation owns BATTLE.
	if mover.Mode() == monster.MoverFollowing && (!hadPrevious || previous.Mode() != monster.MoverFollowing) {
		s.beginFollowCadenceLocked(state, gid)
	}
	state.movers.set(gid, mover)
	state.syncApproachActor(gid, mover)
	state.behavior.set(gid, 0)
	if mover.TargetGID() == 0 && mover.Mode() != monster.MoverPending && mover.Mode() != monster.MoverWandering && mover.Mode() != monster.MoverFollowing {
		// Releasing target ownership (the retail server's target-change message
		// with a zero target) clears both records.
		instance.Opponents = [2]monster.Opponent{}
	} else if mover.TargetGID() != 0 && instance.Opponents[0].GID == 0 {
		// Local acquisition can establish a primary without a preceding hit.
		instance.Opponents[0] = monster.Opponent{GID: mover.TargetGID(), LastHitMs: uint32(s.nowMillis())}
	}
	state.instances.set(gid, instance)
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
	if err := mover.Validate(); err != nil {
		panic(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	if !s.commitMoverChecked(state, gid, mover, baseGen, true) {
		return state.movers.generation(gid), false
	}
	return state.movers.generation(gid), true
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
MoverGeneration

One actor's mover value with the commit generation it was read at:
the re-read a step makes after a transaction of its own (an idle
entry, the activity gate) has already advanced the row.
================
*/
func (s *MonsterState) MoverGeneration(divisionID string, gid uint32) (monster.MoverState, uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	if !state.instances.contains(gid) {
		return monster.MoverState{}, 0, false
	}
	if mover, ok := state.movers.lookup(gid); ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return mover, state.movers.generation(gid), true
	}
	mover := monster.NewSpawnMover(state.instances.get(gid), s.nowMillis())
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	return mover, state.movers.generation(gid), true
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
