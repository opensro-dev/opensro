/*
===========================================================================

terrain-associations.ts - native terrain texture coverage and sampling rules

===========================================================================
*/
/*
================
passes
================
*/
// Native association ordering/claim/fringe algorithm (sub_8b3aa0). Unlike a
// dominant-texture shortcut, every surviving association has a corner mask.
export function passes( words: readonly number[], step: number ) {
	const n = 16 / step, axis = n + 1, pad = n + 2, keys = new Set<number>();
	const sampled = [];
	for ( let z = 0; z <= n; z++ ) {
		for ( let x = 0; x <= n; x++ ) {
			const w = words[z * step * 17 + x * step]!;
			const key = ((w & 1023) << 6) | ((w >>> 13) & 7);
			sampled.push( w );
			keys.add( key );
		}
	}
	const claimed = new Uint8Array( n * n ), result: { x: number; z: number; key: number; mask: number; }[] = [];
	for ( const key of [ ...keys ].sort( ( a, b ) => a - b ).slice( 0, 49 ) ) {
		const mask = new Uint8Array( axis * axis ), cells = new Uint8Array( pad * pad );
		for ( let z = 0; z <= n; z++ ) {
			for ( let x = 0; x <= n; x++ ) {
				if ( sampled[z * axis + x] === ((key >>> 6) | ((key & 7) << 13)) ) {
					mask[z * axis + x] = 1;
					const t = z * pad + x;
					cells[t] =
						cells[t + 1] =
						cells[t + pad] =
						cells[t + pad + 1] =
							2;
				}
			}
		}
		for ( let z = 0; z < n; z++ ) {
			for ( let x = 0; x < n; x++ ) {
				const i = z * n + x, t = (z + 1) * pad + x + 1;
				if ( !(cells[t]! & 254) || claimed[i] ) continue;
				const v = z * axis + x;
				mask[v] =
					mask[v + 1] =
					mask[v + axis] =
					mask[v + axis + 1] =
						1;
				claimed[i] = 1;
				for ( let dz = -1; dz <= 1; dz++ ) {
					for ( let dx = -1; dx <= 1; dx++ ) if ( dx || dz ) cells[t + dz * pad + dx]! |= 1;
				}
			}
		}
		for ( let z = 0; z < n; z++ ) {
			for ( let x = 0; x < n; x++ ) {
				if ( cells[(z + 1) * pad + x + 1] ) {
					const i = z * axis + x,
						m = mask[i]! | (mask[i + 1]! << 1) | (mask[i + axis]! << 2) | (mask[i + axis + 1]! << 3);
					if ( m ) result.push( { x: x * step, z: z * step, key, mask: m } );
				}
			}
		}
	}
	const last = new Map<number, number>();
	for ( let i = 0; i < result.length; i++ ) {
		const p = result[i]!;
		if ( p.mask === 15 ) last.set( p.z * 17 + p.x, i );
	}
	return result.filter( ( p, i ) => i >= (last.get( p.z * 17 + p.x ) ?? 0) );
}

/*
================
heightRange

Lowest and highest of a terrain block's heights. Folded rather than spread:
the spread copied 289 values up to six times per block per detail level.
Math.min/Math.max keep their NaN and signed-zero results.
================
*/
export function heightRange( heights: readonly number[] ): { readonly min: number; readonly max: number; } {
	let min = Infinity, max = -Infinity;
	for ( let i = 0; i < heights.length; i++ ) {
		min = Math.min( min, heights[i]! );
		max = Math.max( max, heights[i]! );
	}
	return { min, max };
}

/*
================
tileUvScale

Texture repeat for a tile association's scale code (low three key bits).
================
*/
export function tileUvScale( code: number ): number {
	switch ( code ) {
		case 0:
			return 1;
		case 1:
			return .5;
		case 2:
			return .25;
		case 3:
			return 2;
		case 4:
			return 4;
		default:
			return 0;
	}
}

// Terrain block grid: 17x17 height samples, 20 units between them.
const BLOCK_GRID_SAMPLES = 17;
const BLOCK_CELL_SIZE = 20;

/*
================
terrainBlockNormals

One normal per height sample of a 17x17 block, from central differences
over the 20-unit cells (one-sided at the borders). Every terrain pass and
detail level shares the table. Retail terrain carries no normals (its
shading is the lightmap's); the terrain-relief stage is their only reader,
so the values are data, not a look change.
================
*/
export function terrainBlockNormals( heights: readonly number[] ): Float32Array {
	const normals = new Float32Array( BLOCK_GRID_SAMPLES * BLOCK_GRID_SAMPLES * 3 );
	for ( let z = 0; z < BLOCK_GRID_SAMPLES; z++ ) {
		for ( let x = 0; x < BLOCK_GRID_SAMPLES; x++ ) {
			const westX = Math.max( 0, x - 1 ),
				eastX = Math.min( BLOCK_GRID_SAMPLES - 1, x + 1 ),
				northZ = Math.max( 0, z - 1 ),
				southZ = Math.min( BLOCK_GRID_SAMPLES - 1, z + 1 );
			// Slope per unit across the samples' actual span: borders keep one
			// neighbour, so their difference covers one cell, not two.
			const gradientX = (heights[z * BLOCK_GRID_SAMPLES + eastX]! - heights[z * BLOCK_GRID_SAMPLES + westX]!) /
					((eastX - westX) * BLOCK_CELL_SIZE),
				gradientZ = (heights[southZ * BLOCK_GRID_SAMPLES + x]! - heights[northZ * BLOCK_GRID_SAMPLES + x]!) /
					((southZ - northZ) * BLOCK_CELL_SIZE);
			const inverse = 1 / Math.hypot( gradientX, 1, gradientZ ),
				at = (z * BLOCK_GRID_SAMPLES + x) * 3;
			normals[at] = -gradientX * inverse;
			normals[at + 1] = inverse;
			normals[at + 2] = -gradientZ * inverse;
		}
	}
	return normals;
}
