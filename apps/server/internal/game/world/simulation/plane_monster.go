/*
===========================================================================

plane_monster.go - the planes' monster create frame and acquisition (#1764)

Two monster sites used to test the dungeon region bit themselves; their
outdoor and cave arms now sit side by side here, picked by PlaneKindOf.
The arms are the former bodies, unchanged: a create row's coordinates
are clamped to the outdoor unsigned packed range only outdoors (a cave's
locals are signed F32 values, as movement sends them), and acquisition
scans the outdoor sight square or the cave's dungeon blocks.

===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/world/monster"

/*
================
spawnFrameXZ

The create row's X and Z for spawn, by its plane.
================
*/
func spawnFrameXZ(spawn Spawn) (x, z float64) {
	if PlaneKindOf(spawn.RegionID) == PlaneCave {
		return caveSpawnFrameXZ(spawn)
	}
	return outdoorSpawnFrameXZ(spawn)
}

/*
================
outdoorSpawnFrameXZ

Outdoor coordinates clamp to the unsigned packed-movement range.
================
*/
func outdoorSpawnFrameXZ(spawn Spawn) (x, z float64) {
	return clampFloat(spawn.X, 0, 0xffff), clampFloat(spawn.Z, 0, 0xffff)
}

/*
================
caveSpawnFrameXZ

Indoor local coordinates are signed F32 values, not outdoor unsigned
packed-movement coordinates; scope creates must agree with movement.
================
*/
func caveSpawnFrameXZ(spawn Spawn) (x, z float64) {
	return spawn.X, spawn.Z
}

/*
================
acquisitionBlocks

The blocks a monster at from scans for targets, by its plane.
================
*/
func acquisitionBlocks(from monster.Pose, sight uint32) acquisitionBlockSet {
	if PlaneKindOf(from.RegionID) == PlaneCave {
		return dungeonAcquisitionBlocks(from)
	}
	return outdoorAcquisitionBlocks(from, sight)
}
