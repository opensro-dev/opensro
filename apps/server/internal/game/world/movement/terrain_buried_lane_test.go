/*
===========================================================================

terrain_buried_lane_test.go - #3667: a terrain walker is not walled by a
sealed lane whose deck lies far below the ground

Region 0x6397 (sector 151,99) on the Hyeongcheon road carries asset 435
(res/artifact/china/greenfield/cj_farmingtool.bsr, ordinal 4) at
(598.36, -144.86, 502.15): one nav mesh of 4 flat cells at deck height
about -138, all 6 outline edges flag 3, no open exit - a sealed lane
buried about 129 below the terrain at its centre (about -9.4) and about
127 below the road pose (586.3, -11.3, 515.9) just west of its west edge.

The placement registers the road's terrain cell, so the entry dispatch
visits it from the chord's start - and before the fix its outline edges
blocked every road-crossing chord at the west edge although no walker
can stand on, enter or even see that deck: the reported character could
not move east at all. A sealed lane only walls a walker whose height it
can reach; a lane this far out of reach is inert to the chord, exactly
as one without registered cells already is.

===========================================================================
*/
package movement

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

// roadWalk is one stepped live-composition walk's outcome on the road.
type roadWalk struct {
	end     simulation.Spawn
	owner   simulation.NavOwner
	reached bool
	owned   bool
}

/*
================
stepRoadWalk

Steps from -> goal in 2u ground steps through the production composition
(ClientClip.processOwnedStep with the WalkOwners/ResolveNavOwner lift),
reporting whether the walker ever became object-owned.
================
*/
func stepRoadWalk(v *WaterValidator, clipper *ClientClip, from, goal simulation.Spawn) roadWalk {
	owner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok {
		return roadWalk{end: from}
	}
	out := roadWalk{end: from, owner: owner}
	pose := from
	for tick := 0; tick < 60; tick++ {
		if owner.Kind == simulation.NavOwnerObject {
			out.owned = true
		}
		dx, dz := goal.X-pose.X, goal.Z-pose.Z
		dist := math.Hypot(dx, dz)
		if dist < 0.5 {
			out.reached = true
			break
		}
		step := 2.0
		if dist < step {
			step = dist
		}
		to := simulation.Spawn{RegionID: pose.RegionID, X: pose.X + dx/dist*step, Y: pose.Y, Z: pose.Z + dz/dist*step}
		next, acceptedOwner, blocked := clipper.processOwnedStep("ground-step", pose, owner, to)
		if blocked {
			break
		}
		if !acceptedOwner.Resolved() {
			w := v.WalkOwners(pose, owner, to)
			acceptedOwner = w.Rest
			if lifted, y, liftedOK := v.ResolveNavOwner(to, w.Rest); liftedOK {
				to.Y, acceptedOwner = y, lifted
			}
			next = to
		}
		pose, owner = next, acceptedOwner
	}
	out.end, out.owner = pose, owner
	return out
}

/*
================
TestBuriedSealedLaneDoesNotWallRoad
================
*/
func TestBuriedSealedLaneDoesNotWallRoad(t *testing.T) {
	root, err := clientdata.ClientPublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "world", "world-region-catalog.json")); err != nil {
		t.Skipf("real client assets not present (%v)", err)
	}
	v := NewWaterValidator(root)
	clipper := &ClientClip{Mode: ClipApply, Validator: v}
	const region = 0x6397

	spot := simulation.Spawn{RegionID: region, X: 586.3, Y: -11.3, Z: 515.9}
	owner, _, ok := v.ResolveNavOwner(spot, simulation.NavOwner{})
	if !ok || owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("spot must arbitrate to terrain, got %+v ok=%v", owner, ok)
	}

	// East along the road: the walk crosses the buried lane's footprint
	// and arrives, terrain-owned the whole way.
	east := stepRoadWalk(v, clipper, spot, simulation.Spawn{RegionID: region, X: 700, Z: 515.9})
	if !east.reached || east.owned || east.owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("eastward road walk must arrive terrain-owned, got %+v", east)
	}

	// The reported click line: north-east to (833, 602) makes real
	// progress instead of being corrected back to the start (the pre-fix
	// clip held it at the first step).
	ne := stepRoadWalk(v, clipper, spot, simulation.Spawn{RegionID: region, X: 833, Z: 602})
	if ne.owned || ne.owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("north-east walk must stay terrain-owned, got %+v", ne)
	}
	if ne.end.X < spot.X+40 && ne.end.Z < spot.Z+15 {
		t.Fatalf("north-east walk must travel, ended at (%.2f, %.2f)", ne.end.X, ne.end.Z)
	}
}
