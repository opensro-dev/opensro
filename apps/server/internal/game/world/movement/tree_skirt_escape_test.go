/*
===========================================================================

tree_skirt_escape_test.go - #4148: a terrain stand inside a sealed sunken
tree skirt can walk out, and the skirt still refuses entry

Region 0x61A8 (sector 168,97) carries asset 574 (tre_tree02.bsr) at
(128.97, 0, 981.87): one nav mesh of 4 flat cells at y=-1.25, all 6 outline
edges flag 3, no open exit - a sealed lane lying 1.25 below the flat
terrain (height 0). The reported pose (99.7, -1.2, 968.3) persisted on
that skirt. Before the fix, the resident seat (navowner.go, the #3006
Hotan-ramp seat) seated a retained terrain walker standing in the skirt's
cells onto the sealed deck on its first step, and the owned-cell stepper
then refused every outline crossing: the character could not move at all.

The fix refuses the resident seat on a sealed lane and lets a
terrain-owned chord leave a sealed lane's cells (sealedExit in
objectnav_collision.go). Entry stays refused: the outline walls a terrain
walker approaching from outside, from either side of the lane.

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

// skirtWalkOutcome is one stepped live-composition walk's result.
type skirtWalkOutcome struct {
	end     simulation.Spawn
	owner   simulation.NavOwner
	reached bool
	seated  bool
}

/*
================
stepSkirtWalk

Steps from -> goal in 2u ground steps through the production composition
(ClientClip.processOwnedStep with the WalkOwners/ResolveNavOwner lift the
runtime's groundStep performs), reporting whether the walker ever became
object-owned or sank below the terrain plane (the pre-fix seat).
================
*/
func stepSkirtWalk(v *WaterValidator, clipper *ClientClip, from, goal simulation.Spawn) skirtWalkOutcome {
	owner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok {
		return skirtWalkOutcome{end: from}
	}
	out := skirtWalkOutcome{end: from, owner: owner}
	pose := from
	for tick := 0; tick < 60; tick++ {
		if owner.Kind == simulation.NavOwnerObject || pose.Y < -0.5 {
			out.seated = true
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
TestSunkenTreeSkirtEscape
================
*/
func TestSunkenTreeSkirtEscape(t *testing.T) {
	root, err := clientdata.ClientPublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "world", "world-region-catalog.json")); err != nil {
		t.Skipf("real client assets not present (%v)", err)
	}
	v := NewWaterValidator(root)
	clipper := &ClientClip{Mode: ClipApply, Validator: v}
	const region = 0x61A8

	// The reported spot at terrain height arbitrates to the terrain: the
	// skirt lies 1.25 below it and does not own the stand.
	spot := simulation.Spawn{RegionID: region, X: 99.7, Y: 0, Z: 968.3}
	owner, _, ok := v.ResolveNavOwner(spot, simulation.NavOwner{})
	if !ok || owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("spot must arbitrate to terrain, got %+v ok=%v", owner, ok)
	}

	// Escape west: the terrain walker walks out of the footprint and
	// arrives on open terrain, never seated onto the skirt.
	west := stepSkirtWalk(v, clipper, spot, simulation.Spawn{RegionID: region, X: 30, Z: 968.3})
	if !west.reached {
		t.Fatalf("westward escape must arrive, ended at (%.2f, %.2f, %.2f) owner %+v", west.end.X, west.end.Y, west.end.Z, west.owner)
	}
	if west.seated || west.owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("westward escape must stay terrain-owned, ended %+v owner %+v", west.end, west.owner)
	}

	// Escape east travels at least to the far side of the footprint
	// (the pre-fix seat stopped it within 0.05u), again unseated.
	east := stepSkirtWalk(v, clipper, spot, simulation.Spawn{RegionID: region, X: 170, Z: 968.3})
	if east.seated || east.owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("eastward escape must stay terrain-owned, ended %+v owner %+v", east.end, east.owner)
	}
	if east.end.X < spot.X+24 {
		t.Fatalf("eastward escape must clear the footprint, ended at x=%.2f", east.end.X)
	}

	// Entry stays refused: a walker approaching from outside stops at
	// the outline, terrain-owned, and never reaches the reported spot.
	entry := stepSkirtWalk(v, clipper,
		simulation.Spawn{RegionID: region, X: 40, Y: 0, Z: 968.3},
		simulation.Spawn{RegionID: region, X: 170, Z: 968.3})
	if entry.reached || entry.seated || entry.owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("entry must stay refused on terrain, got %+v", entry)
	}
	if entry.end.X >= spot.X {
		t.Fatalf("entry must stop west of the reported spot, ended at x=%.2f", entry.end.X)
	}

	// The persisted skirt pose is rescue-covered, and the rescue point -
	// inside the footprint's XZ - can itself walk out now.
	rescued, stranded, found := v.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: region, X: 99.7, Y: -1.2, Z: 968.3})
	if !stranded || !found {
		t.Fatalf("skirt pose must rescue: stranded=%v found=%v", stranded, found)
	}
	out2 := stepSkirtWalk(v, clipper, rescued, simulation.Spawn{RegionID: region, X: 30, Z: rescued.Z})
	if !out2.reached || out2.seated || out2.owner.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("rescued stand must walk out on terrain, got %+v", out2)
	}
}
