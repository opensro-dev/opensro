/*
===========================================================================

gmwarp.go - GM /warp destination validation and height normalization

Native GM /wp (the server's special-command handler, case 0xC, through
its teleport-to-position, enter-world and move-to steps and the region
manager's point-valid and nav-cell checks) admits a warp when the target
resolves to a valid object-nav cell or walkable terrain tile and lifts
below-terrain Y onto the resolved surface.

===========================================================================
*/
package enterworld

import (
	"math"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
ResolveGMWarpDestination

Validates and normalizes a GM /warp target position inside the character
authority door without mutating the character. The entry checks (the GM
privilege, finite coordinates, a nonzero region) live here; the
validation itself is the position's plane's (gmwarp_plane.go): a cave
destination by its dungeon spawn surface alone, an outdoor one by the
region frame, the terrain height and the stranded check.
================
*/
func (d *Deps) ResolveGMWarpDestination(c *Character, p simulation.Spawn) (simulation.Spawn, bool) {
	if c == nil || !c.GMPrivilege || d.CanEnterWorldRegion == nil || !d.CanEnterWorldRegion(c, p.RegionID) {
		return p, false
	}
	for _, v := range []float64{p.X, p.Y, p.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return p, false
		}
	}
	if p.RegionID == 0 {
		return p, false
	}
	return d.warpDestinationOnPlane(simulation.PlaneKindOf(p.RegionID), p)
}
