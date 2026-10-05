package simulation

import "opensro.online/server/internal/game/world/monster"

/*
==================
HostilityEvent

One hate event for a monster's opponent ledger (5473C0): the damage and
aggression it adds for Attacker, and Percent, the share of Attacker's held
aggression it adds first. Aggression and Percent are signed: a threat
decrease (dtnt) sends them negative and the ledger clamps at zero.
==================
*/
type HostilityEvent struct {
	Attacker, Damage    uint32
	Aggression, Percent int32
}

// RecordHostility commits one completed action's damage/aggression event.
// MonsterState owns both records and the resulting mover transition; a caller
// cannot replace a live mover from its earlier snapshot.
func (s *MonsterState) RecordHostility(division string, gid, attacker, damage, aggression uint32, candidates [3]monster.OpponentCandidate, now int64) bool {
	byGID := make(map[uint32]monster.OpponentCandidate, len(candidates))
	for _, c := range candidates {
		byGID[c.GID] = c
	}
	return s.RecordHostilitySequence(division, gid, []HostilityEvent{{Attacker: attacker, Damage: damage, Aggression: int32(aggression)}}, byGID, now)
}

// Linked threat dispatches source then attacker. Both events must see the
// preceding ledger mutation before a mover tick can observe the final target.
func (s *MonsterState) RecordHostilitySequence(division string, gid uint32, events []HostilityEvent, byGID map[uint32]monster.OpponentCandidate, now int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 || len(events) == 0 {
		return false
	}
	for _, e := range events {
		if e.Attacker == 0 {
			return false
		}
	}
	mover, exists := state.movers.lookup(gid)
	if !exists {
		mover = monster.NewSpawnMover(instance, now)
	}
	for _, event := range events {
		var candidates [3]monster.OpponentCandidate
		for i, candidate := range [3]uint32{instance.Opponents[0].GID, instance.Opponents[1].GID, event.Attacker} {
			candidates[i] = byGID[candidate]
			candidates[i].GID = candidate
		}
		target := monster.RecordOpponentHit(&instance.Opponents, instance.Nest.TargetPolicy, event.Attacker, event.Damage, event.Aggression, event.Percent, uint32(now), mover.AttackIntervalMs, candidates)
		state.instances.set(gid, instance)
		if target == 0 {
			continue
		}
		if mover.TargetGID() == target && mover.Retaliating() && (mover.Mode() == monster.MoverChasing || mover.Mode() == monster.MoverAttacking) {
			continue
		}
		if err := mover.Transition(monster.MoverEventRetaliationArmed, target); err != nil {
			panic(err)
		}
		if state.movers == nil {
			state.movers = make(moverStorage)
		}
		state.movers.set(gid, mover)
		state.syncApproachActor(gid, mover)
		state.behavior.set(gid, 0)
	}
	return true
}
