/*
===========================================================================

particle-graph-slots.ts - an EFP element tree's state in flat slot storage

A particle graph (particle-graph.ts) is ticked for every effect on screen,
and an element object with its own vectors and matrices cost about 2.9 KB
of heap per occupied slot and a collection pause whenever many died. Here a
graph's elements live in one struct of arrays: slot s of emitter n is
plan.base[n] + b for its slot b, and slot 0 is the holder (the graph
root). Every array is sized at the emitters' capacity when the graph is
created, so a tick writes in place and allocates nothing.

The tick kernel (particle-graph.ts) is the only writer; presentation and
tests read the arrays through the slot of an emitter's slot b
(particleSlot).

===========================================================================
*/

import type { ParticleEmitter } from "@/engine/foundation/animation/particle-graph";
import { createParticleGraphPlan, type ParticleGraphPlan } from "@/engine/foundation/animation/particle-graph-plan";
import type { ParticleSample } from "@/engine/foundation/animation/particle-program";
import { followMatrix } from "@/engine/foundation/animation/particle-holder-link";
import { ROTATION_WORK, turnByStep } from "@/engine/foundation/animation/particle-presentation";
import {
	PARTICLE_RECORD,
	RIBBON_MOTION,
	RIBBON_STEP,
	RIBBON_TICK
} from "@/engine/foundation/animation/particle-records";

// The holder's slot, and a slot that names no element.
export const ROOT_SLOT = 0;
export const NO_SLOT = -1;

// flags bits. OCCUPIED: the slot holds an element (an emission slot
// reserved for a birth holds none until it is born). ALIVE: it has not
// expired. RELEASED: it has expired and holds no descendant, so its slot
// may be reused. SNAPSHOT: its tick pose holds the start of its latest
// tick (a drawn emitter's element after its first tick).
export const SLOT_OCCUPIED = 1;
export const SLOT_ALIVE = 2;
export const SLOT_RELEASED = 4;
export const SLOT_SNAPSHOT = 8;

/*
================
ParticleGraphScratch

Per-graph working storage for the tick loop. Owned by the graph state so
no module holds mutable state.
================
*/
export interface ParticleGraphScratch {
	readonly matrix: Float32Array;
	readonly inverse: Float32Array;
	readonly previous: Float32Array;
	// A random sample's basis, its vectors and their scratch.
	readonly basis: Float32Array;
	readonly sample: ParticleSample;
	readonly oldRoot: Float64Array;
	readonly inherited: Float64Array;
	readonly vector: Float64Array;
	// An element's predecessor of the same parent (sibling commands), and
	// the latest element per parent slot while they are found.
	readonly siblings: Int32Array;
	readonly latest: Int32Array;
	// Record writing: the holder's turned frame and tick shift, the rotation
	// work.
	readonly turned: Float32Array;
	readonly shift: Float64Array;
	readonly work: Float64Array;
}

/*
================
ParticleGraphState

One graph's state, every array indexed by slot (or by an emitter block's
entry, plan.snapshots and the rest). Per slot:

- flags (SLOT_*), serials (birth order, the holder 0), parent (the slot of
  the element it was born from; an emission slot's parent is set when the
  slot is reserved), children (descendants that hold it), link (the
  holder links it carries, particle-holder-link.ts).
- birth (its birth tick), clockBirth (the birth its age counts from, which
  a loop moves on), age, frames (its program frame).
- x16 float32: matrix (its frame), delta (its frame's turn this tick),
  local (its own turn per tick), shape (its shape spin), rotation (its
  program rotation).
- x3 float64: position, velocity, scale, origin (its position at the start
  of the tick's links), previousPosition (its position at the start of its
  latest tick). Doubles: a region shift adds without rounding to float32.

Per emitter n: counts[n], the slots b in use (0..counts[n]-1; a slot
comes into use at its first birth and stays in use). Drawn emitters keep the tick pose their elements
had at the start of their latest tick (previousScale, previousRotation,
previousMatrix, by snapshot entry) and the tick writes their records
(records, PARTICLE_RECORD floats an entry, particle-records.ts) or ribbon
frames (ribbonTicks, RIBBON_TICK an entry). groupTotals hold each emission
group's float accumulator (particleEmission), by its parent's entry;
orders the slots of an emitter's unreleased elements in birth order.
revision counts the record writes, so a reader copies them only when
they changed.
================
*/
export interface ParticleGraphState {
	serial: number;
	// frame, birth and clockBirth count ticks of the graph's own clock, whose
	// tick 0 is the actor's tick startTick (particle-graph-clock.ts).
	startTick: number;
	frame: number;
	index: number;
	revision: number;
	// The model's plan (particle-graph-plan.ts): the tables every tick reads.
	readonly plan: ParticleGraphPlan;
	readonly counts: Int32Array;
	readonly flags: Uint8Array;
	readonly link: Uint8Array;
	readonly serials: Int32Array;
	readonly parent: Int32Array;
	readonly children: Int32Array;
	readonly birth: Float64Array;
	readonly clockBirth: Float64Array;
	readonly age: Float64Array;
	readonly frames: Float64Array;
	readonly matrix: Float32Array;
	readonly delta: Float32Array;
	readonly local: Float32Array;
	readonly shape: Float32Array;
	readonly rotation: Float32Array;
	readonly position: Float64Array;
	readonly velocity: Float64Array;
	readonly scale: Float64Array;
	readonly origin: Float64Array;
	readonly previousPosition: Float64Array;
	readonly previousScale: Float64Array;
	readonly previousRotation: Float32Array;
	readonly previousMatrix: Float32Array;
	readonly records: Float32Array;
	readonly ribbonTicks: Float64Array;
	readonly groupTotals: Float32Array;
	readonly orders: Int32Array;
	readonly orderCounts: Int32Array;
	readonly scratch: ParticleGraphScratch;
}

/*
================
setIdentityAt

The identity at m[at..at+15].
================
*/
export function setIdentityAt( m: Float32Array, at: number ): void {
	m.fill( 0, at, at + 16 );
	m[at] =
		m[at + 5] =
		m[at + 10] =
		m[at + 15] =
			1;
}

/*
================
identities

count identity matrices.
================
*/
function identities( count: number ): Float32Array {
	const m = new Float32Array( count * 16 );
	for ( let s = 0; s < count; s++ ) setIdentityAt( m, s * 16 );
	return m;
}

/*
================
createParticleGraph

plan is graph's plan; a caller that ticks many graphs of one model passes
the model's, built once with its primitives (createParticleGraphPlan).
Every slot starts empty but for the holder's: the identity frame at the
origin, alive.
================
*/
export function createParticleGraph(
	graph: readonly ParticleEmitter[],
	index: number,
	plan = createParticleGraphPlan( graph )
): ParticleGraphState {
	if ( plan.graph !== graph ) throw Error( "Particle graph plan of another graph" );
	const total = plan.total, snapshots = plan.snapshotTotal;
	const state: ParticleGraphState = {
		serial: 0,
		startTick: 0,
		frame: -1,
		index,
		revision: 0,
		plan,
		counts: new Int32Array( graph.length ),
		flags: new Uint8Array( total ),
		link: new Uint8Array( total ),
		serials: new Int32Array( total ),
		parent: new Int32Array( total ).fill( NO_SLOT ),
		children: new Int32Array( total ),
		birth: new Float64Array( total ),
		clockBirth: new Float64Array( total ),
		age: new Float64Array( total ),
		frames: new Float64Array( total ),
		matrix: new Float32Array( total * 16 ),
		delta: new Float32Array( total * 16 ),
		local: new Float32Array( total * 16 ),
		shape: new Float32Array( total * 16 ),
		rotation: new Float32Array( total * 16 ),
		position: new Float64Array( total * 3 ),
		velocity: new Float64Array( total * 3 ),
		scale: new Float64Array( total * 3 ),
		origin: new Float64Array( total * 3 ),
		previousPosition: new Float64Array( total * 3 ),
		previousScale: new Float64Array( snapshots * 3 ),
		previousRotation: new Float32Array( snapshots * 16 ),
		previousMatrix: new Float32Array( snapshots * 16 ),
		records: new Float32Array( plan.recordTotal * PARTICLE_RECORD ),
		ribbonTicks: new Float64Array( plan.ribbonTotal * RIBBON_TICK ),
		groupTotals: new Float32Array( plan.groupTotal ),
		orders: new Int32Array( plan.orderTotal ),
		orderCounts: new Int32Array( graph.length ),
		scratch: {
			matrix: identities( 1 ),
			inverse: identities( 1 ),
			previous: identities( 1 ),
			basis: identities( 1 ),
			sample: {
				position: new Float64Array( 3 ),
				velocity: new Float64Array( 3 ),
				scale: new Float64Array( 3 ),
				at: 0,
				work: new Float64Array( 3 )
			},
			oldRoot: new Float64Array( 3 ),
			inherited: new Float64Array( 3 ),
			vector: new Float64Array( 3 ),
			siblings: new Int32Array( total ).fill( NO_SLOT ),
			latest: new Int32Array( plan.latest ).fill( NO_SLOT ),
			turned: new Float32Array( 16 ),
			shift: new Float64Array( 3 ),
			work: new Float64Array( ROTATION_WORK )
		}
	};
	// A fixed-birth emitter's slots keep their authored birth ticks.
	for ( let n = 0; n < graph.length; n++ ) {
		const def = graph[n]!;
		if ( !def.emission ) state.birth.set( def.births, plan.base[n]! );
	}
	state.flags[ROOT_SLOT] = SLOT_OCCUPIED | SLOT_ALIVE;
	state.parent[ROOT_SLOT] = ROOT_SLOT;
	for ( const m of [ state.matrix, state.delta, state.local, state.shape, state.rotation ] ) {
		setIdentityAt( m, ROOT_SLOT * 16 );
	}
	for ( let axis = 0; axis < 3; axis++ ) state.scale[ROOT_SLOT * 3 + axis] = 1;
	return state;
}

/*
================
particleSlot

The slot of emitter n's slot b.
================
*/
export function particleSlot( graph: ParticleGraphState, n: number, b: number ): number {
	return graph.plan.base[n]! + b;
}

/*
================
slotAlive
================
*/
export function slotAlive( graph: ParticleGraphState, s: number ): boolean {
	return (graph.flags[s]! & SLOT_ALIVE) !== 0;
}

/*
================
slotOccupied
================
*/
export function slotOccupied( graph: ParticleGraphState, s: number ): boolean {
	return (graph.flags[s]! & SLOT_OCCUPIED) !== 0;
}

/*
================
ribbonElementFrames

The drawn frames of emitter n's live elements, each at out[b * 16] for its
slot b (a ribbon's strip runs through them; other slots are left as they
are). Presentation-only bounded prediction: exact tick poses are
unchanged, and no random draw, command, birth or retirement occurs here.
Each frame continues its element frame's last tick rotation by fraction
of the next tick (particle-presentation.ts, deliberate smoothing
deviation) and its own last tick motion; an element that rides its holder
is then carried by the holder's motion since the tick (follow, this
frame's holderFollow), as native B153A0 carries it every draw. The tick
wrote those steps and motions (particle-records.ts writeRibbonTick). work
is ROTATION_WORK scratch.
================
*/
export function ribbonElementFrames(
	graph: ParticleGraphState,
	n: number,
	fraction: number,
	follow: Float32Array,
	out: Float32Array,
	work: Float64Array
): void {
	const plan = graph.plan, first = plan.ribbons[n]!, base = plan.base[n]!, count = graph.counts[n]!;
	if ( first < 0 ) throw Error( "Particle emitter drawn by no ribbon" );
	const blend = Math.max( 0, Math.min( 1, fraction ) ), ticks = graph.ribbonTicks, current = graph.scratch.matrix;
	for ( let b = 0; b < count; b++ ) {
		const s = base + b;
		if ( !(graph.flags[s]! & SLOT_ALIVE) ) continue;
		const at = (first + b) * RIBBON_TICK, offset = b * 16;
		for ( let i = 0; i < 16; i++ ) current[i] = graph.matrix[s * 16 + i]!;
		turnByStep(
			current,
			ticks[at + RIBBON_STEP]!,
			ticks[at + RIBBON_STEP + 1]!,
			ticks[at + RIBBON_STEP + 2]!,
			ticks[at + RIBBON_STEP + 3]!,
			blend,
			out,
			offset,
			work
		);
		for ( let axis = 0; axis < 3; axis++ ) {
			out[offset + 12 + axis] = graph.position[s * 3 + axis]! + ticks[at + RIBBON_MOTION + axis]! * blend;
		}
		const flags = graph.link[s]!;
		if ( flags ) followMatrix( flags, follow, 0, out, offset );
	}
}
