/*
===========================================================================

world-math.ts - world placement, camera and frustum math

Placement matrices for characters and map objects, the native left-handed
camera with WebGPU 0..1 depth, and the frustum tests the renderer culls
with. Frustum planes are kept in double precision so every test sees the
exact coefficients the native float path rounds from.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";
import type { Radians } from "@/engine/foundation/math/angles";
/*
================
identity
================
*/
export function identity() {
	return new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
}
/*
================
placement

Character pose yaw uses the presentation convention; map records use the
opposite rotation (mapPlacement). Both retain native region-local distance
units.
================
*/
export function placement( region: number, origin: number, x: number, y: number, z: number, yaw: Radians ) {
	const c = Math.cos( yaw ), s = Math.sin( yaw );
	return new Float32Array( [
		c,
		0,
		-s,
		0,
		0,
		1,
		0,
		0,
		s,
		0,
		c,
		0,
		x + ((region & 255) - (origin & 255)) * 1920,
		y,
		z + ((region >>> 8) - (origin >>> 8)) * 1920,
		1
	] );
}
/*
================
mapPlacement

MapLoader 0x441484 -> 0x451a10: m[2]=sin(yaw), m[8]=-sin(yaw). This maps
+X toward +Z for positive map yaw. Do not reuse character yaw here.
================
*/
export function mapPlacement( region: number, origin: number, x: number, y: number, z: number, yaw: Radians ) {
	const matrix = placement( region, origin, x, y, z, yaw );
	matrix[2] = Math.sin( yaw );
	matrix[8] = -Math.sin( yaw );
	return matrix;
}
/*
================
terrainLod

Terrain LOD by squared distance in cells from the eye cell.
================
*/
export function terrainLod( distanceSquared: number ) {
	return distanceSquared <= 16 ? 0 : distanceSquared <= 39 ? 1 : distanceSquared <= 81 ? 2 : 3;
}
/*
================
cameraBasis
================
*/
export function cameraBasis( camera: WorldCamera ) {
	const forward = camera.target.map( ( v, i ) => v - camera.eye[i]! );
	const length = Math.hypot( ...forward );
	if ( !Number.isFinite( length ) || length < 1e-8 ) throw new Error( "Invalid world camera" );
	const [zx, zy, zz] = forward.map( v => v / length ) as [number, number, number];
	const [ux, uy, uz] = camera.up ?? [ 0, 1, 0 ];
	let xx = uy * zz - uz * zy, xy = uz * zx - ux * zz, xz = ux * zy - uy * zx;
	const xl = Math.hypot( xx, xy, xz );
	if ( !Number.isFinite( xl ) || xl < 1e-8 ) throw new Error( "Camera up vector is parallel to view" );
	xx /= xl;
	xy /= xl;
	xz /= xl;
	return {
		forward: [ zx, zy, zz ] as const,
		right: [ xx, xy, xz ] as const,
		up: [ zy * xz - zz * xy, zz * xx - zx * xz, zx * xy - zy * xx ] as const
	};
}
/*
================
viewProjection
================
*/
export function viewProjection( camera: WorldCamera, aspect: number ) {
	if (
		!Number.isFinite( aspect ) || aspect <= 0 || camera.near <= 0 || camera.far <= camera.near || camera.fov <= 0 ||
		camera.fov >= Math.PI
	) throw new Error( "Invalid world camera" );
	// Native left-handed camera and WebGPU 0..1 depth share this basis with sky.
	const axes = cameraBasis( camera ),
		[ex, ey, ez] = camera.eye,
		[zx, zy, zz] = axes.forward,
		[xx, xy, xz] = axes.right,
		[yx, yy, yz] = axes.up;
	const view = new Float32Array( [
		xx,
		yx,
		zx,
		0,
		xy,
		yy,
		zy,
		0,
		xz,
		yz,
		zz,
		0,
		-xx * ex - xy * ey - xz * ez,
		-yx * ex - yy * ey - yz * ez,
		-zx * ex - zy * ey - zz * ez,
		1
	] );
	const f = 1 / Math.tan( camera.fov / 2 ), a = camera.far / (camera.far - camera.near), b = -camera.near * a;
	const projection = new Float32Array( [ f / aspect, 0, 0, 0, 0, f, 0, 0, 0, 0, a, 1, 0, 0, b, 0 ] ),
		out = new Float32Array( 16 );
	for ( let col = 0; col < 4; col++ ) {
		for ( let row = 0; row < 4; row++ ) {
			let sum = 0;
			for ( let k = 0; k < 4; k++ ) sum += projection[k * 4 + row]! * view[col * 4 + k]!;
			out[col * 4 + row] = sum;
		}
	}
	return out;
}
/*
================
visibleSphere

A sphere against the clip planes of a view-projection matrix.
================
*/
export function visibleSphere( m: Float32Array, c: readonly number[], radius: number ) {
	const x = c[0]!, y = c[1]!, z = c[2]!;
	// WebGPU clip space: -w<=x,y<=w, 0<=z<=w.
	for ( let plane = 0; plane < 6; plane++ ) {
		const axis = plane < 2 ? 0 : plane < 4 ? 1 : 2, sign = plane % 2 === 0 ? 1 : -1;
		const zero = plane === 4;
		const a = (zero ? 0 : m[3]!) + sign * m[axis]!,
			b = (zero ? 0 : m[7]!) + sign * m[4 + axis]!,
			d = (zero ? 0 : m[11]!) + sign * m[8 + axis]!,
			e = (zero ? 0 : m[15]!) + sign * m[12 + axis]!;
		if ( a * x + b * y + d * z + e < -radius * Math.hypot( a, b, d ) ) return false;
	}
	return true;
}

/*
================
prepareViewFrustum

The six clip planes (a, b, d, e, |abd|) of a view-projection matrix.
Double precision preserves the exact coefficients and Math.hypot result
used by visibleSphere. Float32 storage here would move clip boundaries.
================
*/
export function prepareViewFrustum( m: Float32Array ): Float64Array {
	const planes = new Float64Array( 30 );
	for ( let plane = 0; plane < 6; plane++ ) {
		const axis = plane < 2 ? 0 : plane < 4 ? 1 : 2,
			sign = plane % 2 === 0 ? 1 : -1,
			zero = plane === 4,
			offset = plane * 5;
		const a = (zero ? 0 : m[3]!) + sign * m[axis]!,
			b = (zero ? 0 : m[7]!) + sign * m[4 + axis]!,
			d = (zero ? 0 : m[11]!) + sign * m[8 + axis]!,
			e = (zero ? 0 : m[15]!) + sign * m[12 + axis]!;
		planes[offset] = a;
		planes[offset + 1] = b;
		planes[offset + 2] = d;
		planes[offset + 3] = e;
		planes[offset + 4] = Math.hypot( a, b, d );
	}
	return planes;
}

/*
================
visibleFrustumSphere
================
*/
export function visibleFrustumSphere( planes: Float64Array, x: number, y: number, z: number, radius: number ): boolean {
	for ( let i = 0; i < 30; i += 5 ) {
		if ( planes[i]! * x + planes[i + 1]! * y + planes[i + 2]! * z + planes[i + 3]! < -radius * planes[i + 4]! ) {
			return false;
		}
	}
	return true;
}

/*
================
visibleFrustumBox

Tests the support corner of the local box b against each transformed
plane. Equivalent to checking all eight world-space corners, without
allocating or rebuilding a larger world AABB when a placement rotates.
================
*/
export function visibleFrustumBox(
	planes: Float64Array,
	b: ArrayLike<number>,
	m: Float32Array,
	offset = 0
): boolean {
	for ( let i = 0; i < 30; i += 5 ) {
		const a = planes[i]!, c = planes[i + 1]!, d = planes[i + 2]!;
		const x = a * m[offset]! + c * m[offset + 1]! + d * m[offset + 2]!,
			y = a * m[offset + 4]! + c * m[offset + 5]! + d * m[offset + 6]!,
			z = a * m[offset + 8]! + c * m[offset + 9]! + d * m[offset + 10]!;
		const tx = a * m[offset + 12]!, ty = c * m[offset + 13]!, tz = d * m[offset + 14]!, w = planes[i + 3]!;
		const px = x * b[x >= 0 ? 3 : 0]!, py = y * b[y >= 0 ? 4 : 1]!, pz = z * b[z >= 0 ? 5 : 2]!;
		const distance = px + py + pz + tx + ty + tz + w;
		// A nonnegative support distance already passes any nonnegative margin.
		// Keep the exact rounding guard for rejected-side boundary candidates only.
		if ( distance >= 0 ) continue;
		// Preserve boundary geometry despite float32 shader transform rounding.
		const magnitude =
			(Math.abs( a * m[offset]! ) + Math.abs( c * m[offset + 1]! ) + Math.abs( d * m[offset + 2]! )) *
				Math.max( Math.abs( b[0]! ), Math.abs( b[3]! ) ) +
			(Math.abs( a * m[offset + 4]! ) + Math.abs( c * m[offset + 5]! ) + Math.abs( d * m[offset + 6]! )) *
				Math.max( Math.abs( b[1]! ), Math.abs( b[4]! ) ) +
			(Math.abs( a * m[offset + 8]! ) + Math.abs( c * m[offset + 9]! ) + Math.abs( d * m[offset + 10]! )) *
				Math.max( Math.abs( b[2]! ), Math.abs( b[5]! ) );
		const margin = 16 * 2 ** -23 *
			(Math.abs( tx ) + Math.abs( ty ) + Math.abs( tz ) + Math.abs( w ) + magnitude + 1);
		if ( distance < -margin ) return false;
	}
	return true;
}

/*
================
visibleFrustumAabb

Retail terrain/character frustum slot +0x14 (A2D1B0) tests the eight
corners of an axis-aligned box. The support corner gives the same plane
rejection without constructing corners; retain float32 boundary headroom.
b holds min xyz, max xyz at at.
================
*/
export function visibleFrustumAabb( planes: Float64Array, b: ArrayLike<number>, at = 0 ): boolean {
	for ( let i = 0; i < 30; i += 5 ) {
		const a = planes[i]!, c = planes[i + 1]!, d = planes[i + 2]!, w = planes[i + 3]!;
		const x = a * b[at + (a >= 0 ? 3 : 0)]!, y = c * b[at + (c >= 0 ? 4 : 1)]!, z = d * b[at + (d >= 0 ? 5 : 2)]!;
		const margin = 16 * 2 ** -23 * (Math.abs( x ) + Math.abs( y ) + Math.abs( z ) + Math.abs( w ) + 1);
		if ( x + y + z + w < -margin ) return false;
	}
	return true;
}
