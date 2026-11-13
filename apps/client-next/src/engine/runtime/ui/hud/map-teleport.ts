/*
===========================================================================

map-teleport.ts - world map double-click teleport target

DELIBERATE ADDITION, NOT NATIVE: retail's world map handles only mouse move,
down and up (CIFWorldMap_OnMouseMessage 57F430). This is a GM-only port tool.

A double-click on the world map picks an outdoor point, or on a dungeon
floor's page a point of that floor, and asks for confirmation; Yes issues the existing GM coordinate warp. The server owns
privilege, area, surface and stranding checks, and snaps the unknown height
(MAP_WARP_UNKNOWN_Y lies below every terrain, so entry lifts it to ground).

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiRect } from "@/engine/contracts/ui";
import { type WorldMapLayout, worldMapPoint } from "@/engine/foundation/ui/world-map";

const MAP_WARP_UNKNOWN_Y = -10000;
// MAP_WARP_LOCAL_MAX keeps a printed coordinate inside its region: the server
// rejects a local x or z of 1920 (ResolveGMWarpDestination), and toFixed(1)
// would round 1919.96 up to it.
const MAP_WARP_LOCAL_MAX = 1919.9;

/*
================
MapTeleportTarget

reversePoint names a reverse return map point (port-only): Yes answers
the pending scroll with its id instead of issuing a GM warp.
================
*/
export interface MapTeleportTarget {
	readonly regionId: number;
	readonly x: number;
	readonly z: number;
	readonly name?: string;
	readonly reversePoint?: number;
}

/*
================
createMapTeleport

The painted page's inverse pick and the target awaiting confirmation.
================
*/
export function createMapTeleport() {
	// The painted page's inverse pick: the world page's projection or a
	// dungeon floor's (cave-map.ts). Cleared whenever neither is painted,
	// so a double-click never resolves against a page no longer shown.
	let picker: (( x: number, y: number ) => MapTeleportTarget | null) | null = null;
	let pending: MapTeleportTarget | null = null;
	return {
		view( page: number, clip: UiRect, pan: readonly [number, number], center: Pose, layout?: WorldMapLayout ) {
			const frame = { page, clip, pan: [ pan[0], pan[1] ] as const, center, layout };
			// A field point stays inside its region: the server rejects a
			// local x or z of 1920, and toFixed(1) would round 1919.96 up
			// to it. A dungeon floor's coordinates need no such clamp.
			picker = ( x, y ) => {
				const point = worldMapPoint( frame.page, frame.clip, frame.pan, frame.center, x, y, frame.layout );
				return point ?
					{
						...point,
						x: Math.min( point.x, MAP_WARP_LOCAL_MAX ),
						z: Math.min( point.z, MAP_WARP_LOCAL_MAX )
					} :
					null;
			};
		},
		viewCave( pick: ( x: number, y: number ) => MapTeleportTarget | null ) {
			picker = pick;
		},
		hide() {
			picker = null;
		},
		pick( x: number, y: number ): MapTeleportTarget | null {
			pending = picker ? picker( x, y ) : null;
			return pending;
		},
		choose( target: MapTeleportTarget ) {
			pending = target;
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
