/*
===========================================================================

plane_surfacepath_test.go - the object-surface path keeps the old
answers on the planes (#1673)

WaterValidator.objectSurfacePath used to test the dungeon region bit
itself. #1673 moved each arm into its plane. These tests keep the old
body here, as the parity reference, and require the plane dispatch to
answer every real probe exactly as it did.

===========================================================================
*/
package movement

import (
	"math"
	"reflect"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyObjectSurfacePath

The object-surface path as object_surface_path.go had it before #1673.
================
*/
func legacyObjectSurfacePath(v *WaterValidator, from, to simulation.Spawn) *objectOwnedPath {
	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		return nil
	}
	surface := v.surfaceForRegion(from.RegionID)
	if surface == nil {
		return nil
	}
	bx := from.X + float64(simulation.SectorX(from.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	bz := from.Z + float64(simulation.SectorY(from.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	terrain, ok := surface.terrainHeightAt(bx, bz)
	if !ok {
		return nil
	}
	stand := v.spawnObjectDeckStand(surface, bx, bz, from.Y, terrain)
	if stand == nil {
		return nil
	}
	a := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	b := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	p := stand.placement
	c, s := math.Cos(p.yaw), math.Sin(p.yaw)
	local := func(x, z float64) (float64, float64) {
		x -= stand.anchorX + p.x
		z -= stand.anchorZ + p.z
		return c*x + s*z, -s*x + c*z
	}
	x0, z0 := local(a.X, a.Z)
	x1, z1 := local(b.X, b.Z)
	return traceObjectCells(stand, x0, z0, x1, z1)
}

/*
================
sameSurfacePath

Whether two surface-path answers are the same answer: both nil, or the
same chord over the same cell of the same mesh with the same spans.
================
*/
func sameSurfacePath(got, want *objectOwnedPath) bool {
	if got == nil || want == nil {
		return got == want
	}
	return got.stand.mesh == want.stand.mesh && got.stand.cellIndex == want.stand.cellIndex &&
		got.x0 == want.x0 && got.z0 == want.z0 && got.x1 == want.x1 && got.z1 == want.z1 &&
		reflect.DeepEqual(got.spans, want.spans)
}

/*
================
TestObjectSurfacePathPlaneDispatchKeepsTheOldAnswers

Real probes on both planes - the Jangan veranda outdoors (a deck at
3.04 over terrain 0, so chords from the anchors retain object cells),
the Donwhang cave anchors - and chords across the cave boundary both
ways.
================
*/
func TestObjectSurfacePathPlaneDispatchKeepsTheOldAnswers(t *testing.T) {
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

	retainedOutdoor, caveChords := 0, 0
	for _, probe := range probes {
		from, to := probe[0], probe[1]
		got := v.objectSurfacePath(from, to)
		if want := legacyObjectSurfacePath(v, from, to); !sameSurfacePath(got, want) {
			t.Fatalf("%+v -> %+v: surface path %+v, legacy %+v", from, to, got, want)
		}
		if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
			if got != nil {
				t.Fatalf("%+v -> %+v: a chord touching a cave retained a surface path", from, to)
			}
			caveChords++
		} else if got != nil {
			retainedOutdoor++
		}
	}
	// Both arms must be exercised for real: outdoor chords retain paths
	// somewhere, and cave chords never do, or the probes no longer reach
	// both planes.
	if retainedOutdoor == 0 || caveChords == 0 {
		t.Fatalf("retained outdoor paths %d, cave chords %d: the probes no longer reach both planes",
			retainedOutdoor, caveChords)
	}
}
