/*
===========================================================================

reverse-return-map.ts - the reverse return scroll's map destinations

Port-only, not native. v1.150's scroll offers two points (count-job.ts
REVERSE_RETURN_LAST_*). Later clients add "move to a location on the map":
v1.188's 4A00C0 reads choice 7 and a u32 saved-point id. The server
publishes its table of points in the world references only when its
SRO_REVERSE_RETURN_MAP option is on; the box offers the map whenever a
table is published. A use sends only the point id; the server resolves it.
The map choice was first written by GrazKe in #336.

A dungeon group (the imported table's RegionIDGroup) is a map point, the
flag, and floor points in dungeon regions. As in the v1.188 client
(sro_client.exe 0x6971c8: the flag's click lists the group's rows whose
MapPoint is not 1), the flag opens a menu of its group's floors, and a
floor, never the flag, is the answer.

===========================================================================
*/

import { featuresAt } from "@/engine/foundation/world/plane";
import { REGION_SIZE } from "@/engine/foundation/world/world-space";

// REVERSE_RETURN_MAP is v1.188's saved-point choice (4A00C0 case 7).
export const REVERSE_RETURN_MAP = 7;
// REVERSE_MAP_FLOOR prefixes a floor menu entry's control id; its point id follows.
export const REVERSE_MAP_FLOOR = "reverse-map-floor:";
// REVERSE_MAP_POINT prefixes a map point button's control id; its point id follows.
export const REVERSE_MAP_POINT = "reverse-map-point:";
// MAX_REVERSE_MAP_POINTS bounds the published table (server maxReverseMapPoints).
const MAX_REVERSE_MAP_POINTS = 4096;
// MAX_REVERSE_MAP_NAME bounds a point name (server maxReverseMapNameBytes).
const MAX_REVERSE_MAP_NAME = 128;

/*
================
ReverseMapPoint

One published destination. The map paints it from these coordinates.
group names its dungeon group (absent for a field point); floor marks a
group's floor, which the map does not paint: its flag's menu lists it.
================
*/
export interface ReverseMapPoint {
	readonly id: number;
	readonly name: string;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly group?: number;
	readonly floor?: boolean;
}

/*
================
reverseMapDestination

Whether a use may name the point: a field point or a floor, not a flag.
================
*/
export function reverseMapDestination( point: ReverseMapPoint ): boolean {
	return point.group === undefined || point.floor === true;
}

/*
================
reverseMapFloors

The floors a flag's menu lists, in table order; none for a field point
or a floor.
================
*/
export function reverseMapFloors(
	points: readonly ReverseMapPoint[],
	flag: ReverseMapPoint
): readonly ReverseMapPoint[] {
	if ( flag.group === undefined || flag.floor ) return [];
	return points.filter( point => point.floor && point.group === flag.group );
}

/*
================
decodeReverseMapPoints

The references' reverseMapPoints: absent is the native empty table. IDs
run 1..n in order, as the server numbers them.
================
*/
export function decodeReverseMapPoints( value: unknown ): readonly ReverseMapPoint[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) || value.length > MAX_REVERSE_MAP_POINTS ) {
		throw Error( "Invalid reverse return map table" );
	}
	return value.map( ( raw, index ) => {
		const point = raw as Partial<ReverseMapPoint> | null;
		if (
			!point || typeof point !== "object" || point.id !== index + 1 || typeof point.name !== "string" ||
			point.name.length > MAX_REVERSE_MAP_NAME || !Number.isInteger( point.regionId ) ||
			point.regionId! < 1 || point.regionId! > 0xffff ||
			![ point.x, point.y, point.z ].every( Number.isFinite ) ||
			point.group !== undefined &&
				(!Number.isInteger( point.group ) || point.group < 1 || point.group > 0xffffffff) ||
			point.floor !== undefined && (point.floor !== true || point.group === undefined)
		) throw Error( "Invalid reverse return map point" );
		// A floor lies in a dungeon, in that floor's own frame; every other
		// point is a field position inside its region.
		const placed = point.floor ?
			!featuresAt( point.regionId! ).reverseReturnPoints :
			featuresAt( point.regionId! ).reverseReturnPoints && point.x! >= 0 && point.x! < REGION_SIZE &&
			point.z! >= 0 && point.z! < REGION_SIZE;
		if ( !placed ) throw Error( "Invalid reverse return map point" );
		return {
			id: point.id,
			name: point.name,
			regionId: point.regionId!,
			x: point.x!,
			y: point.y!,
			z: point.z!,
			...(point.group === undefined ? {} : { group: point.group }),
			...(point.floor ? { floor: true } : {})
		};
	} );
}

/*
================
reverseMapTail

The item-use tail for a map point: choice 7 and the id, little-endian.
================
*/
export function reverseMapTail( id: number ): Uint8Array {
	if ( !Number.isInteger( id ) || id < 1 || id > 0xffffffff ) {
		throw Error( "Choose a point on the map for the reverse return scroll" );
	}
	const tail = new Uint8Array( 5 );
	tail[0] = REVERSE_RETURN_MAP;
	new DataView( tail.buffer ).setUint32( 1, id, true );
	return tail;
}
