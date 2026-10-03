/*
===========================================================================

particle-presentation.ts - draw 20 Hz particle ticks smoothly at any frame rate

The native effect clock runs whole 50 ms ticks (CCompoundModelManager_
AdvanceFrameTime A2E980 carries the remainder), and the native renderer
draws the last tick as is. At a 144-240 Hz display that reads as 20 FPS
effects. Deliberate deviation (approved 2026-10-03): the simulation keeps
its exact ticks, and presentation alone carries each particle forward by
the fraction of the next tick, continuing the motion of the last tick.

Only presentation reads these values. No random draw, command, birth or
retirement happens here, so tick results stay bit-identical to native.

===========================================================================
*/

import type { ParticleInstance } from "@/engine/foundation/animation/particle-program";

// A2E980: whole 50 ms effect ticks.
export const PARTICLE_TICKS_PER_SECOND = 20;

// A rotation step beyond this between two ticks is a reset (a command
// replaced the rotation), not motion; it is drawn as is.
const MAX_TICK_ROTATION_RAD = 1.5707963267948966; // pi / 2
const MIN_DETERMINANT = 1e-12;

export interface ParticleTickPose {
	readonly position: number[];
	readonly scale: number[];
	readonly rotation: Float32Array;
	readonly matrix: Float32Array;
	// The last tick step of rotation and of matrix (continueRotation's cache).
	rotationStep?: Float64Array;
	matrixStep?: Float64Array;
}

// A step cache: the previous and current 3x3 it was found for (0..17), its
// state (18: 0 empty, 1 a rotation, 2 none), and axis xyz and angle (19..22).
const STEP_CACHE = 23;
const STEP_STATE = 18;
const STEP_EMPTY = 0;
const STEP_ROTATION = 1;
const STEP_NONE = 2;

/*
================
createStepCache
================
*/
export function createStepCache(): Float64Array {
	return new Float64Array( STEP_CACHE );
}

/*
================
snapshotParticleTick

Record the state a particle had at the start of its latest tick. The
optional matrix is the owning graph element's frame.
================
*/
export function snapshotParticleTick( state: ParticleInstance, matrix?: Float32Array ): void {
	const previous = state.previous ?? {
		position: [ 0, 0, 0 ],
		scale: [ 1, 1, 1 ],
		rotation: new Float32Array( 16 ),
		matrix: new Float32Array( 16 )
	};
	for ( let axis = 0; axis < 3; axis++ ) {
		previous.position[axis] = state.position[axis]!;
		previous.scale[axis] = state.scale[axis]!;
	}
	previous.rotation.set( state.rotation );
	if ( matrix ) previous.matrix.set( matrix );
	state.previous = previous;
}

/*
================
PresentedParticle

The caller's scratch for one drawn particle. It owns every array it
holds, so writing it never touches a live particle, and work is the
rotation math's scratch: nothing here allocates per particle per frame.
================
*/
export interface PresentedParticle extends ParticleInstance {
	readonly work: Float64Array;
}

// invert3 9, step 9, partial 9.
const ROTATION_WORK = 27;

export function createPresentedParticle(): PresentedParticle {
	return {
		position: [ 0, 0, 0 ],
		velocity: [ 0, 0, 0 ],
		scale: [ 1, 1, 1 ],
		rotation: new Float32Array( 16 ),
		frame: 0,
		work: new Float64Array( ROTATION_WORK )
	};
}

/*
================
presentParticle

Fill out with the particle carried fraction (0..1) of a tick past its
latest tick. Position and scale continue linearly; rotation continues by
the same fraction of its last tick rotation.
================
*/
export function presentParticle(
	state: ParticleInstance,
	fraction: number,
	out: PresentedParticle
): PresentedParticle {
	const previous = state.previous, blend = Math.max( 0, Math.min( 1, fraction ) );
	out.frame = state.frame;
	out.velocity = state.velocity;
	if ( !previous || blend === 0 ) {
		for ( let axis = 0; axis < 3; axis++ ) {
			out.position[axis] = state.position[axis]!;
			out.scale[axis] = state.scale[axis]!;
		}
		out.rotation.set( state.rotation );
		return out;
	}
	for ( let axis = 0; axis < 3; axis++ ) {
		const position = state.position[axis]!, scale = state.scale[axis]!;
		out.position[axis] = position + (position - previous.position[axis]!) * blend;
		out.scale[axis] = scale + (scale - previous.scale[axis]!) * blend;
	}
	continueRotation(
		previous.rotation,
		state.rotation,
		blend,
		out.rotation,
		0,
		out.work,
		previous.rotationStep ??= createStepCache()
	);
	return out;
}

/*
================
continueRotation

Write current * step^fraction into out at offset, where step is the
rotation taken from previous to current (previous^-1 * current). The
translation row and any scale of current are kept. A step that is not a
rotation, or is too large to be motion, leaves current as is. work is
caller scratch of at least ROTATION_WORK entries.

Most particles do not rotate between ticks; they take the copy path. The
step changes only at a tick, so with a cache (createStepCache, one per
tick pose) its axis and angle are found once per tick and each frame only
turns by its fraction; the cache answers only for the exact inputs it was
found for, so the result is unchanged.
================
*/
export function continueRotation(
	previous: ArrayLike<number>,
	current: ArrayLike<number>,
	fraction: number,
	out: Float32Array,
	offset: number,
	work: Float64Array = new Float64Array( ROTATION_WORK ),
	cache?: Float64Array
): void {
	for ( let i = 0; i < 16; i++ ) out[offset + i] = current[i]!;
	if ( fraction <= 0 ) return;
	if (
		previous[0] === current[0] && previous[1] === current[1] && previous[2] === current[2] &&
		previous[4] === current[4] && previous[5] === current[5] && previous[6] === current[6] &&
		previous[8] === current[8] && previous[9] === current[9] && previous[10] === current[10]
	) return;
	let x: number, y: number, z: number, angle: number;
	if ( cache && cache[STEP_STATE] !== STEP_EMPTY && stepInputsMatch( cache, previous, current ) ) {
		if ( cache[STEP_STATE] === STEP_NONE ) return;
		x = cache[19]!;
		y = cache[20]!;
		z = cache[21]!;
		angle = cache[22]!;
	} else {
		const found = invert3( previous, work ) && stepRotation( current, work );
		if ( cache ) {
			for ( let i = 0; i < 9; i++ ) {
				const at = (i / 3 | 0) * 4 + i % 3;
				cache[i] = previous[at]!;
				cache[9 + i] = current[at]!;
			}
			cache[STEP_STATE] = found ? STEP_ROTATION : STEP_NONE;
			for ( let i = 0; i < 4; i++ ) cache[19 + i] = work[18 + i]!;
		}
		if ( !found ) return;
		x = work[18]!;
		y = work[19]!;
		z = work[20]!;
		angle = work[21]!;
	}
	partialRotation( x, y, z, angle, fraction, work );
	// out = current * partial (3x3 part only); partial is at work[18].
	for ( let column = 0; column < 3; column++ ) {
		for ( let row = 0; row < 3; row++ ) {
			out[offset + column * 4 + row] = current[row]! * work[18 + column * 3]! +
				current[4 + row]! * work[18 + column * 3 + 1]! +
				current[8 + row]! * work[18 + column * 3 + 2]!;
		}
	}
}

/*
================
invert3

Inverse of the upper 3x3 of a column-major 4x4, written column-major into
work[0..8]. False when it is singular.
================
*/
function invert3( m: ArrayLike<number>, work: Float64Array ): boolean {
	const a = m[0]!, b = m[4]!, c = m[8]!;
	const d = m[1]!, e = m[5]!, f = m[9]!;
	const g = m[2]!, h = m[6]!, i = m[10]!;
	const det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g);
	if ( !Number.isFinite( det ) || Math.abs( det ) < MIN_DETERMINANT ) return false;
	work[0] = (e * i - f * h) / det;
	work[1] = (f * g - d * i) / det;
	work[2] = (d * h - e * g) / det;
	work[3] = (c * h - b * i) / det;
	work[4] = (a * i - c * g) / det;
	work[5] = (b * g - a * h) / det;
	work[6] = (b * f - c * e) / det;
	work[7] = (c * d - a * f) / det;
	work[8] = (a * e - b * d) / det;
	return true;
}

/*
================
stepInputsMatch

True when cache was found for these exact previous and current 3x3.
================
*/
function stepInputsMatch( cache: Float64Array, previous: ArrayLike<number>, current: ArrayLike<number> ): boolean {
	for ( let i = 0; i < 9; i++ ) {
		const at = (i / 3 | 0) * 4 + i % 3;
		if ( cache[i] !== previous[at] || cache[9 + i] !== current[at] ) return false;
	}
	return true;
}

/*
================
stepRotation

With previous^-1 at work[0..8], the step previous^-1 * current at
work[9..17], and its axis xyz and angle at work[18..21]. False when the
step is not a rotation (it also scaled or sheared) or exceeds
MAX_TICK_ROTATION_RAD.
================
*/
function stepRotation( current: ArrayLike<number>, work: Float64Array ): boolean {
	// step = previous^-1 * current, column-major 3x3 at work[9].
	for ( let column = 0; column < 3; column++ ) {
		for ( let row = 0; row < 3; row++ ) {
			work[9 + column * 3 + row] = work[row]! * current[column * 4]! +
				work[3 + row]! * current[column * 4 + 1]! +
				work[6 + row]! * current[column * 4 + 2]!;
		}
	}
	for ( let column = 0; column < 3; column++ ) {
		const x = work[9 + column * 3]!, y = work[10 + column * 3]!, z = work[11 + column * 3]!;
		if ( Math.abs( x * x + y * y + z * z - 1 ) > 2e-3 ) return false;
	}
	const cos = Math.max( -1, Math.min( 1, (work[9]! + work[13]! + work[17]! - 1) / 2 ) );
	const angle = Math.acos( cos );
	if ( angle > MAX_TICK_ROTATION_RAD || angle < 1e-7 ) return false;
	let x = work[14]! - work[16]!, y = work[15]! - work[11]!, z = work[10]! - work[12]!;
	const length = Math.sqrt( x * x + y * y + z * z );
	if ( length < 1e-9 ) return false;
	x /= length;
	y /= length;
	z /= length;
	work[18] = x;
	work[19] = y;
	work[20] = z;
	work[21] = angle;
	return true;
}

/*
================
partialRotation

The rotation by fraction of angle about the unit axis xyz, written
column-major into work[18..26].
================
*/
function partialRotation( x: number, y: number, z: number, angle: number, fraction: number, work: Float64Array ) {
	const turn = angle * fraction, c = Math.cos( turn ), s = Math.sin( turn ), t = 1 - c;
	work[18] = t * x * x + c;
	work[19] = t * x * y + s * z;
	work[20] = t * x * z - s * y;
	work[21] = t * x * y - s * z;
	work[22] = t * y * y + c;
	work[23] = t * y * z + s * x;
	work[24] = t * x * z + s * y;
	work[25] = t * y * z - s * x;
	work[26] = t * z * z + c;
}
