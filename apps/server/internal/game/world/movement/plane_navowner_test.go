/*
===========================================================================

plane_navowner_test.go - surface ownership and the path guard keep the old
answers on the planes (#1672)

Owner resolution, the owner walk and the path guard used to test the
dungeon region bit in navowner.go and pathguard.go. #1672 moved each arm
into its plane. These tests keep the old dispatchers here, as the parity
reference, and require the plane dispatch to answer every real probe
exactly as it did.

===========================================================================
*/
package movement

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyResolveNavOwner

Owner resolution as navowner.go had it before #1672.
================
*/
func legacyResolveNavOwner(v *WaterValidator, p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool) {
	if simulation.IsDungeonRegion(p.RegionID) {
		// #1727: a cave point now resolves to its dungeon cell, its
		// height unchanged.
		if surface := v.caveSurface(p.RegionID); surface != nil {
			owner, ok := surface.dungeonOwnerAt(p.RegionID, p.X, p.Y, p.Z, hint)
			return owner, p.Y, ok
		}
		return simulation.NavOwner{}, p.Y, false
	}
	return v.resolveOutdoorNavOwner(p, hint)
}

/*
================
legacyOwnerWalk

The owner walk as navowner.go had it before #1672.
================
*/
func legacyOwnerWalk(v *WaterValidator, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) *navWalk {
	from = v.ownedStart(from, fromOwner)
	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		return nil
	}
	return v.outdoorOwnerWalk(from, fromOwner, to)
}

/*
================
legacyValidatePath

The path guard's classification as pathguard.go had it before #1672.
================
*/
func legacyValidatePath(v *WaterValidator, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) PathReport {
	from = v.ownedStart(from, fromOwner)
	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		return PathReport{Verdict: PathDungeonExempt, DistanceUnits: simulation.WorldDistance2D(from, to)}
	}
	return v.validateOutdoorPath(from, fromOwner, to)
}

/*
================
TestNavOwnerAndPathGuardPlaneDispatchKeepTheOldAnswers

Real probes on both planes - the Jangan veranda outdoors (terrain 0 under
a deck at 3.04, so resolution picks an object owner), the Donwhang cave
anchors - and chords across the cave boundary both ways.
================
*/
func TestNavOwnerAndPathGuardPlaneDispatchKeepTheOldAnswers(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	outdoor := []simulation.Spawn{
		{RegionID: planeOutdoorRegion, X: 623, Y: 3.04, Z: 1271},
		{RegionID: planeOutdoorRegion, X: 680, Y: 3.04, Z: 1271}, // at the veranda rail
	}
	cave := []simulation.Spawn{
		{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: planeCaveRegion, X: 629, Y: -2.22, Z: 74},
	}
	probes := append(planeProbes(outdoor), planeProbes(cave)...)
	probes = append(probes, [2]simulation.Spawn{outdoor[0], cave[0]}, [2]simulation.Spawn{cave[0], outdoor[0]})

	resolvedOutdoor, walked, guarded := 0, 0, 0
	for _, probe := range probes {
		from, to := probe[0], probe[1]
		for _, p := range probe {
			gotOwner, gotY, gotOK := v.ResolveNavOwner(p, simulation.NavOwner{})
			wantOwner, wantY, wantOK := legacyResolveNavOwner(v, p, simulation.NavOwner{})
			if gotOwner != wantOwner || gotY != wantY || gotOK != wantOK {
				t.Fatalf("%+v: owner (%+v, %v, %t), legacy (%+v, %v, %t)", p, gotOwner, gotY, gotOK, wantOwner, wantY, wantOK)
			}
			if gotOK && gotOwner.Kind == simulation.NavOwnerObject {
				resolvedOutdoor++
			}
		}
		gotWalk, wantWalk := v.ownerWalk(from, simulation.NavOwner{}, to), legacyOwnerWalk(v, from, simulation.NavOwner{}, to)
		if !reflect.DeepEqual(gotWalk, wantWalk) {
			t.Fatalf("%+v -> %+v: owner walk %+v, legacy %+v", from, to, gotWalk, wantWalk)
		}
		if gotWalk != nil {
			walked++
		}
		gotReport := v.ValidateMovementPathFrom(from, simulation.NavOwner{}, to)
		if wantReport := legacyValidatePath(v, from, simulation.NavOwner{}, to); !reflect.DeepEqual(gotReport, wantReport) {
			t.Fatalf("%+v -> %+v: path %+v, legacy %+v", from, to, gotReport, wantReport)
		}
		if gotReport.Verdict == PathDungeonExempt {
			guarded++
		}
	}
	// Both arms must be exercised for real: deck owners resolve outdoors,
	// outdoor walks exist, and cave chords are exempt.
	if resolvedOutdoor == 0 || walked == 0 || guarded == 0 {
		t.Fatalf("deck owners %d, owner walks %d, exempt chords %d: the probes no longer reach both planes", resolvedOutdoor, walked, guarded)
	}
}
