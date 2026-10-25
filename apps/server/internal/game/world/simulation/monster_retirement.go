/*
===========================================================================

monster_retirement.go - the population's current evacuation state

The allocator owns retirement, including the nonempty evacuation interval.
The population owner exposes that exact lease state under its usual lock;
the final-release inbox remains a separate manager transition.

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/instance"

/*
================
PopulationRetiring

Returns the evacuation state and whether the exact population lifetime still
exists. A stale lease never observes the state of its replacement layer.
================
*/
func (s *MonsterState) PopulationRetiring(division string, lease instance.Lease) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return false, false
	}
	return s.worldAllocators[division].Retiring(lease)
}
