/*
===========================================================================

particle-records.ts - the tick state an emitted particle is drawn from

Emitted particles tick at 20 Hz (particle-presentation.ts) and are drawn
at the display rate. Everything a frame needs to draw a particle that the
tick does not change, it changes only at a tick: so the CPU writes one
record per particle slot when its tick state changes, and the GPU
presentation pass (runtime/renderer/device/particle-shader.ts) turns the
records into instance matrices, palettes and appearance every frame.

A record holds a tick's values and the motion of its last tick; the pass
continues that motion by the frame's fraction of the next tick: linearly
for position and scale, by turnByStep for rotation (as
particleElementMatrix does on the CPU). Rotation motion is a step (axis
and angle, tickRotationStep), found once a tick.

The layout is the WGSL Record struct: vec3 members are followed by one
scalar, so every group fills 16 bytes.

===========================================================================
*/

import type { ParticleElement } from "@/engine/foundation/animation/particle-graph";
import type { ParticleInstance } from "@/engine/foundation/animation/particle-program";
import { PARTICLE_TICKS_PER_SECOND, tickRotationStep } from "@/engine/foundation/animation/particle-presentation";

// Floats in one slot's record.
export const PARTICLE_RECORD = 64;
// The instance matrix at the tick (graph: the element frame at its
// position; otherwise the birth matrix), its rotation step and its motion.
export const RECORD_MATRIX = 0;
export const RECORD_MATRIX_STEP = 16;
export const RECORD_MOTION = 20;
export const RECORD_FLAGS = 23;
// The particle program's own pose: position, scale and rotation with
// their last tick motion, its velocity (ViewVBillboard), its tick and its
// birth in seconds (the age the draw is timed by).
export const RECORD_POSITION = 24;
export const RECORD_TICK = 27;
export const RECORD_POSITION_MOTION = 28;
export const RECORD_BIRTH = 31;
export const RECORD_SCALE = 32;
export const RECORD_SCALE_MOTION = 36;
export const RECORD_ROTATION = 40;
export const RECORD_ROTATION_STEP = 56;
export const RECORD_VELOCITY = 60;

// RECORD_FLAGS bits. LIVE: the slot holds a particle. PRESENTED: it has a
// program pose (a plain emitted mesh has none and keeps its palette).
export const RECORD_LIVE = 1;
export const RECORD_PRESENTED = 2;

// Floats in one actor row: time, opacity, graph tick fraction, padding,
// then the palette every particle of the actor starts from.
export const PARTICLE_ACTOR = 20;
export const ACTOR_TIME = 0;
export const ACTOR_OPACITY = 1;
export const ACTOR_FRACTION = 2;
export const ACTOR_PALETTE = 4;

// The EFP view modes (effect-billboard.ts) as the pass numbers them.
export const PARTICLE_VIEW_NONE = 0;
export const PARTICLE_VIEW_CAMERA = 1;
export const PARTICLE_VIEW_Y = 2;
export const PARTICLE_VIEW_V = 3;

/*
================
particleView
================
*/
export function particleView( mode: "camera" | "y" | "v" | undefined ): number {
	return mode === "camera" ?
		PARTICLE_VIEW_CAMERA :
		mode === "y" ?
		PARTICLE_VIEW_Y :
		mode === "v" ?
		PARTICLE_VIEW_V :
		PARTICLE_VIEW_NONE;
}

/*
================
writeTickPose

The program pose part of a record: the state at its latest tick and the
motion of that tick (none before the first tick).
================
*/
function writeTickPose( records: Float32Array, at: number, state: ParticleInstance, work: Float64Array ): void {
	const previous = state.previous;
	for ( let axis = 0; axis < 3; axis++ ) {
		const position = state.position[axis]!, scale = state.scale[axis]!;
		records[at + RECORD_POSITION + axis] = position;
		records[at + RECORD_SCALE + axis] = scale;
		records[at + RECORD_POSITION_MOTION + axis] = previous ? position - previous.position[axis]! : 0;
		records[at + RECORD_SCALE_MOTION + axis] = previous ? scale - previous.scale[axis]! : 0;
		records[at + RECORD_VELOCITY + axis] = state.velocity[axis]!;
	}
	records[at + RECORD_TICK] = state.frame;
	records.set( state.rotation, at + RECORD_ROTATION );
	if ( previous ) tickRotationStep( previous.rotation, state.rotation, records, at + RECORD_ROTATION_STEP, work );
	else records.fill( 0, at + RECORD_ROTATION_STEP, at + RECORD_ROTATION_STEP + 4 );
}

/*
================
writeGraphRecord

A live particle graph element. Its instance matrix continues the element
frame's last tick rotation and moves its position by its last tick motion
(particleElementMatrix). work is ROTATION_WORK scratch.
================
*/
export function writeGraphRecord(
	records: Float32Array,
	slot: number,
	element: ParticleElement,
	work: Float64Array
): void {
	const at = slot * PARTICLE_RECORD, state = element.state, previous = state.previous;
	records.set( element.matrix, at + RECORD_MATRIX );
	for ( let axis = 0; axis < 3; axis++ ) {
		const position = state.position[axis]!;
		records[at + RECORD_MATRIX + 12 + axis] = position;
		records[at + RECORD_MOTION + axis] = position - element.previousPosition[axis]!;
	}
	if ( previous ) tickRotationStep( previous.matrix, element.matrix, records, at + RECORD_MATRIX_STEP, work );
	else records.fill( 0, at + RECORD_MATRIX_STEP, at + RECORD_MATRIX_STEP + 4 );
	writeTickPose( records, at, state, work );
	records[at + RECORD_BIRTH] = element.clockBirth / PARTICLE_TICKS_PER_SECOND;
	records[at + RECORD_FLAGS] = RECORD_LIVE | RECORD_PRESENTED;
}

/*
================
writeEmittedRecord

A live emitted particle of a primitive's own emission: drawn at its birth
matrix (matrices at matrixAt), with its program pose when it has one.
================
*/
export function writeEmittedRecord(
	records: Float32Array,
	slot: number,
	matrices: Float32Array,
	matrixAt: number,
	birth: number,
	state: ParticleInstance | undefined,
	work: Float64Array
): void {
	const at = slot * PARTICLE_RECORD;
	for ( let i = 0; i < 16; i++ ) records[at + RECORD_MATRIX + i] = matrices[matrixAt + i]!;
	records.fill( 0, at + RECORD_MATRIX_STEP, at + RECORD_MATRIX_STEP + 7 );
	if ( state ) writeTickPose( records, at, state, work );
	records[at + RECORD_BIRTH] = birth;
	records[at + RECORD_FLAGS] = state ? RECORD_LIVE | RECORD_PRESENTED : RECORD_LIVE;
}

/*
================
emittedMatrixMatches

True when the record already holds the birth matrix at matrices[matrixAt]:
a following emitter or a region crossing moves it without a tick.
================
*/
export function emittedMatrixMatches(
	records: Float32Array,
	slot: number,
	matrices: Float32Array,
	matrixAt: number
): boolean {
	const at = slot * PARTICLE_RECORD + RECORD_MATRIX;
	for ( let i = 0; i < 16; i++ ) if ( records[at + i] !== matrices[matrixAt + i] ) return false;
	return true;
}

/*
================
hideRecord

An empty slot: the pass draws it as a zero matrix.
================
*/
export function hideRecord( records: Float32Array, slot: number ): void {
	records[slot * PARTICLE_RECORD + RECORD_FLAGS] = 0;
}

/*
================
recordLive
================
*/
export function recordLive( records: Float32Array, slot: number ): boolean {
	return records[slot * PARTICLE_RECORD + RECORD_FLAGS] !== 0;
}
