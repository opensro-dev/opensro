/*
===========================================================================

pick-destination.ts - ray hit to regional world destination

Converts a scene-origin-relative ray hit into its canonical outdoor sector
or dungeon local coordinates before worker navigation admits the walk.

===========================================================================
*/

import { planeKindOf } from "@/engine/foundation/world/plane";
import { normalizeRegionXZ, REGION_SIZE, sectorX, sectorY } from "@/engine/foundation/world/world-space";
import type { PickRay } from "./picking";

/*
================
pickDestination

The render ray is relative to the committed scene's origin region. Convert
once at the picking boundary; worker navigation still admits the destination.
================
*/
export function pickDestination( ray: PickRay, depth: number, originRegion: number ) {
	if ( !Number.isFinite( depth ) || depth < 0 || depth > 1 || !originRegion ) return null;
	const rawX = ray.start[0]! + ray.delta[0]! * depth,
		y = ray.start[1]! + ray.delta[1]! * depth,
		rawZ = ray.start[2]! + ray.delta[2]! * depth;
	if ( ![ rawX, y, rawZ ].every( Number.isFinite ) ) return null;
	if ( planeKindOf( originRegion ) === "outdoor" ) {
		const normalized = normalizeRegionXZ( originRegion, rawX, rawZ );
		const rx = sectorX( originRegion ) + Math.floor( rawX / REGION_SIZE ),
			rz = sectorY( originRegion ) + Math.floor( rawZ / REGION_SIZE );
		if ( rx < 0 || rx > 255 || rz < 0 || rz >= 128 ) return null;
		return { regionId: normalized.regionId, x: normalized.x, y, z: normalized.z };
	}
	return { regionId: originRegion, x: rawX, y, z: rawZ };
}
