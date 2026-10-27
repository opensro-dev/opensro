package simulation

import "opensro.online/server/internal/game/world/monster"

// detachNestLocked is the retail server's release-command path to its nest
// detach (nest vtable slot 10). Releasing the nest
// slot does not remove the actor, movement, controller, or authored tactics.
// Caller holds the population lock; a later death cannot release it twice.
func (s *MonsterState) detachNestLocked(state *divisionMonsterState, gid uint32, now int64) bool {
	index, attached := state.gidNests[gid]
	if !attached {
		return false
	}
	actor, exists := state.instances.lookup(gid)
	if !exists {
		panic("nest attachment without a live actor")
	}
	delete(state.gidNests, gid)
	actor.NestDetached = true
	state.instances.set(gid, actor)
	s.nestDeath(state, index, now)
	return true
}

// The retail server's detach test compares only the indoor bit, detaches, and returns before
// activity, queued commands, and OnTick. Preserve that ordering even if the
// planned detachment loses admission to a concurrent actor update.
func (ops *MonsterMoverOps) detachCrossPlaneNest(division string, actor monster.Instance, mover monster.MoverState, now int64) bool {
	if actor.NestDetached || (actor.Nest.RegionID^mover.LivePoseAt(now, ops.GroundHeight).RegionID)&0x8000 == 0 {
		return false
	}
	s := ops.Monsters
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, actor.Gid)
	if _, attached := state.gidNests[actor.Gid]; !attached {
		return false
	}
	if state.instances.get(actor.Gid) == actor && state.movers.get(actor.Gid) == mover {
		s.detachNestLocked(state, actor.Gid, now)
	}
	return true
}
