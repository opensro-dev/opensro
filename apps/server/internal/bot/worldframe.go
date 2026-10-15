/*
===========================================================================

worldframe.go - the seed-local world frame for live-position interpolation

The bot reckons its live position along a committed movement leg, because the
0xB738 acknowledgement carries the goal, not the live position. Interpolating
in the region-local frame of either endpoint flips the reported region the
instant the goal is adopted, so a cross-region leg shows a phantom region. A
WorldFrame fixes one seed region (the bot's start region) and expresses every
position in that frame; the region and locals are then resolved from the
interpolated world point at each step, so the estimate stays continuous across
a boundary.

Phase 2 of issue #74 (issue #76); defect F1 of issue #143.

===========================================================================
*/
package bot

import "opensro.online/server/internal/game/world/simulation"

// WorldFrame is the fixed seed-local frame used to interpolate the bot's live
// position across region boundaries. SeedRegion is the bot's start region and
// does not change for the session.
type WorldFrame struct {
	SeedRegion uint16
}

// ToWorld re-expresses a region-local spawn in the seed-local world frame. The
// y coordinate passes through untouched.
func (f WorldFrame) ToWorld(spawn simulation.Spawn) simulation.Vec3 {
	if simulation.IsDungeonRegion(f.SeedRegion) {
		// The dungeon plane is a single non-sectorized region word: its locals
		// are not bounded by the outdoor grid, so the sector fold does not
		// apply. Keep the locals and the seed region.
		return simulation.Vec3{X: spawn.X, Y: spawn.Y, Z: spawn.Z}
	}
	return simulation.RegionLocalToSeedLocal(f.SeedRegion, spawn.RegionID,
		simulation.Vec3{X: spawn.X, Y: spawn.Y, Z: spawn.Z}, simulation.NativeRegionSize)
}

// FromWorld resolves a seed-local position back to the region and local
// coordinates it lands in. The region is the sector the point falls into and
// the locals are that region's remainder, so a point that crossed a boundary is
// reported in the NEW region instead of snapping to the goal.
func (f WorldFrame) FromWorld(world simulation.Vec3) simulation.Spawn {
	if simulation.IsDungeonRegion(f.SeedRegion) {
		return simulation.Spawn{RegionID: f.SeedRegion, X: world.X, Y: world.Y, Z: world.Z}
	}
	regionID := simulation.RegionIDFromSeedLocal(f.SeedRegion, world.X, world.Z, simulation.NativeRegionSize)
	local := simulation.SeedLocalToRegionLocal(f.SeedRegion, regionID, world, simulation.NativeRegionSize)
	return simulation.Spawn{RegionID: regionID, X: local.X, Y: local.Y, Z: local.Z}
}

// InterpolateWorld returns the point between from and goal at fraction t,
// clamped to [0, 1]. Both points are in the same seed-local world frame.
func InterpolateWorld(from, goal simulation.Vec3, t float64) simulation.Vec3 {
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return goal
	}
	return simulation.Vec3{
		X: from.X + (goal.X-from.X)*t,
		Y: from.Y + (goal.Y-from.Y)*t,
		Z: from.Z + (goal.Z-from.Z)*t,
	}
}
