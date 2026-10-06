/*
===========================================================================

character-shadow.ts - character shadow projection and its terrain receiver

8A3AE0 projects a fixed light onto the terrain cells under a character.
The receiver mesh is the terrain triangles inside the projected box, clipped
as the native does; the blob form is used past the shadow limit.

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
import { identity } from "./world-math";
import { terrainCellKey, type TerrainCells } from "./terrain-interaction";
import { hypot2 } from "@/engine/foundation/math/hypot";
export const SHADOW_LIMIT = 10;
export const SHADOW_DISTANCE = 3000;
export const BLOB_SHADOW_TEXTURE = "/assets/images/Map_extracted/skybox/shadowsphere.png";
/*
================
ShadowProjection
================
*/
export interface ShadowProjection {
	readonly point: readonly [number, number, number];
	readonly size: number;
	readonly matrix: Float32Array;
	readonly horizontalDepth: number;
}
/*
================
shadowProjection

8A3AE0: fixed light (1,1,0), body box height +10; LookAtLH toward
height/3 and square OrthoLH(1,10000). This is independent of time of day.
================
*/
export function shadowProjection( point: readonly [number, number, number], height: number ): ShadowProjection {
	const f = Math.fround,
		size = f( height + 10 ),
		ex = f( size * Math.SQRT1_2 ),
		ey = f( ex + size * .5 ),
		target = f( size / 3 ),
		len = hypot2( ex, ey - target ),
		zx = -ex / len,
		zy = -(ey - target) / len;
	// LookAtLH gives right +Z and up (zy,-zx,0).
	const ux = zy, uy = -zx, k = 2 / size, m = identity();
	m[0] = 0;
	m[4] = 0;
	m[8] = k;
	m[12] = -point[2] * k;
	m[1] = ux * k;
	m[5] = uy * k;
	m[9] = 0;
	m[13] = -(ux * (point[0] + ex) + uy * (point[1] + ey)) * k;
	m[2] = zx / 9999;
	m[6] = zy / 9999;
	m[10] = 0;
	m[14] = (-zx * (point[0] + ex) - zy * (point[1] + ey) - 1) / 9999;
	return { point, size, matrix: m, horizontalDepth: ex };
}
// 87EF50: native 20-unit terrain quads, alternating diagonals. Receiver
// attenuation uses the horizontal post-render view; no receiver on water.
/*
================
ShadowTerrainSurface
================
*/
export interface ShadowTerrainSurface {
	readonly positions: Float32Array;
	readonly indices: Uint32Array;
	readonly start: number;
	readonly count: number;
}
/*
================
ShadowReceiverBounds
================
*/
export interface ShadowReceiverBounds {
	readonly tx: number;
	readonly tz: number;
	readonly extent: number;
	readonly loX: number;
	readonly hiX: number;
	readonly loZ: number;
	readonly hiZ: number;
}
/*
================
shadowReceiverBounds

The native 20-unit quads a receiver covers around the shadow point, and
their world square. The terrain cells under that square are the only
terrain a receiver reads, so a cached receiver stays valid while those
cells are unchanged.
================
*/
export function shadowReceiverBounds(
	point: readonly [number, number, number],
	blobSize?: number
): ShadowReceiverBounds {
	const radius = blobSize === undefined ? 100 : blobSize,
		tx = Math.floor( point[0] / 20 ),
		tz = Math.floor( point[2] / 20 ),
		extent = Math.trunc( radius / 20 ) + (blobSize === undefined ? 0 : 1);
	return {
		tx,
		tz,
		extent,
		loX: (tx - extent) * 20,
		hiX: (tx + extent + 1) * 20,
		loZ: (tz - extent) * 20,
		hiZ: (tz + extent + 1) * 20
	};
}

/*
================
clipShadowPolygon

Return the same vertices when a bound clips nothing. Keep intersection
arithmetic and polygon order unchanged so terrain seams and fade edges match.
================
*/
function clipShadowPolygon( polygon: number[][], axis: number, bound: number, lower: boolean ): number[][] {
	let insideCount = 0;
	for ( const p of polygon ) if ( lower ? p[axis]! >= bound : p[axis]! <= bound ) insideCount++;
	if ( insideCount === polygon.length ) return polygon;
	const out: number[][] = [];
	if ( !insideCount ) return out;
	for ( let j = 0; j < polygon.length; j++ ) {
		const a = polygon[j]!, b = polygon[(j + 1) % polygon.length]!;
		const ai = lower ? a[axis]! >= bound : a[axis]! <= bound;
		const bi = lower ? b[axis]! >= bound : b[axis]! <= bound;
		if ( ai ) out.push( a );
		if ( ai !== bi ) {
			const t = (bound - a[axis]!) / (b[axis]! - a[axis]!);
			out.push( [ a[0]! + (b[0]! - a[0]!) * t, a[1]! + (b[1]! - a[1]!) * t, a[2]! + (b[2]! - a[2]!) * t ] );
		}
	}
	return out;
}

/*
================
characterShadowReceiver
================
*/
export function characterShadowReceiver(
	cells: TerrainCells,
	projection: ShadowProjection,
	blobSize?: number,
	surfaces?: ReadonlyMap<number, readonly ShadowTerrainSurface[]>
): Geometry | null {
	const { point, matrix: m } = projection,
		positions: number[] = [],
		uvs: number[] = [],
		colors: number[] = [],
		indices: number[] = [];
	const { tx, tz, extent, loX, hiX, loZ, hiZ } = shadowReceiverBounds( point, blobSize );
	/*
	================
	append
	================
	*/
	const append = ( q: readonly (readonly number[])[] ) => {
		if ( blobSize === undefined && q.every( p => p[0]! > point[0] ) ) return;
		const n = positions.length / 3;
		for ( const p of q ) {
			const px = p[0]!, py = p[1]!, pz = p[2]!;
			positions.push( px, py, pz );
			if ( blobSize !== undefined ) {
				uvs.push( (pz - point[2]) / blobSize + .5, (px - point[0]) / blobSize + .5 );
				colors.push( 0, 0, 0, 168 / 255 );
			} else {
				uvs.push(
					(m[0]! * px + m[4]! * py + m[8]! * pz + m[12]!) * .5 + .5,
					.5 - (m[1]! * px + m[5]! * py + m[9]! * pz + m[13]!) * .5
				);
				colors.push(
					0,
					0,
					0,
					Math.trunc( 150 - Math.min( 150, Math.max( 0, (-px + point[0] - 12) * 9 ) ) ) / 255
				);
			}
		}
		for ( let i = 1; i + 1 < q.length; i++ ) indices.push( n, n + i, n + i + 1 );
	};
	if ( surfaces ) {
		// Follow the triangles actually submitted for this frame, including LOD
		// interpolation and stitched edges. Raw heightfield receivers can lie below
		// those triangles even though every source sample is individually correct.
		const seen = new Set<string>();
		for ( let cz = Math.floor( loZ / 320 ); cz <= Math.floor( hiZ / 320 ); cz++ ) {
			for ( let cx = Math.floor( loX / 320 ); cx <= Math.floor( hiX / 320 ); cx++ ) {
				for ( const surface of surfaces.get( terrainCellKey( cx, cz ) ) ?? [] ) {
					for ( let i = surface.start; i < surface.start + surface.count; i += 3 ) {
						const vertices = surface.positions,
							a = surface.indices[i]! * 3,
							b = surface.indices[i + 1]! * 3,
							c = surface.indices[i + 2]! * 3;
						const ax = vertices[a]!,
							az = vertices[a + 2]!,
							bx = vertices[b]!,
							bz = vertices[b + 2]!,
							cx = vertices[c]!,
							cz = vertices[c + 2]!;
						const minX = Math.min( ax, bx, cx ),
							maxX = Math.max( ax, bx, cx ),
							minZ = Math.min( az, bz, cz ),
							maxZ = Math.max( az, bz, cz );
						// Most submitted triangles are outside this receiver. Reject them before
						// allocating vertices, closures or duplicate keys; moving actors repeat this.
						if (
							maxX < loX || minX > hiX || maxZ < loZ || minZ > hiZ ||
							blobSize === undefined && minX > point[0]
						) continue;
						const q = [ [ ax, vertices[a + 1]!, az ], [ bx, vertices[b + 1]!, bz ], [
							cx,
							vertices[c + 1]!,
							cz
						] ];
						const key = q.map( p => p.join( "," ) ).sort().join( ";" );
						if ( seen.has( key ) ) continue;
						seen.add( key );
						if ( blobSize !== undefined ) {
							append( q );
							continue;
						}
						// 87EF50 evaluates depth attenuation on 20-unit cells. Evaluating it only
						// at coarse LOD vertices loses the entire narrow fade band (or stretches
						// it across a terrain triangle). Split on the native grid while retaining
						// the submitted triangle's plane, so receivers cannot sink below terrain.
						const x0 = Math.max( tx - extent, Math.floor( minX / 20 ) ),
							x1 = Math.min( tx + extent, Math.ceil( maxX / 20 ) - 1 );
						const z0 = Math.max( tz - extent, Math.floor( minZ / 20 ) ),
							z1 = Math.min( tz + extent, Math.ceil( maxZ / 20 ) - 1 );
						for ( let z = z0; z <= z1; z++ ) {
							for ( let x = x0; x <= x1; x++ ) {
								let polygon = clipShadowPolygon( q, 0, x * 20, true );
								polygon = clipShadowPolygon( polygon, 0, (x + 1) * 20, false );
								polygon = clipShadowPolygon( polygon, 2, z * 20, true );
								polygon = clipShadowPolygon( polygon, 2, (z + 1) * 20, false );
								if ( polygon.length >= 3 ) append( polygon );
							}
						}
					}
				}
			}
		}
	} else {
		for ( let z = tz - extent; z <= tz + extent; z++ ) {
			for ( let x = tx - extent; x <= tx + extent; x++ ) {
				const cx = Math.floor( x / 16 ),
					cz = Math.floor( z / 16 ),
					cell = cells.get( terrainCellKey( cx, cz ) );
				if ( !cell ) continue;
				const ix = x - cx * 16,
					iz = z - cz * 16,
					h = ( dx: number, dz: number ) => cell.heights[(iz + dz) * 17 + ix + dx]!;
				const q = [ [ x * 20, h( 0, 0 ), z * 20 ], [ x * 20, h( 0, 1 ), (z + 1) * 20 ], [
					(x + 1) * 20,
					h( 1, 1 ),
					(z + 1) * 20
				], [ (x + 1) * 20, h( 1, 0 ), z * 20 ] ];
				if ( (x & 1) !== (z & 1) ) q.unshift( q.pop()! );
				append( q );
			}
		}
	}
	return indices.length ?
		{
			world: true,
			positions: new Float32Array( positions ),
			uvs: new Float32Array( uvs ),
			colors: new Float32Array( colors ),
			indices: new Uint32Array( indices ),
			transform: identity(),
			instances: identity(),
			material: {
				alphaCutoff: 0,
				color: [ 1, 1, 1, 1 ],
				blend: true,
				unlit: true,
				doubleSided: true,
				depthWrite: false
			}
		} :
		null;
}
