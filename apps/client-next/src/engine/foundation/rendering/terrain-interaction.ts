/*
===========================================================================

terrain-interaction.ts - terrain picking and the selection decal

Ground picks against the terrain cells (88D340) and the decal laid under
a selected point (CIODecal). Picks run on clicks and hover, so they fold
heights instead of spreading them.

===========================================================================
*/

import type { TerrainRange, WorldScene } from "@/engine/contracts/scene";
import type { Geometry } from "@/engine/contracts/geometry";
import { identity } from "./world-math";
import { pickGeometry, type PickRay } from "./picking";
/*
================
selectionTextures
================
*/
export function selectionTextures() {
	return [ 1, 2, 3, 4 ].map( i => `/assets/images/Media_extracted/effect/select_0${i}.png` );
}
// Terrain cells by terrainCellKey: one 320-unit block of heights each.
export type TerrainCells = ReadonlyMap<number, TerrainRange>;

/*
================
terrainCellKey

A numeric key for a terrain cell, which may lie on either side of the
scene anchor. Rebuilding the cell map keyed by joined strings cost a
scene switch thousands of string allocations.
================
*/
export function terrainCellKey( cx: number, cz: number ): number {
	return (cx + 0x8000) * 0x10000 + (cz + 0x8000);
}

/*
================
terrainInteractionCells
================
*/
export function terrainInteractionCells( scene: WorldScene | null ): Map<number, TerrainRange> {
	const cells = new Map<number, TerrainRange>();
	for ( const group of scene?.groups ?? [] ) {
		if ( group.material.terrain ) {
			for ( const range of group.ranges ?? [] ) {
				cells.set( terrainCellKey( range.cell[0], range.cell[1] ), range );
			}
		}
	}
	return cells;
}
/*
================
tile
================
*/
function tile( cells: TerrainCells, x: number, z: number ) {
	const cx = Math.floor( x / 16 ), cz = Math.floor( z / 16 ), cell = cells.get( terrainCellKey( cx, cz ) );
	if ( !cell ) return null;
	const ix = x - cx * 16,
		iz = z - cz * 16,
		read = ( dx: number, dz: number ) => cell.heights[(iz + dz) * 17 + ix + dx]!;
	const corners = [ [ x * 20, read( 0, 0 ), z * 20 ], [ x * 20, read( 0, 1 ), (z + 1) * 20 ], [
		(x + 1) * 20,
		read( 1, 1 ),
		(z + 1) * 20
	], [ (x + 1) * 20, read( 1, 0 ), z * 20 ] ];
	return (x & 1) === (z & 1) ? corners : [ corners[3]!, corners[0]!, corners[1]!, corners[2]! ];
}
// A cell's heights never change while it is resident: its bounds and pick
// triangles are built once, not on every click and hover. The world renderer
// owns one cache; a caller without one rebuilds per pick.
export type TerrainPickCache = WeakMap<TerrainRange, { bounds: Float64Array; geometry: Geometry; }>;

/*
================
createTerrainPickCache
================
*/
export function createTerrainPickCache(): TerrainPickCache {
	return new WeakMap();
}

/*
================
cellPick

The bounds and pick triangles of a terrain cell. 88D340: special water
contributes two extra triangles; all paths then continue into the terrain
loop at 88D93C. Land can lie ABOVE the plane. Min/max keep NaN propagation.
================
*/
function cellPick( cells: TerrainCells, cell: TerrainRange, cache: TerrainPickCache ) {
	let pick = cache.get( cell );
	if ( pick ) return pick;
	const [cx, cz] = cell.cell, special = cell.water?.type === 1 && cell.water.waveType !== 0;
	let lo = special ? cell.water!.height : Infinity, hi = special ? cell.water!.height : -Infinity;
	for ( let i = 0; i < cell.heights.length; i++ ) {
		const h = cell.heights[i]!;
		lo = Math.min( lo, h );
		hi = Math.max( hi, h );
	}
	const positions: number[] = [], indices: number[] = [];
	/*
	================
	append
	================
	*/
	function append( corners: readonly (readonly number[])[] ) {
		const n = positions.length / 3;
		for ( const corner of corners ) positions.push( corner[0]!, corner[1]!, corner[2]! );
		indices.push( n, n + 1, n + 2, n, n + 2, n + 3 );
	}
	if ( special ) {
		const h = cell.water!.height;
		append( [
			[ cx * 320, h, cz * 320 ],
			[ cx * 320, h, (cz + 1) * 320 ],
			[ (cx + 1) * 320, h, (cz + 1) * 320 ],
			[ (cx + 1) * 320, h, cz * 320 ]
		] );
	}
	for ( let z = cz * 16; z < (cz + 1) * 16; z++ ) {
		for ( let x = cx * 16; x < (cx + 1) * 16; x++ ) append( tile( cells, x, z )! );
	}
	pick = {
		bounds: Float64Array.of( cx * 320, lo, cz * 320, (cx + 1) * 320, hi, (cz + 1) * 320 ),
		geometry: {
			positions: new Float32Array( positions ),
			indices: new Uint32Array( indices ),
			transform: identity()
		}
	};
	cache.set( cell, pick );
	return pick;
}

/*
================
boxEntry

Where ray enters an axis-aligned box (rayIntersectsBounds's slab test with
the same padding), or Infinity when it misses within [0, 1]. A hit inside
the box is never nearer than its entry.
================
*/
function boxEntry( ray: PickRay, b: Float64Array ): number {
	let enter = 0, exit = 1;
	for ( let axis = 0; axis < 3; axis++ ) {
		const low = b[axis]! - 1e-5, high = b[axis + 3]! + 1e-5, delta = ray.delta[axis]!, start = ray.start[axis]!;
		if ( Math.abs( delta ) < 1e-15 ) {
			if ( start < low || start > high ) return Infinity;
			continue;
		}
		const a = (low - start) / delta, c = (high - start) / delta;
		enter = Math.max( enter, Math.min( a, c ) );
		exit = Math.min( exit, Math.max( a, c ) );
		if ( enter > exit ) return Infinity;
	}
	return enter;
}

/*
================
pickTerrainCells

The nearest terrain hit along ray (0..1), or null. Cells are tested in
order of where the ray enters them, and the walk stops at the first cell
entered beyond the best hit: the answer is the same minimum as testing
every cell, without building every cell's triangles on each pick.
================
*/
export function pickTerrainCells( cells: TerrainCells, ray: PickRay, cache = createTerrainPickCache() ) {
	const candidates: { entry: number; cell: TerrainRange; }[] = [];
	for ( const cell of cells.values() ) {
		const entry = boxEntry( ray, cellPick( cells, cell, cache ).bounds );
		if ( entry !== Infinity ) candidates.push( { entry, cell } );
	}
	candidates.sort( ( a, b ) => a.entry - b.entry );
	let best = Infinity;
	for ( const { entry, cell } of candidates ) {
		if ( entry > best ) break;
		const pick = cellPick( cells, cell, cache );
		const t = pickGeometry( ray, pick.geometry, pick.geometry.transform );
		if ( t !== null && t < best ) best = t;
	}
	return Number.isFinite( best ) ? best : null;
}
/*
================
selectionDecalGeometry

CIODecal has yaw zero and size twelve. This is the corresponding stored
LookAtLH * OrthoLH projection, preserving the native float32 cancellation.
================
*/
export function selectionDecalGeometry(
	cells: TerrainCells,
	point: readonly [number, number, number]
): Geometry | null {
	const f = Math.fround,
		[x, y, z] = point.map( f ),
		center = cells.get( terrainCellKey( Math.floor( x! / 320 ), Math.floor( z! / 320 ) ) );
	if ( !center ) return null;
	const positions: number[] = [],
		uvs: number[] = [],
		indices: number[] = [],
		k = f( 2 / 12 ),
		u0 = f( -z! * k ),
		v0 = f( -x! * k );
	const append = ( corners: readonly number[][], flat = false ) => {
		const n = positions.length / 3;
		for ( const [i, p] of corners.entries() ) {
			positions.push( p[0]!, p[1]!, p[2]! );
			if ( flat ) uvs.push( ...[ [ 0, 0 ], [ 1, 0 ], [ 1, 1 ], [ 0, 1 ] ][i]! );
			else uvs.push( f( f( k * p[2]! + u0 ) * .5 + .5 ), f( f( k * p[0]! + v0 ) * .5 + .5 ) );
		}
		indices.push( n, n + 1, n + 2, n, n + 2, n + 3 );
	};
	const tx = Math.floor( x! / 20 ), tz = Math.floor( z! / 20 );
	for ( let iz = tz - 1; iz <= tz + 1; iz++ ) {
		for ( let ix = tx - 1; ix <= tx + 1; ix++ ) {
			const corners = tile( cells, ix, iz );
			if ( corners ) append( corners );
		}
	}
	if ( center.water?.type === 1 && center.water.waveType !== 0 ) {
		const h = f( y! + f( .1 ) );
		append( [ [ f( x! - 6 ), h, f( z! - 6 ) ], [ f( x! - 6 ), h, f( z! + 6 ) ], [ f( x! + 6 ), h, f( z! + 6 ) ], [
			f( x! + 6 ),
			h,
			f( z! - 6 )
		] ], true );
	}
	if ( !indices.length ) return null;
	return {
		world: true,
		positions: new Float32Array( positions ),
		uvs: new Float32Array( uvs ),
		indices: new Uint32Array( indices ),
		transform: identity(),
		instances: identity(),
		material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: true, doubleSided: false, unlit: true, decal: true }
	};
}
