/*
===========================================================================

pose-math.ts - 4x4 column-major matrix kernels for poses

Products keep the original left-to-right double accumulation (with its
leading +0) and round to float32 only at the store, so results match the
native float path bit for bit.

===========================================================================
*/
import { hypot4 } from "@/engine/foundation/math/hypot";

/*
================
multiply

out = a * b. Reads a and b as it writes, so out must not alias them.
================
*/
export function multiply( a: ArrayLike<number>, b: ArrayLike<number>, out: Float32Array, offset = 0 ) {
	for ( let c = 0; c < 4; c++ ) {
		for ( let r = 0; r < 4; r++ ) {
			// Preserve the original left-to-right double accumulation and final
			// float32 store, including the leading +0 and alias read order.
			out[offset + c * 4 + r] =
				((0 + a[r]! * b[c * 4]! + a[4 + r]! * b[c * 4 + 1]!) + a[8 + r]! * b[c * 4 + 2]!) +
				a[12 + r]! * b[c * 4 + 3]!;
		}
	}
}

/*
================
multiplyDisjoint

multiply for owned storage: a is read once into locals, so pose owners
can call it without per-element alias checks.
================
*/
export function multiplyDisjoint( a: Float32Array, b: Float32Array, out: Float32Array, offset = 0 ) {
	const a0 = a[0]!, a1 = a[1]!, a2 = a[2]!, a3 = a[3]!, a4 = a[4]!, a5 = a[5]!, a6 = a[6]!, a7 = a[7]!;
	const a8 = a[8]!, a9 = a[9]!, a10 = a[10]!, a11 = a[11]!, a12 = a[12]!, a13 = a[13]!, a14 = a[14]!, a15 = a[15]!;
	for ( let c = 0; c < 4; c++ ) {
		const at = c * 4, b0 = b[at]!, b1 = b[at + 1]!, b2 = b[at + 2]!, b3 = b[at + 3]!, to = offset + at;
		out[to] = ((0 + a0 * b0 + a4 * b1) + a8 * b2) + a12 * b3;
		out[to + 1] = ((0 + a1 * b0 + a5 * b1) + a9 * b2) + a13 * b3;
		out[to + 2] = ((0 + a2 * b0 + a6 * b1) + a10 * b2) + a14 * b3;
		out[to + 3] = ((0 + a3 * b0 + a7 * b1) + a11 * b2) + a15 * b3;
	}
}

/*
================
compose

out = T * R(q) * S, q normalized.
================
*/
export function compose(
	t: ArrayLike<number>,
	q: ArrayLike<number>,
	s: ArrayLike<number>,
	out: Float32Array,
	offset = 0
) {
	const length = hypot4( q[0]!, q[1]!, q[2]!, q[3]! );
	if ( length < 1e-12 ) throw new Error( "Invalid pose quaternion" );
	const x = q[0]! / length, y = q[1]! / length, z = q[2]! / length, w = q[3]! / length;
	offset = Math.trunc( offset ) || 0;
	if ( offset < 0 || offset + 16 > out.length ) throw new RangeError( "Pose matrix destination is too small" );
	const sx = s[0]!, sy = s[1]!, sz = s[2]!, tx = t[0]!, ty = t[1]!, tz = t[2]!;
	out[offset] = (1 - 2 * (y * y + z * z)) * sx;
	out[offset + 1] = 2 * (x * y + z * w) * sx;
	out[offset + 2] = 2 * (x * z - y * w) * sx;
	out[offset + 3] = 0;
	out[offset + 4] = 2 * (x * y - z * w) * sy;
	out[offset + 5] = (1 - 2 * (x * x + z * z)) * sy;
	out[offset + 6] = 2 * (y * z + x * w) * sy;
	out[offset + 7] = 0;
	out[offset + 8] = 2 * (x * z + y * w) * sz;
	out[offset + 9] = 2 * (y * z - x * w) * sz;
	out[offset + 10] = (1 - 2 * (x * x + y * y)) * sz;
	out[offset + 11] = 0;
	out[offset + 12] = tx;
	out[offset + 13] = ty;
	out[offset + 14] = tz;
	out[offset + 15] = 1;
}

/*
================
slerp
================
*/
export function slerp(
	a: ArrayLike<number>,
	b: ArrayLike<number>,
	t: number,
	out: Float32Array,
	aOffset = 0,
	bOffset = 0
) {
	let dot = 0;
	for ( let i = 0; i < 4; i++ ) dot += a[aOffset + i]! * b[bOffset + i]!;
	const sign = dot < 0 ? -1 : 1;
	dot = Math.min( 1, Math.abs( dot ) );
	const angle = Math.acos( dot ),
		sin = Math.sin( angle ),
		left = sin < 1e-6 ? 1 - t : Math.sin( (1 - t) * angle ) / sin,
		right = sin < 1e-6 ? t : Math.sin( t * angle ) / sin;
	for ( let i = 0; i < 4; i++ ) out[i] = a[aOffset + i]! * left + b[bOffset + i]! * right * sign;
}

/*
================
multiplyQuaternion

out = a * b in Hamilton order (b's rotation first, then a's), x y z w. out
may alias either input.
================
*/
export function multiplyQuaternion( a: ArrayLike<number>, b: ArrayLike<number>, out: Float32Array ) {
	const ax = a[0]!, ay = a[1]!, az = a[2]!, aw = a[3]!, bx = b[0]!, by = b[1]!, bz = b[2]!, bw = b[3]!;
	out[0] = aw * bx + ax * bw + ay * bz - az * by;
	out[1] = aw * by - ax * bz + ay * bw + az * bx;
	out[2] = aw * bz + ax * by - ay * bx + az * bw;
	out[3] = aw * bw - ax * bx - ay * by - az * bz;
}
