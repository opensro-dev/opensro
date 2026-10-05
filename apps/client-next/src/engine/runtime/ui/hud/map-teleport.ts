/*
===========================================================================

map-teleport.ts - world map double-click teleport target

DELIBERATE ADDITION, NOT NATIVE: retail's world map handles only mouse move,
down and up (CIFWorldMap_OnMouseMessage 57F430). This is a GM-only port tool.

A double-click on the world map picks an outdoor point and asks
for confirmation; Yes issues the existing GM coordinate warp. The server owns
privilege, area, surface and stranding checks, and snaps the unknown height
(MAP_WARP_UNKNOWN_Y lies below every terrain, so entry lifts it to ground).

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiRect } from "@/engine/contracts/ui";
import { worldMapPoint } from "@/engine/foundation/ui/world-map";

const MAP_WARP_UNKNOWN_Y = -10000;
// MAP_WARP_LOCAL_MAX keeps a printed coordinate inside its region: the server
// rejects a local x or z of 1920 (ResolveGMWarpDestination), and toFixed(1)
// would round 1919.96 up to it.
const MAP_WARP_LOCAL_MAX = 1919.9;

/*
================
MapTeleportTarget
================
*/
export interface MapTeleportTarget {
	readonly regionId: number;
	readonly x: number;
	readonly z: number;
}

/*
================
createMapTeleport

The last painted map frame and the target awaiting confirmation.
================
*/
export function createMapTeleport() {
	let frame: { page: number; clip: UiRect; pan: readonly [number, number]; center: Pose; } | null = null;
	let pending: MapTeleportTarget | null = null;
	return {
		view( page: number, clip: UiRect, pan: readonly [number, number], center: Pose ) {
			frame = { page, clip, pan: [ pan[0], pan[1] ], center };
		},
		pick( x: number, y: number ): MapTeleportTarget | null {
			pending = frame ? worldMapPoint( frame.page, frame.clip, frame.pan, frame.center, x, y ) : null;
			return pending;
		},
		pending(): MapTeleportTarget | null {
			return pending;
		},
		clear() {
			pending = null;
		},
		command( target: MapTeleportTarget ): string {
			const x = Math.min( target.x, MAP_WARP_LOCAL_MAX ), z = Math.min( target.z, MAP_WARP_LOCAL_MAX );
			return `/warp ${target.regionId} ${x.toFixed( 1 )} ${MAP_WARP_UNKNOWN_Y} ${z.toFixed( 1 )}`;
		}
	};
}
