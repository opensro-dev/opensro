/*
===========================================================================

cosmount.go - the one writer of a companion's ride state

While its owner rides it, a vehicle moves in the rider's world; the
rest of the time its own mover (simulation.CompanionMover) poses it.
pet.Mounted says which, and it is persisted on the record, so it stays
the one fact - but flipping it changes the pose owner, and a flip that
forgot to carry the pose across left the vehicle at its stale parked
spot (the ridden transport whose cargo dropped at the specialty shop,
PR #3464). Every flip goes through mountCompanion or unmountCompanion;
internal/gates refuses an assignment to Mounted anywhere else.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
mountCompanion

The rider takes the vehicle: from here the rider's world moves it. A
follower still bound stops where it stands (the mount door relocated the
rider onto the vehicle, so that is the same spot).
================
*/
func (rt *Runtime) mountCompanion(division string, c *enterworld.Character, pet *enterworld.CharacterCOS, now int64) {
	if pet == nil || pet.Mounted {
		return
	}
	if state := rt.petSessionFor(division, c.Name, pet.GID); state != nil {
		state.mover.StopInPlace(now)
	}
	pet.Mounted = true
}

/*
================
unmountCompanion

The rider leaves the vehicle - stepping off, the vehicle's death, the
rider's death or departure, a fresh summon: the vehicle stands where
the rider stood, parked in its own mover, before Mounted clears and
the mover becomes its pose owner again. A companion not mounted is
left alone.
================
*/
func (rt *Runtime) unmountCompanion(division string, c *enterworld.Character, pet *enterworld.CharacterCOS, now int64) {
	if pet == nil || !pet.Mounted {
		return
	}
	pose := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	if state := rt.petSessionFor(division, c.Name, pet.GID); state != nil {
		state.mover.Park(pet.GID, pose)
	}
	pet.Mounted = false
}
