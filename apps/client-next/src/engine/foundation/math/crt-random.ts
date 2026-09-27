/*
===========================================================================

crt-random.ts - the native client's C runtime rand() and ranged draws

The v1.150 client draws presentation randomness through the MSVC C runtime
rand() and a `lower + rand() % (upper - lower)` range helper (retail 9c4776
and 878a10). Porting both exactly keeps effects, idles and curves in step
with the original for the same seed. The state is a plain value: the caller
owns which thread's stream it projects and stores the returned state.

===========================================================================
*/

// MSVC rand(): state = state * 214013 + 2531011, output = (state >> 16) & 0x7fff.
const CRT_RAND_MULTIPLIER = 0x343fd;
const CRT_RAND_INCREMENT = 0x269ec3;
const CRT_RAND_MAX = 0x7fff;

const INT32_MIN = -0x80000000;
const INT32_MAX = 0x7fffffff;
const UINT32_MAX = 0xffffffff;

/*
================
crtRandomRange

Draws one value in [lower, upper) and returns it with the advanced state.
The upper bound is exclusive. Reversed or equal bounds return lower without
consuming a draw, as the native range helper does. Bounds must fit the
native 32-bit integers, and the span must fit a positive int32.
================
*/
export function crtRandomRange( state: number, lower: number, upper: number ): { state: number; value: number; } {
	if (
		!Number.isInteger( state ) || state < 0 || state > UINT32_MAX || !Number.isInteger( lower ) ||
		!Number.isInteger( upper ) || lower < INT32_MIN || lower > INT32_MAX || upper < INT32_MIN ||
		upper > INT32_MAX ||
		upper - lower > INT32_MAX
	) {
		throw new Error( "Invalid CRT random range" );
	}
	if ( upper <= lower ) {
		return { state, value: lower };
	}
	const next = (Math.imul( state, CRT_RAND_MULTIPLIER ) + CRT_RAND_INCREMENT) >>> 0;
	return { state: next, value: lower + ((next >>> 16) & CRT_RAND_MAX) % (upper - lower) };
}
