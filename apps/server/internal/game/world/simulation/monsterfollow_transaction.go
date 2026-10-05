package simulation

import "opensro.online/server/internal/game/world/monster"

// One detached FOLLOW decision. MonsterState owns the live timer banks and
// movement; callbacks/terrain sampling must never execute under its mutex.
// Value snapshots let the final commit reject leader changes as well as damage.
type monsterFollowPlan struct {
	division             string
	instance             monster.Instance
	mover                monster.MoverState
	leader               monster.MoverState
	leaderHP             uint32
	leaderRadius         float64
	leaderExists         bool
	timersBefore, timers monster.AITimeManager
}

func (s *MonsterState) prepareFollow(division string, expected monster.Instance, mover monster.MoverState, nowMs int64) (monsterFollowPlan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, expected.Gid)
	instance, exists := state.instances.lookup(expected.Gid)
	if !exists || instance.CurrentHP == 0 || instance != expected ||
		state.movers.get(expected.Gid) != mover {
		return monsterFollowPlan{}, false
	}
	plan := monsterFollowPlan{division: division, instance: instance, mover: mover}
	parent, exists := state.instances.lookup(mover.ControllerGID())
	leader, moving := state.movers.lookup(mover.ControllerGID())
	plan.leaderExists = exists && moving
	plan.leader, plan.leaderHP = leader, parent.CurrentHP
	plan.leaderRadius = parent.BodyRadius()
	plan.timersBefore = *s.aiTimersLocked(state, instance, nowMs)
	plan.timers = plan.timersBefore
	return plan, true
}

// commitFollow admits scan cadence, movement, and entry effects together.
// Negative eligibility is an accepted scan and consumes its gate; a stale
// snapshot is rejected and consumes nothing. Never roll back a live timer.
func (s *MonsterState) commitFollow(plan monsterFollowPlan, mover monster.MoverState, frames []Frame) ([]Frame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(plan.division, plan.instance.Gid)
	current, exists := state.instances.lookup(plan.instance.Gid)
	parent, parentExists := state.instances.lookup(plan.mover.ControllerGID())
	leader, leaderExists := state.movers.lookup(plan.mover.ControllerGID())
	timers := state.aiTimer(plan.instance.Gid)
	if !exists || current.CurrentHP == 0 || current != plan.instance ||
		state.movers.get(current.Gid) != plan.mover || timers == nil || *timers != plan.timersBefore ||
		(parentExists && leaderExists) != plan.leaderExists || parent.CurrentHP != plan.leaderHP || parent.BodyRadius() != plan.leaderRadius || leader != plan.leader {
		return nil, false
	}
	// No callbacks follow validation. Publish staged cadence before entry;
	// entry may replace Timer 0, while Timer 7 must retain its scan timestamp.
	*timers = plan.timers
	if mover != plan.mover && !s.commitMoverLocked(state, current.Gid, mover) {
		panic("validated FOLLOW transaction lost admission under owner lock")
	}
	return frames, true
}
