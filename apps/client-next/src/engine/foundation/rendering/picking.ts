/*
===========================================================================

picking.ts - segment tests against meshes

The intersection kernel for picking, occlusion and camera collision:
skinned or static, with optional alpha and winding rules (PickSurface) or
bare block culling (pickGeometryBlocks).

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
type Point = readonly number[];
export type PickRay = { readonly start: Point; readonly delta: Point; };
export interface PickAlpha {
	readonly width: number;
	readonly height: number;
	readonly pixels: Uint8Array;
}
export interface PickSurface {
	readonly alpha?: PickAlpha;
	readonly opacity?: number;
	readonly blocks?: Float64Array;
	readonly ranges?: readonly {
		readonly indexCount: number;
		readonly center: readonly [number, number, number];
		readonly radius: number;
	}[];
}
export type PickBounds = readonly [number, number, number, number, number, number];
export const PICK_BLOCK_INDICES = 96;
// Only for immutable, unskinned geometry. Index order and exact surface tests
// remain unchanged; a rejected block cannot contain a segment intersection.
export function geometryPickBlocks( geometry: Geometry ): Float64Array {
	const blocks = new Float64Array( Math.ceil( geometry.indices.length / PICK_BLOCK_INDICES ) * 6 );
	for ( let start = 0; start < geometry.indices.length; start += PICK_BLOCK_INDICES ) {
		let x = Infinity, y = Infinity, z = Infinity, X = -Infinity, Y = -Infinity, Z = -Infinity;
		for ( let i = start; i < Math.min( start + PICK_BLOCK_INDICES, geometry.indices.length ); i++ ) {
			const at = geometry.indices[i]! * 3,
				a = geometry.positions[at]!,
				b = geometry.positions[at + 1]!,
				c = geometry.positions[at + 2]!;
			x = Math.min( x, a );
			y = Math.min( y, b );
			z = Math.min( z, c );
			X = Math.max( X, a );
			Y = Math.max( Y, b );
			Z = Math.max( Z, c );
		}
		const at = start / PICK_BLOCK_INDICES * 6;
		blocks[at] = x;
		blocks[at + 1] = y;
		blocks[at + 2] = z;
		blocks[at + 3] = X;
		blocks[at + 4] = Y;
		blocks[at + 5] = Z;
	}
	return blocks;
}
export function geometryPickBounds( positions: Float32Array ): PickBounds {
	const b = [ Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity ];
	for ( let i = 0; i < positions.length; i++ ) {
		const axis = i % 3;
		b[axis] = Math.min( b[axis]!, positions[i]! );
		b[axis + 3] = Math.max( b[axis + 3]!, positions[i]! );
	}
	return [ b[0]!, b[1]!, b[2]!, b[3]!, b[4]!, b[5]! ];
}
// Normalized nonnegative skin weights form a convex combination of bone
// transforms. Their union therefore encloses every skinned vertex.
export function palettePickBounds( b: PickBounds, palette: Float32Array ): PickBounds {
	const result = [ Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity ];
	for ( let offset = 0; offset < palette.length; offset += 16 ) {
		for ( let axis = 0; axis < 3; axis++ ) {
			let center = palette[offset + 12 + axis]!, extent = 0;
			for ( let k = 0; k < 3; k++ ) {
				center += palette[offset + k * 4 + axis]! * (b[k]! + b[k + 3]!) * .5;
				extent += Math.abs( palette[offset + k * 4 + axis]! ) * (b[k + 3]! - b[k]!) * .5;
			}
			result[axis] = Math.min( result[axis]!, center - extent );
			result[axis + 3] = Math.max( result[axis + 3]!, center + extent );
		}
	}
	return [ result[0]!, result[1]!, result[2]!, result[3]!, result[4]!, result[5]! ];
}
// Conservative transformed AABB. The exact triangle/alpha test remains the oracle.
export function rayIntersectsBounds(
	ray: PickRay,
	b: PickBounds | Float64Array,
	m: Float32Array,
	limit = 1,
	offset = 0
): boolean {
	let enter = 0, exit = limit;
	for ( let axis = 0; axis < 3; axis++ ) {
		let center = m[12 + axis]!, extent = 0;
		for ( let k = 0; k < 3; k++ ) {
			center += m[k * 4 + axis]! * (b[offset + k]! + b[offset + k + 3]!) * .5;
			extent += Math.abs( m[k * 4 + axis]! ) * (b[offset + k + 3]! - b[offset + k]!) * .5;
		}
		const delta = ray.delta[axis]!,
			start = ray.start[axis]!,
			low = center - extent - 1e-5,
			high = center + extent + 1e-5;
		if ( Math.abs( delta ) < 1e-15 ) {
			if ( start < low || start > high ) return false;
			continue;
		}
		const a = (low - start) / delta, c = (high - start) / delta;
		enter = Math.max( enter, Math.min( a, c ) );
		exit = Math.min( exit, Math.max( a, c ) );
		if ( enter > exit ) return false;
	}
	return true;
}
export function samplePickAlpha( mask: PickAlpha, u: number, v: number ): number {
	const x = u * mask.width - .5,
		y = v * mask.height - .5,
		x0 = Math.floor( x ),
		y0 = Math.floor( y ),
		fx = x - x0,
		fy = y - y0;
	const read = ( x: number, y: number ) =>
		mask.pixels[
			((y % mask.height + mask.height) % mask.height) * mask.width + (x % mask.width + mask.width) % mask.width
		]! / 255;
	return (read( x0, y0 ) * (1 - fx) + read( x0 + 1, y0 ) * fx) * (1 - fy) +
		(read( x0, y0 + 1 ) * (1 - fx) + read( x0 + 1, y0 + 1 ) * fx) * fy;
}
// WebGPU clip depth is 0..1. Coordinates are normalized canvas coordinates,
// independent of backing-buffer size and device pixel ratio.
export function pickRay( matrix: Float32Array, x: number, y: number ): PickRay | null {
	if ( !Number.isFinite( x ) || !Number.isFinite( y ) || x < 0 || x > 1 || y < 0 || y > 1 ) return null;
	return pickRayProjector( matrix )?.( x, y ) ?? null;
}
// One camera inverse per query fan, not one inverse per neighboring pixel.
export function pickRayProjector( matrix: Float32Array ): (( x: number, y: number ) => PickRay | null) | null {
	const a = Array.from(
		{ length: 4 },
		( _, r ) => Array.from( { length: 8 }, ( _, c ) => c < 4 ? matrix[c * 4 + r]! : Number( c - 4 === r ) )
	);
	for ( let c = 0; c < 4; c++ ) {
		let pivot = c;
		for ( let r = c + 1; r < 4; r++ ) if ( Math.abs( a[r]![c]! ) > Math.abs( a[pivot]![c]! ) ) pivot = r;
		if ( Math.abs( a[pivot]![c]! ) < 1e-12 ) return null;
		[a[c], a[pivot]] = [ a[pivot]!, a[c]! ];
		const divisor = a[c]![c]!;
		for ( let k = 0; k < 8; k++ ) a[c]![k]! /= divisor;
		for ( let r = 0; r < 4; r++ ) {
			if ( r !== c ) {
				const factor = a[r]![c]!;
				for ( let k = 0; k < 8; k++ ) a[r]![k]! -= factor * a[c]![k]!;
			}
		}
	}
	return ( x: number, y: number ) => {
		if ( !Number.isFinite( x ) || !Number.isFinite( y ) || x < 0 || x > 1 || y < 0 || y > 1 ) return null;
		function point( z: number ) {
			const px = x * 2 - 1, py = 1 - y * 2, w = a[3]![4]! * px + a[3]![5]! * py + a[3]![6]! * z + a[3]![7]!;
			return [ 0, 1, 2 ].map( r => (a[r]![4]! * px + a[r]![5]! * py + a[r]![6]! * z + a[r]![7]!) / w );
		}
		const start = point( 0 ), end = point( 1 );
		if ( ![ ...start, ...end ].every( Number.isFinite ) ) return null;
		return { start, delta: end.map( ( n, i ) => n - start[i]! ) };
	};
}
export function geometryVertex( geometry: Geometry, instance: Float32Array, index: number, palette?: Float32Array ) {
	const out = [ 0, 0, 0 ];
	writeGeometryVertex( geometry, instance, index, palette, out, 0 );
	return out;
}
function writeGeometryVertex(
	geometry: Geometry,
	instance: Float32Array,
	index: number,
	palette: Float32Array | undefined,
	out: number[] | Float64Array,
	at: number
) {
	const x = geometry.positions[index * 3]!,
		y = geometry.positions[index * 3 + 1]!,
		z = geometry.positions[index * 3 + 2]!;
	let px = x, py = y, pz = z, pw = 1;
	if ( palette && geometry.joints && geometry.weights ) {
		px = 0;
		py = 0;
		pz = 0;
		pw = 0;
		for ( let j = 0; j < 4; j++ ) {
			const weight = geometry.weights[index * 4 + j]!;
			if ( !weight ) continue;
			const offset = geometry.joints[index * 4 + j]! * 16;
			px += weight *
				(palette[offset]! * x + palette[offset + 4]! * y + palette[offset + 8]! * z + palette[offset + 12]!);
			py += weight *
				(palette[offset + 1]! * x + palette[offset + 5]! * y + palette[offset + 9]! * z +
					palette[offset + 13]!);
			pz += weight *
				(palette[offset + 2]! * x + palette[offset + 6]! * y + palette[offset + 10]! * z +
					palette[offset + 14]!);
			pw += weight *
				(palette[offset + 3]! * x + palette[offset + 7]! * y + palette[offset + 11]! * z +
					palette[offset + 15]!);
		}
	}
	out[at] = instance[0]! * px + instance[4]! * py + instance[8]! * pz + instance[12]! * pw;
	out[at + 1] = instance[1]! * px + instance[5]! * py + instance[9]! * pz + instance[13]! * pw;
	out[at + 2] = instance[2]! * px + instance[6]! * py + instance[10]! * pz + instance[14]! * pw;
}
export function pickGeometry(
	ray: PickRay,
	geometry: Geometry,
	instance: Float32Array,
	palette?: Float32Array,
	surface?: PickSurface
): number | null {
	return intersectGeometry( ray, geometry, instance, { palette, surface } );
}
// A bare segment test (no alpha, winding or opacity rules) that skips index
// blocks whose bounds the segment misses. blocks come from geometryPickBlocks,
// so this is exact only for immutable, unskinned geometry.
export function pickGeometryBlocks(
	ray: PickRay,
	geometry: Geometry,
	instance: Float32Array,
	blocks: Float64Array
): number | null {
	return intersectGeometry( ray, geometry, instance, { blocks } );
}
// Visibility asks whether any accepted surface precedes the character. It does
// not need the nearest surface. Alpha, winding and finite-ray semantics remain
// owned by the same intersection kernel as destination picking.
export function occludesGeometry(
	ray: PickRay,
	geometry: Geometry,
	instance: Float32Array,
	limit: number,
	palette?: Float32Array,
	surface?: PickSurface
): boolean {
	if ( !Number.isFinite( limit ) || limit < 0 ) throw Error( "Invalid occlusion depth" );
	return intersectGeometry( ray, geometry, instance, { palette, surface, stopBefore: limit } ) !== null;
}
// One intersection query. surface brings alpha, winding and opacity rules
// (and its own blocks or ranges); blocks alone only culls.
type IntersectQuery = {
	readonly palette?: Float32Array;
	readonly surface?: PickSurface;
	readonly stopBefore?: number;
	readonly blocks?: Float64Array;
};
function intersectGeometry(
	ray: PickRay,
	geometry: Geometry,
	instance: Float32Array,
	query: IntersectQuery
): number | null {
	const { palette, surface, stopBefore } = query,
		blocks = query.blocks ?? (surface?.ranges ? undefined : surface?.blocks);
	// Shared triangle vertices are skinned once per query, not three times per
	// incident triangle. Query-local storage cannot outlive a changing pose.
	// An unskinned mesh transforms each corner directly into a nine-value
	// scratch: a mesh-sized cache per query cost more to allocate and clear
	// than the three matrix products it saved.
	let vertices: Float64Array | undefined, ready: Uint8Array | undefined;
	const corners = palette && geometry.joints && geometry.weights ? undefined : new Float64Array( 9 );
	const vertex = ( index: number ) => {
		if ( !vertices ) {
			vertices = new Float64Array( geometry.positions.length );
			ready = new Uint8Array( geometry.positions.length / 3 );
		}
		if ( !ready![index] ) {
			writeGeometryVertex( geometry, instance, index, palette, vertices, index * 3 );
			ready![index] = 1;
		}
		return index * 3;
	};
	let nearest = stopBefore ?? Infinity, rangeIndex = 0, rangeEnd = 0;
	const rangeBounds: [number, number, number, number, number, number] | undefined = surface?.ranges ?
		[ 0, 0, 0, 0, 0, 0 ] :
		undefined;
	for ( let i = 0; i < geometry.indices.length; i += 3 ) {
		if ( surface?.ranges && i === rangeEnd ) {
			let range;
			do {
				range = surface.ranges[rangeIndex++]!;
				rangeEnd += range.indexCount;
			} while ( rangeEnd === i );
			const b = rangeBounds!;
			for ( let axis = 0; axis < 3; axis++ ) {
				b[axis] = range.center[axis]! - range.radius;
				b[axis + 3] = range.center[axis]! + range.radius;
			}
			if ( !rayIntersectsBounds( ray, rangeBounds!, instance, Math.min( 1, nearest ) ) ) {
				i = rangeEnd - 3;
				continue;
			}
		}
		if (
			blocks && i % PICK_BLOCK_INDICES === 0 &&
			!rayIntersectsBounds( ray, blocks, instance, Math.min( 1, nearest ), i / PICK_BLOCK_INDICES * 6 )
		) {
			i += PICK_BLOCK_INDICES - 3;
			continue;
		}
		let a = 0, b = 3, c = 6, points: Float64Array;
		if ( corners ) {
			writeGeometryVertex( geometry, instance, geometry.indices[i]!, undefined, corners, 0 );
			writeGeometryVertex( geometry, instance, geometry.indices[i + 1]!, undefined, corners, 3 );
			writeGeometryVertex( geometry, instance, geometry.indices[i + 2]!, undefined, corners, 6 );
			points = corners;
		} else {
			a = vertex( geometry.indices[i]! );
			b = vertex( geometry.indices[i + 1]! );
			c = vertex( geometry.indices[i + 2]! );
			points = vertices!;
		}
		const ex = points[b]! - points[a]!, ey = points[b + 1]! - points[a + 1]!, ez = points[b + 2]! - points[a + 2]!;
		const fx = points[c]! - points[a]!, fy = points[c + 1]! - points[a + 1]!, fz = points[c + 2]! - points[a + 2]!;
		const dx = ray.delta[0]!,
			dy = ray.delta[1]!,
			dz = ray.delta[2]!,
			hx = dy * fz - dz * fy,
			hy = dz * fx - dx * fz,
			hz = dx * fy - dy * fx,
			det = ex * hx + ey * hy + ez * hz;
		if ( Math.abs( det ) < 1e-12 ) continue;
		const sx = ray.start[0]! - points[a]!,
			sy = ray.start[1]! - points[a + 1]!,
			sz = ray.start[2]! - points[a + 2]!,
			u = (sx * hx + sy * hy + sz * hz) / det;
		if ( u < 0 || u > 1 ) continue;
		const qx = sy * ez - sz * ey,
			qy = sz * ex - sx * ez,
			qz = sx * ey - sy * ex,
			v = (dx * qx + dy * qy + dz * qz) / det;
		if ( v < 0 || u + v > 1 ) continue;
		const t = (fx * qx + fy * qy + fz * qz) / det;
		if ( t < 0 || t > 1 || t >= nearest ) continue;
		if ( surface ) {
			if ( geometry.material?.doubleSided === false && det < 0 ) continue;
			const weights = [ 1 - u - v, u, v ];
			let alpha = (geometry.material?.color[3] ?? 1) * (surface.opacity ?? 1);
			if ( geometry.colors ) {
				alpha *= weights.reduce(
					( n, w, k ) => n + w * geometry.colors![geometry.indices[i + k]! * 4 + 3]!,
					0
				);
			}
			if ( surface.alpha && geometry.uvs ) {
				const uv = [ 0, 1 ].map( a =>
					weights.reduce( ( n, w, k ) => n + w * geometry.uvs![geometry.indices[i + k]! * 2 + a]!, 0 )
				);
				alpha *= samplePickAlpha( surface.alpha, uv[0]!, uv[1]! );
			}
			if ( alpha <= 0 || alpha < (geometry.material?.alphaCutoff ?? 0) ) continue;
		}
		if ( stopBefore !== undefined ) return t;
		nearest = t;
	}
	return stopBefore === undefined && Number.isFinite( nearest ) ? nearest : null;
}
