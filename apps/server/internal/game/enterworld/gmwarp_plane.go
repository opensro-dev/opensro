/*
===========================================================================

gmwarp_plane.go - the planes' GM /warp destination validation (#1646)

ResolveGMWarpDestination (gmwarp.go) used to test the dungeon region
bit itself; the two validations now live here, one arm per plane. A
cave destination stands or falls by its dungeon spawn surface alone.
An outdoor one is frame-checked against its region, lifted onto the
resolved surface (LiftSpawnAboveTerrain over SpawnTerrainHeight and
SpawnSurfaceHeight - WalkableSpawnHeightAt / native CheckPointValid),
and rejected where RelocateStrandedSpawn would rewrite it on re-entry.

===========================================================================
*/
package enterworld

import (
	"math"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
Deps.warpDestinationOnPlane

The destination validation of the position's plane.
================
*/
func (d *Deps) warpDestinationOnPlane(kind simulation.PlaneKind, p simulation.Spawn) (simulation.Spawn, bool) {
	if kind == simulation.PlaneCave {
		return d.caveWarpDestination(p)
	}
	return d.outdoorWarpDestination(p)
}

/*
================
Deps.caveWarpDestination

A cave destination is valid when the dungeon spawn surface covers it.
The surface decides validity only; the caller's Y is never rewritten.
================
*/
func (d *Deps) caveWarpDestination(p simulation.Spawn) (simulation.Spawn, bool) {
	if d.SpawnSurfaceHeight == nil {
		return p, false
	}
	_, ok := d.SpawnSurfaceHeight(p.RegionID, p.X, p.Y, p.Z)
	return p, ok
}

/*
================
Deps.outdoorWarpDestination

An outdoor destination must sit inside its region's 1920-unit frame,
stand on the terrain (lifted onto the resolved surface when the saved
Y sits below it), and not be a point the stranded rescue would rewrite
on re-entry.
================
*/
func (d *Deps) outdoorWarpDestination(p simulation.Spawn) (simulation.Spawn, bool) {
	// Outdoor coordinates are local to one 1920-unit region. Reject malformed
	// frames rather than normalizing an overflow into a different world/region.
	if p.X < 0 || p.X >= 1920 || p.Z < 0 || p.Z >= 1920 || d.SpawnTerrainHeight == nil {
		return p, false
	}
	h, ok := d.SpawnTerrainHeight(p.RegionID, p.X, p.Z)
	if !ok || math.IsNaN(h) || math.IsInf(h, 0) {
		return p, false
	}
	entry := LocalPlayerEntry{}
	entry.StartProfile.RegionID = int64(p.RegionID)
	entry.StartProfile.X = p.X
	entry.StartProfile.Y = p.Y
	entry.StartProfile.Z = p.Z
	LiftSpawnAboveTerrain(&entry, d.SpawnTerrainHeight, d.SpawnSurfaceHeight)
	p.Y = entry.StartProfile.Y
	if d.SpawnSurfaceHeight != nil {
		if _, ok := d.SpawnSurfaceHeight(p.RegionID, p.X, p.Y, p.Z); !ok {
			return p, false
		}
	}
	if d.RelocateStrandedSpawn != nil {
		_, stranded, _ := d.RelocateStrandedSpawn(p)
		if stranded {
			return p, false
		}
	}
	return p, true
}
