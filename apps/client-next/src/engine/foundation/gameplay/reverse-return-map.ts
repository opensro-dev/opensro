/*
===========================================================================

reverse-return-map.ts - the reverse return scroll's map destinations

Port-only, not native. v1.150's scroll offers two points (count-job.ts
REVERSE_RETURN_LAST_*). Later clients add "move to a location on the map":
v1.188's 4A00C0 reads choice 7 and a u32 saved-point id. The server
publishes its table of points in the world references only when its
SRO_REVERSE_RETURN_MAP option is on, and the Experimental window's row
must be on too. A use sends only the point id; the server resolves it.
The map choice was first written by GrazKe in #336.

===========================================================================
*/

// REVERSE_RETURN_MAP is v1.188's saved-point choice (4A00C0 case 7).
export const REVERSE_RETURN_MAP = 7;
// MAX_REVERSE_MAP_POINTS bounds the published table (server maxReverseMapPoints).
const MAX_REVERSE_MAP_POINTS = 4096;
// MAX_REVERSE_MAP_NAME bounds a point name (server maxReverseMapNameBytes).
const MAX_REVERSE_MAP_NAME = 128;
// REVERSE_MAP_REGION_SIZE is a region's local extent: x and z lie in [0, 1920).
const REVERSE_MAP_REGION_SIZE = 1920;
// A field region id has bit 15 clear.
const DUNGEON_REGION_BIT = 0x8000;

/*
================
ReverseMapPoint

One published destination. The map paints it from these coordinates.
================
*/
export interface ReverseMapPoint {
	readonly id: number;
	readonly name: string;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
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
			point.regionId! < 1 || (point.regionId! & DUNGEON_REGION_BIT) !== 0 || point.regionId! > 0xffff ||
			![ point.x, point.y, point.z ].every( Number.isFinite ) || point.x! < 0 ||
			point.x! >= REVERSE_MAP_REGION_SIZE || point.z! < 0 || point.z! >= REVERSE_MAP_REGION_SIZE
		) throw Error( "Invalid reverse return map point" );
		return { id: point.id, name: point.name, regionId: point.regionId!, x: point.x!, y: point.y!, z: point.z! };
	} );
}

/*
================
reverseMapTail

The item-use tail for a map point: choice 7 and the id, little-endian.
================
*/
export function reverseMapTail( id: number ): Uint8Array {
	if ( !Number.isInteger( id ) || id < 1 || id > 0xffffffff ) throw Error( "Choose a point on the map" );
	const tail = new Uint8Array( 5 );
	tail[0] = REVERSE_RETURN_MAP;
	new DataView( tail.buffer ).setUint32( 1, id, true );
	return tail;
}
