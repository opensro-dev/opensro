/*
===========================================================================

dungeon_clip.go - a mover's step through a dungeon resident host

clipDungeonPath is the dungeon plane's clip: the original's cell-by-cell
dungeon walk (dungeon_walk.go, #1728) from the cell under the start. The
cave plane's clip (plane_clip.go) walks from the mover's retained cell
instead. The result bits match the original's edge walk (#1712).

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// maxDungeonFloorSampleSpacing is the widest XZ gap between floor samples
// the cave route's string-pull takes along a chord (cave_route.go).
const maxDungeonFloorSampleSpacing = 2.0

// maxDungeonFloorStep is the largest height change between two floor
// samples the cave route takes as the same floor (cave_route.go).
const maxDungeonFloorStep = 2.0

// dungeonWallFlags are the edge flags the native edge walk stops at with
// the clipped bit (client 428B36; the server's twin tests the same mask).
const dungeonWallFlags = 0x12

/*
================
dungeonOutlineResult

The native result bit for a chord stopped on a dungeon block's outline
edge (#1712). The edge walk, run with its stop flag set (the movement
query of the AI's route lookahead and of the spawn test both set it),
stops at an edge whose flags carry 0x12 - a wall; every unlinked outline
edge of Donwhang's blocks but 14 carries 3 - and reports bit 0, clipped
(client 428B36..428B3B). Bit 28 comes only from a doorway (flag 8) whose
neighbouring block cannot be resolved, from an edge event, or from the
query's own guards, so a doorway edge the walk finds no link for stays
blocked.
================
*/
func dungeonOutlineResult(flags byte) uint32 {
	if flags&dungeonWallFlags != 0 {
		return monster.NavResultClipped
	}
	return monster.NavResultBlocked
}

/*
================
clipDungeonPath

The whole dungeon move from the cell under the start (the original's
relocation rule) to the destination: the walk's legs through the shared
query loop (ClipMovementPathFrom), doorway by doorway. Missing coverage and
cross-dungeon requests keep the start with bit 28; teleportation owns
region changes.
================
*/
func (v *WaterValidator) clipDungeonPath(from, to simulation.Spawn) ClipReport {
	return v.ClipMovementPathFrom(from, simulation.NavOwner{}, to)
}
