/*
===========================================================================

tactics_plane.go - the pursuit routine each plane's actors run (#1646)

Native picks a monster's pursuit routine once, at spawn, from the region
test: 53FD04 installs CAITactics_PursueTargetWithRegionRoute (549460) for
dungeon actors and CAITactics_PursueTargetWithDirectionGate (548BE0)
outdoors. That choice is an AI decision keyed by the plane's kind, not a
navigation service, so it is not on simulation.Plane; this file is its one
owner, and callers ask it instead of testing the region bit.

===========================================================================
*/
package simulation

/*
================
PursuitRoutine
================
*/
type PursuitRoutine uint8

const (
	// PursuitDirectionGate is the outdoor routine (548BE0).
	PursuitDirectionGate PursuitRoutine = iota
	// PursuitRegionRoute is the cave routine (549460).
	PursuitRegionRoute
)

/*
================
pursuitFor

The routine 53FD04 installs for an actor on a plane of this kind.
================
*/
func pursuitFor(kind PlaneKind) PursuitRoutine {
	if kind == PlaneCave {
		return PursuitRegionRoute
	}
	return PursuitDirectionGate
}

/*
================
pursuitAt

The routine for an actor standing in regionID.
================
*/
func pursuitAt(regionID uint16) PursuitRoutine {
	return pursuitFor(PlaneKindOf(regionID))
}

/*
================
PursuitRoutine.ExtendsTowardMovingTarget

Whether the routine carries its goal on toward a target that is moving:
548BE0 travels the actor-to-target center distance along the approach
direction (548CE2..548D75, 548CF6), so a running target is not outpaced;
549460 has no such branch and steers to the slot point whether or not the
target moves.
================
*/
func (r PursuitRoutine) ExtendsTowardMovingTarget() bool {
	return r == PursuitDirectionGate
}
