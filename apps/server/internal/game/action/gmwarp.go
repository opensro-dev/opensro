/*
===========================================================================

gmwarp.go - owns gmwarp behavior and its authority boundary

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// WarpGM owns relocation, transient action cleanup and ordered world re-entry.
// The GM dispatcher supplies decoded values, never writes character state.
/*
================
WarpGM
================
*/
func (rt *Runtime) WarpGM(division, name string, p wire.Position) bool {
	if rt == nil || rt.deps == nil || rt.PushCharacterFrames == nil {
		return false
	}
	authority, ok := rt.deps.(interface {
		ResolveGMWarpDestination(*enterworld.Character, simulation.Spawn) (simulation.Spawn, bool)
	})
	if !ok {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return false
	}
	requested := simulation.Spawn{RegionID: p.RegionID, X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z), Angle: p.Heading}
	return rt.relocateCharacter(division, c, "gm-warp", func() (travelPoint, bool) {
		if !c.GMPrivilege || !enterworld.CharacterAlive(c) {
			return travelPoint{}, false
		}
		// A summoned COS does not refuse the warp (issue #4422): the warp is
		// the server-driven PC world teleport (relocation.go) that the
		// fortress expulsions already run on riders. relocateCharacter carries
		// the companions to the destination, a mounted transport rides on the
		// rider's own world pose, and re-entry respawns the COS with its ride
		// state (enterworld bootstrap). INFERENCE: native /warp moves a mounted
		// GM like any other teleport; no binary read was possible here.
		destination, admitted := authority.ResolveGMWarpDestination(c, requested)
		// INFERENCE: /warp names field coordinates (the GM tool targets the
		// region grid the field map loads); a GM inside a fortress or instance
		// is brought out to the field rather than left at field coordinates
		// inside a world whose map does not hold them.
		return travelPoint{spawn: destination, world: instance.ID(domain.DefaultWorldInstance)}, admitted
	})
}
