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

// followLeaderFacts is one leg's resolved leader reading for a
// summoner gid: the leader's mover row folded with the instance
// facts commitFollow re-derives. Followers of one leader prepare
// once per leg between them (#2136); the commit still re-reads, so
// sharing the prepare reading can only reject a stale follower,
// never follow from one.
type followLeaderFacts struct {
	mover  monster.MoverState
	hp     uint32
	radius float64
	exists bool
}

func (s *MonsterState) prepareFollow(division string, expected *monster.Instance, mover monster.MoverState, nowMs int64) (monsterFollowPlan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, expected.Gid)
	instance, exists := state.instances.lookup(expected.Gid)
	if !exists || instance.CurrentHP == 0 || instance != *expected ||
		state.movers.get(expected.Gid) != mover {
		return monsterFollowPlan{}, false
	}
	plan := monsterFollowPlan{division: division, instance: instance, mover: mover}
	plan.leader, plan.leaderHP, plan.leaderRadius, plan.leaderExists = state.followLeaderReading(mover.ControllerGID(), nowMs)
	plan.timersBefore = *s.aiTimersLocked(state, instance, nowMs)
	plan.timers = plan.timersBefore
	return plan, true
}

/*
================
followLeaderReading

The leader reading one leg's followers of a summoner share: the
first prepare of the leg resolves the leader's mover and instance
facts, later prepares at the same stamp reuse them. A new stamp
re-reads. The caller holds the state lock; commitFollow re-reads
everything, so the sharing window - one leg - can only add the
commit rejections a mid-leg leader move already causes.
================
*/
func (state *divisionMonsterState) followLeaderReading(summonerGID uint32, nowMs int64) (monster.MoverState, uint32, float64, bool) {
	if state.followLeaderMemo == nil || state.followLeaderStamp != nowMs {
		state.followLeaderMemo = make(map[uint32]followLeaderFacts)
		state.followLeaderStamp = nowMs
	}
	if facts, ok := state.followLeaderMemo[summonerGID]; ok {
		return facts.mover, facts.hp, facts.radius, facts.exists
	}
	leaderHP, leaderRadius, parentExists := state.instances.leaderFacts(summonerGID)
	leader, moving := state.movers.lookup(summonerGID)
	facts := followLeaderFacts{mover: leader, hp: leaderHP, radius: leaderRadius, exists: parentExists && moving}
	state.followLeaderMemo[summonerGID] = facts
	return facts.mover, facts.hp, facts.radius, facts.exists
}

// commitFollow admits scan cadence, movement, and entry effects together.
// Negative eligibility is an accepted scan and consumes its gate; a stale
// snapshot is rejected and consumes nothing. Never roll back a live timer.
func (s *MonsterState) commitFollow(plan monsterFollowPlan, mover monster.MoverState, frames []Frame) ([]Frame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(plan.division, plan.instance.Gid)
	current, exists := state.instances.lookup(plan.instance.Gid)
	leaderHP, leaderRadius, parentExists := state.instances.leaderFacts(plan.mover.ControllerGID())
	leader, leaderExists := state.movers.lookup(plan.mover.ControllerGID())
	timers := state.aiTimer(plan.instance.Gid)
	if !exists || current.CurrentHP == 0 || current != plan.instance ||
		state.movers.get(current.Gid) != plan.mover || timers == nil || *timers != plan.timersBefore ||
		(parentExists && leaderExists) != plan.leaderExists || leaderHP != plan.leaderHP || leaderRadius != plan.leaderRadius || leader != plan.leader {
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
