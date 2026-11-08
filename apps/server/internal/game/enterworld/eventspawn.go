/*
===========================================================================

eventspawn.go - public event destinations use the ordinary spawn surface

No GM access bypass: missing navigation or a private region refuses entry.

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/game/world/simulation"

/*
================
ResolvePublicEventSpawn
================
*/
func (d *Deps) ResolvePublicEventSpawn(c *Character, p simulation.Spawn) (simulation.Spawn, bool) {
	if c == nil || d.CanEnterWorldRegion == nil {
		return p, false
	}
	ordinary := *c
	ordinary.GMPrivilege = false
	if !d.CanEnterWorldRegion(&ordinary, p.RegionID) {
		return p, false
	}
	return d.warpDestinationOnPlane(simulation.PlaneKindOf(p.RegionID), p)
}
