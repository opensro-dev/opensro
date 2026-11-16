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
ribbonElementFrames does on the CPU). Rotation motion is a step (axis
and angle, tickRotationStep), found once a tick.

A particle graph's records are written by its tick (particle-graph.ts),
once per tick, into the graph's own storage (particle-graph-slots.ts); a
stream copies them as a block. The ribbon tick frames beside them are the
same tick values in doubles, for the CPU ribbon strips.

The layout is the WGSL Record struct: vec3 members are followed by one
scalar, so every group fills 16 bytes.

===========================================================================
*/

import type { ParticleGraphState } from "@/engine/foundation/animation/particle-graph-slots";
import { LINK_MATRIX, rootTickShift } from "@/engine/foundation/animation/particle-holder-link";
import { multiplyAt } from "@/engine/foundation/math/pose-math";
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

// Floats of one ribbon tick frame (a ribbon's CPU strip, graph-owned
// beside the records): the element frame's last tick rotation step (axis
// xyz, angle), its own last tick motion, 1 while its element is live.
export const RIBBON_TICK = 8;
export const RIBBON_STEP = 0;
export const RIBBON_MOTION = 4;
const RIBBON_LIVE = 7;

// RECORD_FLAGS bits. LIVE: the slot holds a particle. PRESENTED: it has a
// program pose (a plain emitted mesh has none and keeps its palette). The
// holder links (particle-holder-link.ts LINK_*) take bits 4, 8 and 16.
export const RECORD_LIVE = 1;
export const RECORD_PRESENTED = 2;

// Floats in one actor row: time, opacity, graph tick fraction, padding,
// the palette every particle of the actor starts from, then a graph's
// holder follow (holderFollow) its linked elements ride this frame.
export const ACTOR_TIME = 0;
export const ACTOR_OPACITY = 1;
export const ACTOR_FRACTION = 2;
export const ACTOR_PALETTE = 4;
export const ACTOR_FOLLOW = 20;
// ACTOR_FOLLOW + HOLDER_FOLLOW (particle-holder-link.ts); the shader's ACTOR_ROW
// is this in vec4s.
export const PARTICLE_ACTOR = 40;

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
motion of that tick (none before the first tick), of an emitted
particle (writeGraphRecord writes a graph element's from its slot).
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
copyMatrix

16 floats of from at fromAt into to.
================
*/
function copyMatrix( from: Float32Array, fromAt: number, to: Float32Array ): void {
	for ( let i = 0; i < 16; i++ ) to[i] = from[fromAt + i]!;
}

/*
================
graphTickShift

The part of slot s's last tick motion its holder links caused
(rootTickShift), into graph.scratch.shift: zero without links. The holder
is the graph's root slot 0.
================
*/
function graphTickShift( graph: ParticleGraphState, s: number, flags: number ): Float64Array {
	const shift = graph.scratch.shift;
	if ( !flags ) {
		shift.fill( 0 );
		return shift;
	}
	const previous = graph.scratch.vector, at = s * 3;
	for ( let axis = 0; axis < 3; axis++ ) previous[axis] = graph.previousPosition[at + axis]!;
	rootTickShift( flags, previous, graph.delta, graph.origin, graph.position, shift );
	return shift;
}

/*
================
graphRotationStep

The element frame's last tick rotation step of slot s (tickRotationStep),
written at out[at..at+3]: from its snapshot entry's frame, turned by the
holder's last tick turn when it rides the holder's turn (LINK_MATRIX), to
its frame. Zero without a snapshot (snapshot -1).
================
*/
function graphRotationStep(
	graph: ParticleGraphState,
	s: number,
	snapshot: number,
	flags: number,
	out: Float32Array | Float64Array,
	at: number
): void {
	if ( snapshot < 0 ) {
		out.fill( 0, at, at + 4 );
		return;
	}
	const scratch = graph.scratch, from = scratch.turned;
	if ( flags & LINK_MATRIX ) multiplyAt( graph.delta, 0, graph.previousMatrix, snapshot * 16, from, 0 );
	else copyMatrix( graph.previousMatrix, snapshot * 16, from );
	copyMatrix( graph.matrix, s * 16, scratch.matrix );
	tickRotationStep( from, scratch.matrix, out, at, scratch.work );
}

/*
================
writeGraphRecord

A live particle graph element, slot s with its tick pose at snapshot
entry snapshot (-1 before its first tick): its instance matrix continues
the element frame's last tick rotation and moves its position by its last
tick motion (ribbonElementFrames). An element that rides its holder
(flags, its holder links) records only its own motion and its links in
the flags; the pass carries it by the actor's holder follow. Graph
streams draw no program position, so RECORD_POSITION and
RECORD_POSITION_MOTION are left as they are.
================
*/
export function writeGraphRecord(
	records: Float32Array,
	slot: number,
	graph: ParticleGraphState,
	s: number,
	snapshot: number,
	flags: number
): void {
	const at = slot * PARTICLE_RECORD, shift = graphTickShift( graph, s, flags );
	for ( let i = 0; i < 16; i++ ) records[at + RECORD_MATRIX + i] = graph.matrix[s * 16 + i]!;
	for ( let axis = 0; axis < 3; axis++ ) {
		const position = graph.position[s * 3 + axis]!;
		records[at + RECORD_MATRIX + 12 + axis] = position;
		records[at + RECORD_MOTION + axis] = position - graph.previousPosition[s * 3 + axis]! - shift[axis]!;
	}
	graphRotationStep( graph, s, snapshot, flags, records, at + RECORD_MATRIX_STEP );
	for ( let axis = 0; axis < 3; axis++ ) {
		const scale = graph.scale[s * 3 + axis]!;
		records[at + RECORD_SCALE + axis] = scale;
		records[at + RECORD_SCALE_MOTION + axis] = snapshot < 0 ? 0 : scale - graph.previousScale[snapshot * 3 + axis]!;
		records[at + RECORD_VELOCITY + axis] = graph.velocity[s * 3 + axis]!;
	}
	records[at + RECORD_TICK] = graph.frames[s]!;
	for ( let i = 0; i < 16; i++ ) records[at + RECORD_ROTATION + i] = graph.rotation[s * 16 + i]!;
	if ( snapshot < 0 ) records.fill( 0, at + RECORD_ROTATION_STEP, at + RECORD_ROTATION_STEP + 4 );
	else {
		const scratch = graph.scratch;
		copyMatrix( graph.previousRotation, snapshot * 16, scratch.turned );
		copyMatrix( graph.rotation, s * 16, scratch.matrix );
		tickRotationStep( scratch.turned, scratch.matrix, records, at + RECORD_ROTATION_STEP, scratch.work );
	}
	records[at + RECORD_BIRTH] = graph.clockBirth[s]! / PARTICLE_TICKS_PER_SECOND;
	records[at + RECORD_FLAGS] = RECORD_LIVE | RECORD_PRESENTED | flags;
}

/*
================
writeRibbonTick

A live element a ribbon draws, slot s (snapshot and flags as
writeGraphRecord): its ribbon tick frame at entry, in doubles, as the
CPU strip continues it every frame (ribbonElementFrames).
================
*/
export function writeRibbonTick(
	ticks: Float64Array,
	entry: number,
	graph: ParticleGraphState,
	s: number,
	snapshot: number,
	flags: number
): void {
	const at = entry * RIBBON_TICK, shift = graphTickShift( graph, s, flags );
	graphRotationStep( graph, s, snapshot, flags, ticks, at + RIBBON_STEP );
	for ( let axis = 0; axis < 3; axis++ ) {
		ticks[at + RIBBON_MOTION + axis] = graph.position[s * 3 + axis]! - graph.previousPosition[s * 3 + axis]! -
			shift[axis]!;
	}
	ticks[at + RIBBON_LIVE] = 1;
}

/*
================
hideRibbonTick

An entry whose element is not live: cleared once, so a graph's state does
not depend on when it was last drawn.
================
*/
export function hideRibbonTick( ticks: Float64Array, entry: number ): void {
	const at = entry * RIBBON_TICK;
	if ( ticks[at + RIBBON_LIVE] !== 0 ) ticks.fill( 0, at, at + RIBBON_TICK );
}

/*
================
clearRecord

A graph record whose element is not live: cleared once (hideRecord
clears only its flags), so a graph's state does not depend on when it was
last drawn.
================
*/
export function clearRecord( records: Float32Array, slot: number ): void {
	const at = slot * PARTICLE_RECORD;
	if ( records[at + RECORD_FLAGS] !== 0 ) records.fill( 0, at, at + PARTICLE_RECORD );
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
