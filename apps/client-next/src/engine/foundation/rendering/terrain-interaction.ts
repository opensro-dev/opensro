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
import { pickGeometry, rayIntersectsBounds, type PickRay } from "./picking";
/*
================
selectionTextures
================
*/
export function selectionTextures() {
	return [ 1, 2, 3, 4 ].map( i => `/assets/images/Media_extracted/effect/select_0${i}.png` );
}
/*
================
terrainInteractionCells
================
*/
export function terrainInteractionCells( scene: WorldScene | null ) {
	const cells = new Map<string, TerrainRange>();
	for ( const group of scene?.groups ?? [] ) {
		if ( group.material.terrain ) {
			for ( const range of group.ranges ?? [] ) cells.set( range.cell.join( ":" ), range );
		}
	}
	return cells;
}
/*
================
tile
================
*/
function tile( cells: ReadonlyMap<string, TerrainRange>, x: number, z: number ) {
	const cx = Math.floor( x / 16 ), cz = Math.floor( z / 16 ), cell = cells.get( cx + ":" + cz );
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
/*
================
pickTerrainCells
================
*/
export function pickTerrainCells( cells: ReadonlyMap<string, TerrainRange>, ray: PickRay ) {
	const transform = identity();
	let best = Infinity;
	for ( const cell of cells.values() ) {
		const [cx, cz] = cell.cell, special = cell.water?.type === 1 && cell.water.waveType !== 0;
		// 88D340: special water contributes two extra triangles; all paths then
		// continue into the terrain loop at 88D93C. Land can lie ABOVE the plane.
		// Folded rather than spread: a spread of 289 heights per cell per pick
		// allocated tens of MB a minute. Math.min/max keep their NaN propagation.
		let lo = special ? cell.water!.height : Infinity, hi = special ? cell.water!.height : -Infinity;
		for ( let i = 0; i < cell.heights.length; i++ ) {
			const h = cell.heights[i]!;
			lo = Math.min( lo, h );
			hi = Math.max( hi, h );
		}
		if (
			!rayIntersectsBounds(
				ray,
				[ cx * 320, lo, cz * 320, (cx + 1) * 320, hi, (cz + 1) * 320 ],
				transform,
				Math.min( best, 1 )
			)
		) continue;
		const positions: number[] = [], indices: number[] = [];
		const append = ( corners: readonly number[][] ) => {
			const n = positions.length / 3;
			positions.push( ...corners.flat() );
			indices.push( n, n + 1, n + 2, n, n + 2, n + 3 );
		};
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
		const t = pickGeometry( ray, {
			positions: new Float32Array( positions ),
			indices: new Uint32Array( indices ),
			transform
		}, transform );
		if ( t !== null && t < best ) best = t;
	}
	return Number.isFinite( best ) ? best : null;
}
/*
================
selectionDecalGeometry
================
*/
// CIODecal has yaw zero and size twelve. This is the corresponding stored
// LookAtLH * OrthoLH projection, preserving the native float32 cancellation.
export function selectionDecalGeometry(
	cells: ReadonlyMap<string, TerrainRange>,
	point: readonly [number, number, number]
): Geometry | null {
	const f = Math.fround,
		[x, y, z] = point.map( f ),
		center = cells.get( Math.floor( x! / 320 ) + ":" + Math.floor( z! / 320 ) );
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
