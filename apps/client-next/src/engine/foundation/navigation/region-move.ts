/*
===========================================================================

region-move.ts - cross-sector navigation continuation protocol

Re-expresses a movement destination in each continuation sector's frame and
decides whether the navigation step stops, continues, or rejects (0x00412230).

===========================================================================
*/

import type { Pose } from "@/engine/contracts/gameplay";
import { REGION_SIZE, regionRelativeXZ, sameCoordinateSpace } from "@/engine/foundation/world/world-space";

/*
================
RegionMoveResult
================
*/
export interface RegionMoveResult {
	readonly point: Pose | null;
	readonly status: number;
}

/*
================
regionMoveDestination

412230 re-expresses the original destination in each continuation's sector.
================
*/
export function regionMoveDestination( to: Pose, regionId: number ): Pose {
	const [x, z] = regionRelativeXZ( regionId, to );
	return {
		...to,
		regionId,
		x: Math.fround( x ),
		z: Math.fround( z )
	};
}

/*
================
regionMoveAllowed
================
*/
export function regionMoveAllowed( from: Pose, to: Pose, limit = 0x7fffffff ): boolean {
	if ( !sameCoordinateSpace( from.regionId, to.regionId ) ) return false;
	const target = regionMoveDestination( to, from.regionId );
	return !(limit > 10 && (target.x - from.x > REGION_SIZE || target.z - from.z > REGION_SIZE));
}

/*
================
regionMoveContinuation

Only the movement owner dispatches segments. This helper returns a decision;
it cannot call back into runtime capabilities from the foundation layer.
================
*/
export function regionMoveContinuation(
	start: Pose,
	target: Pose,
	result: RegionMoveResult,
	calls: number
): "stop" | "continue" | "reject" {
	if ( !result.point || (result.status & 0x10000001) || !(result.status & 0x14) ) return "stop";
	const dx = Math.fround( target.x - start.x ),
		dy = Math.fround( target.y - start.y ),
		dz = Math.fround( target.z - start.z );
	if ( Math.fround( dy * dy + dx * dx + dz * dz ) < 25 ) return "stop";
	return calls >= 6 ? "reject" : "continue";
}
