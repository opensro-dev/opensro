package bot_test

import (
	"math"
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/world/simulation"
)

// TestWorldFrameCrossRegionContinuity guards defect F1: a leg that crosses a
// region boundary must keep a continuous world position and must not report the
// goal region before arrival.
func TestWorldFrameCrossRegionContinuity(t *testing.T) {
	const seed = uint16(0x6011)
	frame := bot.WorldFrame{SeedRegion: seed}
	east := simulation.RegionIDForSectors(simulation.SectorX(seed)+1, simulation.SectorY(seed))

	from := simulation.Spawn{RegionID: seed, X: 100, Y: 10, Z: 500}
	goal := simulation.Spawn{RegionID: east, X: 100, Y: 10, Z: 500}
	fromWorld := frame.ToWorld(from)
	goalWorld := frame.ToWorld(goal)

	// The world-frame distance spans the whole leg across the boundary, not
	// just one region's locals.
	if got, want := math.Hypot(goalWorld.X-fromWorld.X, goalWorld.Z-fromWorld.Z), simulation.NativeRegionSize; math.Abs(got-want) > 1e-6 {
		t.Fatalf("world distance = %v, want %v", got, want)
	}

	// At t=0 the estimate is still in the start region: no immediate jump.
	if start := frame.FromWorld(bot.InterpolateWorld(fromWorld, goalWorld, 0)); start.RegionID != seed {
		t.Fatalf("start region = 0x%04X, want the start region 0x%04X", start.RegionID, seed)
	}

	// Every step is a small, continuous world move; the region only changes
	// when the world point crosses the border.
	var prevWorld simulation.Vec3
	crossed := false
	for i := 0; i <= 100; i++ {
		live := frame.FromWorld(bot.InterpolateWorld(fromWorld, goalWorld, float64(i)/100))
		world := frame.ToWorld(live)
		if live.X < 0 || live.X >= simulation.NativeRegionSize || live.Z < 0 || live.Z >= simulation.NativeRegionSize {
			t.Fatalf("step %d local out of range: %+v", i, live)
		}
		if i > 0 {
			if step := math.Hypot(world.X-prevWorld.X, world.Z-prevWorld.Z); step > simulation.NativeRegionSize/100+1e-6 {
				t.Fatalf("step %d jumped %v world units", i, step)
			}
		}
		if live.RegionID == east {
			crossed = true
		}
		prevWorld = world
	}
	if !crossed {
		t.Fatal("the estimate never reached the goal region")
	}

	// The endpoint is exactly the goal.
	end := frame.FromWorld(bot.InterpolateWorld(fromWorld, goalWorld, 1))
	if end.RegionID != east || math.Abs(end.X-goal.X) > 1e-6 || math.Abs(end.Z-goal.Z) > 1e-6 {
		t.Fatalf("endpoint = %+v, want %+v", end, goal)
	}
}

// TestWorldFrameFoldsAcrossTwoSectors checks that a point several sectors out
// resolves to the right region with folded locals.
func TestWorldFrameFoldsAcrossTwoSectors(t *testing.T) {
	const seed = uint16(0x5E9E)
	frame := bot.WorldFrame{SeedRegion: seed}
	goalRegion := simulation.RegionIDForSectors(simulation.SectorX(seed)+2, simulation.SectorY(seed)+1)
	goal := simulation.Spawn{RegionID: goalRegion, X: 400, Y: 0, Z: 700}
	got := frame.FromWorld(frame.ToWorld(goal))
	if got.RegionID != goalRegion || math.Abs(got.X-goal.X) > 1e-6 || math.Abs(got.Z-goal.Z) > 1e-6 {
		t.Fatalf("round trip = %+v, want %+v", got, goal)
	}
}
