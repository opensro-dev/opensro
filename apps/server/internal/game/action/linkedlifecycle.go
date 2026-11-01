/*
===========================================================================

linkedlifecycle.go - retire live links when either endpoint or its tether fails

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The reference server checks the live pair, not the two original cast positions.
// Retirement itself stays in the existing character-effect update owner.
/*
================
advanceLinkedEffects
================
*/
func (rt *Runtime) advanceLinkedEffects(nowMs int64) {
	rt.advanceForcedTargets()
	for _, link := range rt.effects.Links() {
		rt.advanceLinkedEffectInDivision(link, nowMs)
	}
}

/*
================
advanceLinkedEffectInDivision
================
*/
func (rt *Runtime) advanceLinkedEffectInDivision(link statuseffect.Link, nowMs int64) {
	unlock := rt.lockDivision(link.DivisionID)
	defer unlock()
	source := rt.findCharacter(link.DivisionID, link.SourceName)
	target := rt.findCharacter(link.DivisionID, link.TargetName)
	valid := rt.linkedEndpointLive(link.DivisionID, source, link.SourceGID) &&
		rt.linkedEndpointLive(link.DivisionID, target, link.TargetGID)
	if valid && link.MaxDistance != 0 {
		from := rt.liveSpawn(simulation.WorldKey(link.DivisionID, source.Name), source, nowMs)
		to := rt.liveSpawn(simulation.WorldKey(link.DivisionID, target.Name), target, nowMs)
		// The reference server shares the trap tether's native 3D distance and float stores.
		a := monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}
		b := monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z}
		valid = monster.NativeActorDistance(a, b) <= float32(link.MaxDistance)
	}
	if !valid {
		rt.effects.StopLink(link.DivisionID, link.SourceToken)
	}
}

/*
================
linkedEndpointLive

The endpoint facts the tether verdict weighs: present, not deletion
pending, still the character the link was cast on, alive. Read as
values under the authority read door (characterLifeState) instead of
detaching both whole records every tick (#1943): the snapshot's only
normalizations feed none of these facts, and the GID weighs the
immutable ID the copy would carry. The record itself is never needed:
liveSpawn takes the live character as its world seed, as the trade
and return lanes already pass it.
================
*/
func (rt *Runtime) linkedEndpointLive(divisionID string, character *enterworld.Character, gid uint32) bool {
	if character == nil || enterworld.ObjectIDForCharacter(character) != gid {
		return false
	}
	alive, deletePending, _ := rt.characterLifeState(divisionID, character)
	return alive && !deletePending
}
