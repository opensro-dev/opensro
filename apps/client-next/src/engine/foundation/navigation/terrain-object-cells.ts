/*
===========================================================================

terrain-object-cells.ts - per-cell object candidate lists

Native terrain cells own their object candidate lists (404510); this
decodes the published offsets and indices for a region.

===========================================================================
*/

import type { NavRegion } from "@/engine/contracts/navigation";
import { base64Bytes } from "@/engine/foundation/assets/base64";
/*
================
terrainObjectCells
================
*/
// Native terrain cells own their object candidate lists (404510). Height is
// resolved after the outside outline probe (428300), not a +/-2 edge-Y gate.
export function terrainObjectCells( region: NavRegion ) {
	const c = region.cells, count = c.count, n = region.objects?.length ?? 0;
	if ( c.objectIndexOffsets === undefined ) return undefined;
	/*
	================
	column
	================
	*/
	function column( value: string | undefined, bytes: number ) {
		if ( typeof value !== "string" ) throw Error( "Missing terrain object-cell column" );
		const raw = base64Bytes( value );
		if ( raw.length !== bytes ) throw Error( "Invalid terrain object-cell column" );
		return new DataView( raw.buffer );
	}
	if ( !Number.isInteger( count ) || count < 0 || count > 65536 ) throw Error( "Invalid terrain object-cell count" );
	const offsets = column( c.objectIndexOffsets, (count + 1) * 4 ), length = offsets.getUint32( count * 4, true );
	if ( length > 1048576 ) throw Error( "Terrain object-cell budget" );
	const ids = column( c.objectIndices, length * 2 ),
		columns = [ c.minX, c.minZ, c.maxX, c.maxZ ].map( v => column( v, count * 4 ) );
	const result: Array<Array<readonly [number, number, number, number]>> = Array.from( { length: n }, () => [] );
	if ( offsets.getUint32( 0, true ) !== 0 ) throw Error( "Invalid terrain object-cell offsets" );
	for ( let i = 0; i < count; i++ ) {
		const a = offsets.getUint32( i * 4, true ), b = offsets.getUint32( (i + 1) * 4, true );
		const r = columns.map( v => v.getFloat32( i * 4, true ) );
		if ( a > b || b > length || !r.every( Number.isFinite ) || r[0]! > r[2]! || r[1]! > r[3]! ) {
			throw Error( "Invalid terrain object cell" );
		}
		const rect: readonly [number, number, number, number] = [
			r[0]! + region.dx * 1920,
			r[1]! + region.dz * 1920,
			r[2]! + region.dx * 1920,
			r[3]! + region.dz * 1920
		];
		for ( let j = a; j < b; j++ ) {
			const id = ids.getUint16( j * 2, true );
			if ( id >= n ) throw Error( "Invalid terrain object reference" );
			result[id]!.push( rect );
		}
	}
	return result;
}
