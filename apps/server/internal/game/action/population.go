/*
===========================================================================

population.go - the present players a population count sees

PopulationPlayers feeds the native message-cell counts. It reads the
division's characters through the store's read door, taking the list
before the door: the store lock does not re-enter.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
PopulationPlayers

Snapshots present PCs at their live position. It does not apply the
reward distributor's alive filter to native message-cell counts.

The division list is taken before the read door: CharactersForDivision
takes the store's read lock itself, and a second read lock inside the door
waits behind any queued writer while the door holds that writer off. The
slice is a fresh copy of the live pointers, so the field reads stay inside
the door.
================
*/
func (rt *Runtime) PopulationPlayers(division string, now int64) []simulation.PopulationPlayer {
	var out []simulation.PopulationPlayer
	characters := rt.deps.CharactersForDivision(division)
	rt.deps.Read(division, func() {
		for _, c := range characters {
			if c == nil || c.DeletePending || rt.RewardActorPresent == nil || !rt.RewardActorPresent(division, c.Name) {
				continue
			}
			out = append(out, simulation.PopulationPlayer{GID: enterworld.ObjectIDForCharacter(c), World: domain.CharacterWorldInstance(c), Spawn: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)})
		}
	})
	return out
}
