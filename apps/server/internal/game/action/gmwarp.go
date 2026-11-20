/*
===========================================================================

gmwarp.go - owns gmwarp behavior and its authority boundary

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

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
	// A refused warp is otherwise silent on the server: name the stage that turned it away.
	why := ""
	moved := rt.relocateCharacter(division, c, "gm-warp", func() (travelPoint, bool) {
		if !c.GMPrivilege {
			why = "the character has no GM privilege"
			return travelPoint{}, false
		}
		if !enterworld.CharacterAlive(c) {
			why = "the character is dead"
			return travelPoint{}, false
		}
		// Active COS need a joint owner/follower migration; do not split poses.
		if c.ActiveCOS != nil && c.ActiveCOS.Summoned {
			why = "a pet or transport is summoned"
			return travelPoint{}, false
		}
		destination, admitted := authority.ResolveGMWarpDestination(c, requested)
		if !admitted {
			why = "the destination is not valid terrain (region, frame, height or surface)"
		}
		// INFERENCE: /warp names field coordinates (the GM tool targets the
		// region grid the field map loads); a GM inside a fortress or instance
		// is brought out to the field rather than left at field coordinates
		// inside a world whose map does not hold them.
		return travelPoint{spawn: destination, world: instance.ID(domain.DefaultWorldInstance)}, admitted
	})
	if !moved {
		if why == "" {
			why = "the world re-entry failed after the destination was admitted"
		}
		log.WithFields(log.Fields{"character": name, "region": p.RegionID, "x": p.X, "y": p.Y, "z": p.Z, "why": why}).Info("gm-warp: refused")
	}
	return moved
}
