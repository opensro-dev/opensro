/*
===========================================================================

mat4.mjs - column-major 4x4 matrix helpers for the character pipeline

One owner for the small matrix math the character build shares: the GLB
exporter and the clip grounding both compose parent/child transforms
from authored quaternions, and they must agree bit for bit (the
grounding ruler measures poses in the same space the exporter
publishes). Column-major Float64Array(16), matching the glTF layout.

===========================================================================
*/

/*
================
quatToMat4

Convert an authored quaternion and translation into a column-major
matrix. Shared by the bind pose, the clip FK and the grounding ruler.
================
*/
export function quatToMat4( q, t ) {
	const [x, y, z, w] = q;
	const [tx, ty, tz] = t;
	const x2 = x + x, y2 = y + y, z2 = z + z;
	const xx = x * x2, xy = x * y2, xz = x * z2;
	const yy = y * y2, yz = y * z2, zz = z * z2;
	const wx = w * x2, wy = w * y2, wz = w * z2;
	// column-major
	return new Float64Array( [
		1 - (yy + zz),
		xy + wz,
		xz - wy,
		0,
		xy - wz,
		1 - (xx + zz),
		yz + wx,
		0,
		xz + wy,
		yz - wx,
		1 - (xx + yy),
		0,
		tx,
		ty,
		tz,
		1
	] );
}

/*
================
mulMat4

Compose parent and child transforms in column-major order.
================
*/
export function mulMat4( a, b ) {
	const o = new Float64Array( 16 );
	for ( let c = 0; c < 4; c += 1 ) {
		for ( let r = 0; r < 4; r += 1 ) {
			o[c * 4 + r] = a[r] * b[c * 4] + a[4 + r] * b[c * 4 + 1] + a[8 + r] * b[c * 4 + 2] +
				a[12 + r] * b[c * 4 + 3];
		}
	}
	return o;
}
