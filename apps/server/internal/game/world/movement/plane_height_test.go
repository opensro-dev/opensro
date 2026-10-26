/*
===========================================================================

plane_height_test.go - a monster path's height sampler keeps the old
answers on the planes (#1675)

The sampler used to test the dungeon region bit in monster_navigation.go.
#1675 moved each arm into its plane. This test keeps the old dispatcher
here, as the parity reference, and requires the plane dispatch to answer
every real sample exactly as it did - including the deck probe, where the
Jangan veranda stands at 3.04 over terrain at 0, so the outdoor arm must
read the walk's deck and not the terrain, and a stacked cave column (floors
at 0 and 280), where the cave arm must keep the caller's height.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// heightParitySamples is how many chord parameters each probe samples.
const heightParitySamples = 9

// veranda deck and terrain heights at the Jangan anchors (#1649's probe).
const (
	verandaDeckY    = 3.04
	verandaTerrainY = 0.0
)

/*
================
legacyMonsterPathHeightAt

The sampler as monster_navigation.go had it before #1675.
================
*/
func legacyMonsterPathHeightAt(v *WaterValidator, t float64, p monster.Pose, walk *navWalk) (float64, bool) {
	if simulation.IsDungeonRegion(p.RegionID) {
		return v.dungeonSpawnHeightAt(p.RegionID, p.X, p.Y, p.Z)
	}
	if y, ok := walk.heightAt(t); ok {
		return y, true
	}
	return v.TerrainHeightAt(p.RegionID, p.X, p.Z)
}

/*
================
TestPathHeightPlaneDispatchKeepsTheOldAnswers
================
*/
func TestPathHeightPlaneDispatchKeepsTheOldAnswers(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	outdoor := []simulation.Spawn{
		{RegionID: planeOutdoorRegion, X: 623, Y: verandaDeckY, Z: 1271},
		{RegionID: planeOutdoorRegion, X: 680, Y: verandaDeckY, Z: 1271}, // at the veranda rail
	}
	cave := []simulation.Spawn{
		{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: planeCaveRegion, X: 629, Y: -2.22, Z: 74},
		// A stacked column: floors at 0 and 280. Standing on the upper
		// one, the cave arm must keep the caller's height to read it.
		{RegionID: planeCaveRegion, X: -200, Y: 280.001, Z: -460},
	}
	onDeck, onCaveFloor, onTerrain, onUpperFloor := 0, 0, 0, 0
	check := func(t *testing.T, s float64, p monster.Pose, walk *navWalk) {
		t.Helper()
		gotY, gotOK := v.monsterPathHeightAt(s, p, walk)
		wantY, wantOK := legacyMonsterPathHeightAt(v, s, p, walk)
		if gotY != wantY || gotOK != wantOK {
			t.Fatalf("%+v at t=%v (walk %t): height (%v, %t), legacy (%v, %t)", p, s, walk != nil, gotY, gotOK, wantY, wantOK)
		}
		switch {
		case !gotOK:
		case simulation.PlaneKindOf(p.RegionID) == simulation.PlaneCave:
			onCaveFloor++
			if gotY > 200 {
				onUpperFloor++
			}
		case math.Abs(gotY-verandaDeckY) < 0.01:
			onDeck++
		case math.Abs(gotY-verandaTerrainY) < 0.01:
			onTerrain++
		}
	}
	for _, probe := range append(planeProbes(outdoor), planeProbes(cave)...) {
		from, to := probe[0], probe[1]
		walk := v.ownerWalk(from, simulation.NavOwner{}, to)
		for i := 0; i < heightParitySamples; i++ {
			s := float64(i) / float64(heightParitySamples-1)
			p := poseOf(from)
			p.X += (to.X - from.X) * s
			p.Z += (to.Z - from.Z) * s
			check(t, s, p, walk)
			// No walk at all: the terrain fallback outdoors.
			check(t, s, p, nil)
		}
	}
	// The deck probe must really reach the deck through the walk, the
	// terrain fallback must really answer, and the cave arm must stand on
	// a floor - or the parity compares two misses.
	if onDeck == 0 || onTerrain == 0 || onCaveFloor == 0 || onUpperFloor == 0 {
		t.Fatalf("deck %d, terrain %d, cave floor %d (upper %d) samples: the probes no longer reach every arm",
			onDeck, onTerrain, onCaveFloor, onUpperFloor)
	}
}
