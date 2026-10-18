/*
===========================================================================

minimap-floor-key.ts - the shared cell key for dungeon floor lookups

The UI asks the simulation worker which DOF floor a pose sits on, then
looks the answer up by a pose sampled frames later. An exact-float key
misses whenever the entity has walked since the query, so dungeon minimap
markers vanished for as long as anything moved. Both sides instead key a
pose by the coarse cell it falls in; the worker resolves the floor at the
queried pose itself, so a cell's answer is its first querier's floor.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";

// Floor volumes are room-sized; cells this wide keep a walking entity's
// key stable across the worker round trip without straddling rooms often.
export const MINIMAP_FLOOR_CELL_XZ = 16;
export const MINIMAP_FLOOR_CELL_Y = 8;

/*
================
minimapPoseKey
================
*/
export function minimapPoseKey( p: Pick<Pose, "regionId" | "x" | "y" | "z"> ): string {
	return [
		p.regionId,
		Math.floor( p.x / MINIMAP_FLOOR_CELL_XZ ),
		Math.floor( p.y / MINIMAP_FLOOR_CELL_Y ),
		Math.floor( p.z / MINIMAP_FLOOR_CELL_XZ )
	].join( ":" );
}
