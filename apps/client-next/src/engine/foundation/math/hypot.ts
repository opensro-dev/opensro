/*
===========================================================================

hypot.ts - vector lengths without Math.hypot's allocation

Math.hypot is variadic: V8 gathers its arguments into a fresh array on
every call, so a length taken per particle or per object per frame is an
allocation per frame. These functions compute V8's own algorithm (math.tq
MathHypot: scale by the largest magnitude, Kahan-sum the squares in
argument order) on plain locals, so they return Math.hypot's exact bits
(tests/runtime/hypot.test.mjs: random magnitudes from subnormal to
overflow, and every special value) and allocate nothing.

They stay small enough for TurboFan to inline at every call site: a call
left out of line returns its double boxed, an allocation again. Zeros,
infinities, NaN and sums that overflow (a total of the magnitudes that is
not a positive finite number) take Math.hypot itself, which is exact by
definition and rare.

===========================================================================
*/

/*
================
hypot3

Math.hypot( x, y, z ), bit for bit.
================
*/
export function hypot3( x: number, y: number, z: number ): number {
	const ax = Math.abs( x ), ay = Math.abs( y ), az = Math.abs( z ), total = ax + ay + az;
	if ( !(total > 0 && total < Infinity) ) return Math.hypot( x, y, z );
	const max = ax > ay ? (ax > az ? ax : az) : (ay > az ? ay : az);
	const a = ax / max, b = ay / max, c = az / max;
	// Kahan summation, term by term as V8 orders it; the first term carries
	// no compensation.
	const ab = a * a + b * b, compensation = ab - a * a - b * b;
	return Math.sqrt( ab + (c * c - compensation) ) * max;
}

/*
================
hypot2

Math.hypot( x, y ), bit for bit.
================
*/
export function hypot2( x: number, y: number ): number {
	const ax = Math.abs( x ), ay = Math.abs( y ), total = ax + ay;
	if ( !(total > 0 && total < Infinity) ) return Math.hypot( x, y );
	const max = ax > ay ? ax : ay, a = ax / max, b = ay / max;
	return Math.sqrt( a * a + b * b ) * max;
}

/*
================
hypot4

Math.hypot( x, y, z, w ), bit for bit (a quaternion's length).
================
*/
export function hypot4( x: number, y: number, z: number, w: number ): number {
	const ax = Math.abs( x ), ay = Math.abs( y ), az = Math.abs( z ), aw = Math.abs( w ), total = ax + ay + az + aw;
	if ( !(total > 0 && total < Infinity) ) return Math.hypot( x, y, z, w );
	const xy = ax > ay ? ax : ay, zw = az > aw ? az : aw, max = xy > zw ? xy : zw;
	const a = ax / max, b = ay / max, c = az / max, d = aw / max;
	const ab = a * a + b * b, k2 = ab - a * a - b * b;
	const s3 = c * c - k2, abc = ab + s3, k3 = abc - ab - s3;
	return Math.sqrt( abc + (d * d - k3) ) * max;
}
