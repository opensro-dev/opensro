/*
===========================================================================

map-teleport.ts - world map double-click teleport target

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
			return `/warp ${target.regionId} ${target.x.toFixed( 1 )} ${MAP_WARP_UNKNOWN_Y} ${target.z.toFixed( 1 )}`;
		}
	};
}
