/*
===========================================================================

footprints.ts - footprint decals on the terrain

87CD10 -> 882010: each footprint is the terrain triangles under an oriented
2 x 3.5 box, clipped to it and textured by surface (sand or snow).

===========================================================================
*/
import type { TerrainRange } from "@/engine/contracts/scene";
import type { Geometry } from "@/engine/contracts/geometry";
import { identity } from "./world-math";
import { terrainCellKey, type TerrainCells } from "./terrain-interaction";
/*
================
footprintTextures
================
*/
export function footprintTextures() {
	return [
		"/assets/images/Media_extracted/effect/footstep_sand.png",
		"/assets/images/Media_extracted/effect/footstep_snow.png"
	] as const;
}
type Vertex = readonly [number, number, number];
/** 87CD10 -> 882010: terrain triangles clipped against an oriented 2 x 3.5
 * box with vertical half-extent 50. Do not project onto water or a body origin. */
export function footprintGeometry( cells: TerrainCells, point: Vertex, yaw: number, right: boolean ): Geometry | null {
	const f = Math.fround,
		c = f( Math.cos( yaw ) ),
		s = f( Math.sin( yaw ) ),
		positions: number[] = [],
		uvs: number[] = [],
		indices: number[] = [];
	const local = (
		p: Vertex
	): Vertex => [
		f( f( p[0] - point[0] ) * c + f( p[2] - point[2] ) * s ),
		f( p[1] - point[1] ),
		f( f( p[0] - point[0] ) * s - f( p[2] - point[2] ) * c )
	];
	const planes: readonly (readonly [number, number, number])[] = [
		[ 0, 1, 1 ],
		[ 0, -1, 1 ],
		[ 2, 1, 1.75 ],
		[ 2, -1, 1.75 ],
		[ 1, 1, 50 ],
		[ 1, -1, 50 ]
	];
	/*
	================
	append
	================
	*/
	function append( input: readonly [Vertex, Vertex, Vertex] ) {
		const [a, b, d] = input,
			ux = b[0] - a[0],
			uy = b[1] - a[1],
			uz = b[2] - a[2],
			vx = d[0] - a[0],
			vy = d[1] - a[1],
			vz = d[2] - a[2];
		const nx = f( uy * vz - uz * vy ), ny = f( uz * vx - ux * vz ), nz = f( ux * vy - uy * vx );
		if ( !(ny > f( Math.sqrt( f( nx * nx + ny * ny + nz * nz ) ) * .25 )) ) return;
		let polygon = [ ...input ];
		for ( const [axis, sign, extent] of planes ) {
			const output: Vertex[] = [];
			for ( let i = 0; i < polygon.length; i++ ) {
				const a = polygon[i]!,
					b = polygon[(i + 1) % polygon.length]!,
					da = extent - sign * local( a )[axis]!,
					db = extent - sign * local( b )[axis]!;
				if ( da >= 0 ) output.push( a );
				if ( (da >= 0) !== (db >= 0) ) {
					const t = da / (da - db);
					output.push( [
						f( a[0] + (b[0] - a[0]) * t ),
						f( a[1] + (b[1] - a[1]) * t ),
						f( a[2] + (b[2] - a[2]) * t )
					] );
				}
			}
			polygon = output;
			if ( polygon.length < 3 ) return;
		}
		const n = positions.length / 3;
		if ( n + polygon.length >= 256 ) return;
		for ( const p of polygon ) {
			const v = local( p ), u = f( v[0] / 2 + .5 );
			positions.push( ...p );
			uvs.push( right ? f( 1 - u ) : u, f( v[2] / 3.5 + .5 ) );
		}
		for ( let i = 1; i + 1 < polygon.length; i++ ) indices.push( n, n + i, n + i + 1 );
	}
	const tx = Math.floor( point[0] / 20 ), tz = Math.floor( point[2] / 20 );
	for ( let z = tz - 1; z <= tz + 1; z++ ) {
		for ( let x = tx - 1; x <= tx + 1; x++ ) {
			const cx = Math.floor( x / 16 ), cz = Math.floor( z / 16 ), cell = cells.get( terrainCellKey( cx, cz ) );
			if ( !cell ) continue;
			const ix = x - cx * 16,
				iz = z - cz * 16,
				h = ( dx: number, dz: number ) => cell.heights[(iz + dz) * 17 + ix + dx]!;
			const q: Vertex[] = [ [ x * 20, h( 0, 0 ), z * 20 ], [ x * 20, h( 0, 1 ), (z + 1) * 20 ], [
				(x + 1) * 20,
				h( 1, 1 ),
				(z + 1) * 20
			], [ (x + 1) * 20, h( 1, 0 ), z * 20 ] ];
			if ( (x & 1) !== (z & 1) ) q.unshift( q.pop()! );
			append( [ q[0]!, q[1]!, q[2]! ] );
			append( [ q[0]!, q[2]!, q[3]! ] );
		}
	}
	return indices.length ?
		{
			world: true,
			positions: new Float32Array( positions ),
			uvs: new Float32Array( uvs ),
			indices: new Uint32Array( indices ),
			transform: identity(),
			instances: identity(),
			material: {
				color: [ 1, 1, 1, 1 ],
				alphaCutoff: 0,
				blend: true,
				doubleSided: false,
				unlit: true,
				groundDecal: true
			}
		} :
		null;
}
