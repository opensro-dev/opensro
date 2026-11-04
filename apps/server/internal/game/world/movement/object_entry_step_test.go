/*
===========================================================================

object_entry_step_test.go - a ground step onto a placed object stands on it

Report BR-261031-1337-33A7 (#2368) sent us to the Fertile Temple
(0x6687); at the clip seam underneath it, an arrived outdoor step that
changed surface (terrain into a placed object, or back) returned the
requested point at the walker's old Y with no owner, discarding the
entered surface the clip had just computed (its Rest) - unlike a
continued step or a dungeon arrival, which already stood at their
rest. These pins hold the seam's contract: an entry step stands at
the clip's rest on the entered surface, a same-surface step still
returns the requested point bit-for-bit, and a second region's entry
(the Constantinople harbor quay, 0x6850) seats the same way.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
groundStepWalk

Walks from -> goal in steps of at most two units, composed as
Runtime.groundStep composes the clip half of a step (groundwalk.go):
the clip's accepted owner when it resolves, the walk-owners lift of
the step end when it does not. Returns the rest pose and owner, and
whether the goal's (x, z) was reached.
================
*/
func groundStepWalk(v *WaterValidator, clipper *ClientClip, from simulation.Spawn, fromOwner simulation.NavOwner, goal simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
	pose, owner := from, fromOwner
	for tick := 0; tick < 600; tick++ {
		dx, dz := goal.X-pose.X, goal.Z-pose.Z
		dist := math.Hypot(dx, dz)
		if dist < 0.5 {
			return pose, owner, true
		}
		step := 2.0
		if dist < step {
			step = dist
		}
		to := simulation.Spawn{RegionID: pose.RegionID, X: pose.X + dx/dist*step, Y: pose.Y, Z: pose.Z + dz/dist*step}
		next, acceptedOwner, blocked := clipper.processOwnedStep("ground-step", pose, owner, to)
		if blocked {
			return pose, owner, false
		}
		if !acceptedOwner.Resolved() {
			// Production Runtime.walkOwners: the surface the walk
			// reached lifts the step end onto that surface's height.
			walk := v.WalkOwners(pose, owner, to)
			acceptedOwner = walk.Rest
			if lifted, y, liftedOK := v.ResolveNavOwner(to, walk.Rest); liftedOK {
				to.Y, acceptedOwner = y, lifted
			}
			next = to
		}
		pose, owner = next, acceptedOwner
	}
	return pose, owner, false
}

/*
================
TestGroundWalkIntoTheTempleSeatsTheDeck

The report's own leg: the capture pose on the floor (105.66, -40.40,
1662.32) walks to a monster standing on the temple deck (45, -35.18,
1902). The walk must end on the deck - its height and its owner - not
under it at floor height.
================
*/
func TestGroundWalkIntoTheTempleSeatsTheDeck(t *testing.T) {
	v := realAuthorityValidator(t)
	clipper := &ClientClip{Mode: ClipApply, Validator: v}

	from := simulation.Spawn{RegionID: fertileTempleRegion, X: 105.66, Y: fertileTempleFloorY, Z: 1662.32}
	fromOwner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || fromOwner.Kind != world.NavOwnerTerrain {
		t.Fatalf("fixture: capture pose owner = %+v, want terrain", fromOwner)
	}
	goal := simulation.Spawn{RegionID: fertileTempleRegion, X: 45, Y: -35.18, Z: 1902}
	rest, owner, arrived := groundStepWalk(v, clipper, from, fromOwner, goal)
	if !arrived {
		t.Fatalf("ground walk never reached the deck point, rest %+v", rest)
	}
	if owner.Kind != world.NavOwnerObject {
		t.Fatalf("ground walk rest owner = %+v, want the temple deck", owner)
	}
	if math.Abs(rest.Y-(-35.18)) > 0.05 {
		t.Fatalf("ground walk rests at y %.2f, want the deck (-35.18) - the follow arrives under the deck", rest.Y)
	}
}

/*
================
TestEntryStepOntoTheTempleDeckStandsAtTheClipRest

One long step across the temple entry (floor to deck): the accepted
pose is the clip's rest on the entered surface, not the requested
point.
================
*/
func TestEntryStepOntoTheTempleDeckStandsAtTheClipRest(t *testing.T) {
	v := realAuthorityValidator(t)
	clipper := &ClientClip{Mode: ClipApply, Validator: v}

	from := simulation.Spawn{RegionID: fertileTempleRegion, X: 66.4, Y: fertileTempleFloorY, Z: 1817.4}
	fromOwner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || fromOwner.Kind != world.NavOwnerTerrain {
		t.Fatalf("fixture: entry step starts on %+v, want terrain", fromOwner)
	}
	to := simulation.Spawn{RegionID: fertileTempleRegion, X: 46.8, Y: -35.18, Z: 1895}
	report := v.ClipMovementPathFrom(from, fromOwner, to)
	if report.Outcome != ClipArrived || report.RestOwner.Kind != world.NavOwnerObject {
		t.Fatalf("fixture: entry chord = %s on %+v, want arrived on the deck", report.Outcome, report.RestOwner)
	}
	pose, owner, blocked := clipper.processOwnedStep("ground-step", from, fromOwner, to)
	if blocked || pose != report.Rest || owner != report.RestOwner {
		t.Fatalf("entry step = (%+v, %+v, %v), want the clip's rest (%+v, %+v)",
			pose, owner, blocked, report.Rest, report.RestOwner)
	}
}

/*
================
TestEntryStepOntoTheHarborQuayStandsAtTheClipRest

The same entry in a different region's bundle: the Constantinople
harbor (0x6850), where a quay floor at -143.09 stands beside the
terrain at the region's west edge. Not temple-shaped.
================
*/
func TestEntryStepOntoTheHarborQuayStandsAtTheClipRest(t *testing.T) {
	v := realAuthorityValidator(t)
	clipper := &ClientClip{Mode: ClipApply, Validator: v}

	from := simulation.Spawn{RegionID: 0x6850, X: 12, Y: 0, Z: 12}
	fromOwner, fromY, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || fromOwner.Kind != world.NavOwnerTerrain {
		t.Fatalf("fixture: quay entry starts on %+v, want terrain", fromOwner)
	}
	from.Y = fromY
	to := simulation.Spawn{RegionID: 0x6850, X: 36, Y: 0, Z: 12}
	toOwner, toY, ok := v.ResolveNavOwner(to, simulation.NavOwner{})
	if !ok || toOwner.Kind != world.NavOwnerObject {
		t.Fatalf("fixture: quay stand = %+v, want the quay object", toOwner)
	}
	to.Y = toY
	report := v.ClipMovementPathFrom(from, fromOwner, to)
	if report.Outcome != ClipArrived || report.RestOwner.Kind != world.NavOwnerObject {
		t.Fatalf("fixture: quay chord = %s on %+v, want arrived on the quay", report.Outcome, report.RestOwner)
	}
	pose, owner, blocked := clipper.processOwnedStep("ground-step", from, fromOwner, to)
	if blocked || pose != report.Rest || owner != report.RestOwner {
		t.Fatalf("quay entry step = (%+v, %+v, %v), want the clip's rest (%+v, %+v)",
			pose, owner, blocked, report.Rest, report.RestOwner)
	}
}

/*
================
TestSameSurfaceStepReturnsTheRequestedPoint

The control: a step that stays on one surface returns the requested
point bit-for-bit with no owner, exactly as before the entry fix - the
blast radius of the change is the surface crossing alone.
================
*/
func TestSameSurfaceStepReturnsTheRequestedPoint(t *testing.T) {
	v := realAuthorityValidator(t)
	clipper := &ClientClip{Mode: ClipApply, Validator: v}

	from := simulation.Spawn{RegionID: fertileTempleRegion, X: 105.66, Y: fertileTempleFloorY, Z: 1662.32}
	fromOwner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || fromOwner.Kind != world.NavOwnerTerrain {
		t.Fatalf("fixture: control step starts on %+v, want terrain", fromOwner)
	}
	to := simulation.Spawn{RegionID: fertileTempleRegion, X: 100, Y: fertileTempleFloorY, Z: 1680}
	pose, owner, blocked := clipper.processOwnedStep("ground-step", from, fromOwner, to)
	if blocked || pose != to || owner.Resolved() {
		t.Fatalf("same-surface step = (%+v, %+v, %v), want the requested point and no owner", pose, owner, blocked)
	}
}
