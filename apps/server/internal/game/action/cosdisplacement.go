/*
===========================================================================

cosdisplacement.go - committed companion hit displacement and movement hold

The existing follower or rider owns the position. No second mover is created.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"strings"
)

/*
================
commitCOSDisplacement

Called under the victim's authority door after a surviving impact.
================
*/
func (rt *Runtime) commitCOSDisplacement(owner *cosAbnormalOwner, d *playerDisplacement) (wire.SkillCastFacingPoint, bool) {
	point, valid := wire.NewSkillCastFacingPoint(d.pose.RegionID, d.pose.X, d.pose.Y, d.pose.Z)
	if !valid {
		return point, false
	}
	if owner.pet.Mounted {
		point, frames, ok := rt.commitPlayerDisplacementInDoor(owner.division, owner.c, d)
		owner.public = append(owner.public, frames...)
		return point, ok
	}
	state := rt.petSessionFor(owner.division, owner.c.Name, owner.pet.GID)
	if state == nil {
		return point, false
	}
	if state.follower != nil {
		state.follower.Displace(d.pose, owner.now)
	} else if state.transportCOS != nil {
		state.transportWorld.Spawn, state.transportWorld.MoveSegment = d.pose, nil
	} else {
		return point, false
	}
	state.displacement = d
	rt.cancelPetCombat(petOwnerKey{division: owner.division, name: strings.ToLower(owner.c.Name), gid: owner.pet.GID}, state, owner.now)
	expireCosPickup(state)
	return point, true
}

/*
================
cosDisplaceable
================
*/
func (rt *Runtime) cosDisplaceable(division string, c *enterworld.Character, pet *enterworld.CharacterCOS, now int64) bool {
	if pet.Mounted {
		return false
	}
	state := rt.petSessionFor(division, c.Name, pet.GID)
	return state != nil && (state.displacement == nil || !state.displacement.down || now >= state.displacement.untilMs)
}
