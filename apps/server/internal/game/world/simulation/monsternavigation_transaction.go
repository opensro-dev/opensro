/*
===========================================================================

monsternavigation_transaction.go - admitting a monster's route and packets together

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

type navigationAdmission struct {
	approach *approachNavigation
	instance monster.Instance
	mover    monster.MoverState
	exists   bool
}

func (s *MonsterState) prepareNavigation(division string, gid uint32) navigationAdmission {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, exists := state.instances.lookup(gid)
	return navigationAdmission{instance: instance, mover: state.movers.get(gid), exists: exists}
}

// Geometry can run outside the population lock. Publish route, current leg
// and packets together only if every authoritative input still matches.
func (s *MonsterState) commitNavigation(division string, before navigationAdmission, next monster.MoverState, frames []Frame) []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, before.instance.Gid)
	current, exists := state.instances.lookup(before.instance.Gid)
	unchanged := before.exists && exists && current == before.instance && state.movers.get(current.Gid) == before.mover
	if !unchanged || !state.approachInputsMatch(before.approach) {
		return nil
	}
	// The retail server's move command refuses under freeze, sleep, root or stun; the
	// tactics state is unchanged and retries on a later tick.
	if current.MovementBlocked() {
		return nil
	}
	if !s.commitMoverLocked(state, current.Gid, next) {
		return nil
	}
	state.commitApproach(before.approach, current.Gid, next)
	return frames
}
