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
	// Packet-side damage can arm retaliation between a tick's snapshot read
	// and commit. Reject only a plan from an older retaliation revision.
	if current, ok := state.movers.lookup(gid); ok &&
		current.RetaliationRevision() > mover.RetaliationRevision() {
		return false
	}
	// Entry effects belong to the accepted transition, never its planner.
	// A stale FOLLOW plan must not reset a timer while retaliation owns BATTLE.
	if previous, exists := state.movers.lookup(gid); mover.Mode() == monster.MoverFollowing &&
		(!exists || previous.Mode() != monster.MoverFollowing) {
		s.beginFollowCadenceLocked(state, gid)
	}
	state.movers.set(gid, mover)
	state.syncApproachActor(gid, mover)
	state.behavior.set(gid, 0)
	if mover.TargetGID() == 0 && mover.Mode() != monster.MoverPending && mover.Mode() != monster.MoverWandering && mover.Mode() != monster.MoverFollowing {
		// 53FFE0(0): releasing target ownership clears both records.
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
	// 5474B6..5474F0: retain the first two distinct hit identities. Repeated
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
