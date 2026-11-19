/*
===========================================================================

particle-graph.ts - the EFP element tree, ticked at the native 20 Hz

One chronological owner updates invisible parents before their child
groups. Retained expired elements supply identity and state to their
descendants, never new draws.

Every value keeps the native float32 stores and evaluation order; what an
element's commands do is particle-graph-commands.ts. The
tick runs for every effect on screen, so it works over the graph's flat
slot storage (particle-graph-slots.ts) and its scratch and allocates
nothing: element objects cost about 2.9 KB of heap a slot, and an
allocation trace showed the tick producing 300 MB of garbage in 18 s,
which the main thread then paid for in collection pauses. The tick also
writes the GPU records (and ribbon tick frames) of the emitters the
model's primitives draw, once per tick (particle-records.ts).

===========================================================================
*/

import {
	particleEmission,
	type ParticleEmitter as EmissionParameters
} from "@/engine/foundation/animation/particle-emission";
import { multiplyAt } from "@/engine/foundation/math/pose-math";
import { sampleParticle, sampleScaleInto, type ParticleProgram } from "@/engine/foundation/animation/particle-program";
import {
	affineInto,
	attract,
	commands,
	copy16,
	copy3,
	setVector,
	vectors
} from "@/engine/foundation/animation/particle-graph-commands";
import { graphStartTick } from "@/engine/foundation/animation/particle-graph-clock";
import { onFrame } from "@/engine/foundation/animation/particle-graph-plan";
import {
	NO_SLOT,
	ROOT_SLOT,
	setIdentityAt,
	SLOT_ALIVE,
	SLOT_OCCUPIED,
	SLOT_RELEASED,
	SLOT_SNAPSHOT,
	type ParticleGraphState
} from "@/engine/foundation/animation/particle-graph-slots";
import {
	clearRecord,
	hideRibbonTick,
	writeGraphRecord,
	writeRibbonTick
} from "@/engine/foundation/animation/particle-records";

/*
================
ParticleEmitter

Immutable authored commands and linkage for one native EFP element group.
================
*/
export interface ParticleEmitter {
	readonly emission?: EmissionParameters;
	readonly capacity?: number;
	readonly loop?: boolean;
	readonly commands?: readonly {
		readonly name: string;
		readonly frames: readonly number[];
		readonly program: ParticleProgram;
		readonly rotations?: readonly (readonly number[])[];
		readonly positions?: readonly (readonly number[])[];
		readonly flags?: number;
	}[];
	readonly parent: number;
	readonly parents: readonly number[];
	readonly births: readonly number[];
	readonly frames: number;
	readonly program?: ParticleProgram;
	readonly matrix?: readonly number[];
	readonly localMotion?: boolean;
	readonly shapeMotion?: boolean;
	readonly keepMatrix: boolean;
	readonly keepOrigin: boolean;
	readonly positionDepth: number;
	readonly matrixDepth: number;
	readonly velocityDepth: number;
	readonly followDepth: number;
	readonly scales: readonly (readonly number[])[];
	readonly positions: readonly (readonly number[])[];
	readonly rotations: readonly (readonly number[])[];
}

// A2E980: whole 50 ms effect ticks.
const TICKS_PER_SECOND = 20;
// advanceParticle's bound on a program frame (particle-program.ts).
const MAX_PROGRAM_FRAME = 1200;
// The program a birth samples when its emitter runs commands or has none.
const NO_PROGRAM = {} as const;

// ============================================================================

/*
================
copyTranslation

The holder transform's translation into to[toAt..].
================
*/
function copyTranslation( transform: Float32Array, to: Float64Array, toAt: number ): void {
	to[toAt] = transform[12]!;
	to[toAt + 1] = transform[13]!;
	to[toAt + 2] = transform[14]!;
}

/*
================
inverseInto

Affine inverse into out. The 3x3 is stored as float32 first, and the
translation is formed from those stored values, as the native does.

A singular frame (a zero rotation axis collapses an element's basis,
particle-rotation.ts) inverts to itself. CEFEffect_UpdateElementMatrices
(AF13D0) calls D3DXMatrixInverse( m, NULL, m ) in place and ignores the
NULL it returns for a zero determinant, which leaves m unwritten; the
element then multiplies by its own frame. No epsilon: the native tests
the determinant against zero only.
================
*/
function inverseInto( m: Float32Array, out: Float32Array ): void {
	const a = m[0]!, b = m[4]!, c = m[8]!, d = m[1]!, e = m[5]!, f = m[9]!, g = m[2]!, h = m[6]!, i = m[10]!;
	const det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g);
	if ( det === 0 ) {
		out.set( m.subarray( 0, 16 ) );
		return;
	}
	out[0] = (e * i - f * h) / det;
	out[1] = (f * g - d * i) / det;
	out[2] = (d * h - e * g) / det;
	out[3] = 0;
	out[4] = (c * h - b * i) / det;
	out[5] = (a * i - c * g) / det;
	out[6] = (b * g - a * h) / det;
	out[7] = 0;
	out[8] = (b * f - c * e) / det;
	out[9] = (c * d - a * f) / det;
	out[10] = (a * e - b * d) / det;
	out[11] = 0;
	out[15] = 1;
	const x = -m[12]!, y = -m[13]!, z = -m[14]!;
	// The translation reads the stored column values with a zero source
	// translation (out[12..14] are not yet part of the sum).
	out[12] = out[13] = out[14] = 0;
	const tx = Math.fround( out[0]! * x + out[4]! * y + out[8]! * z + 0 ),
		ty = Math.fround( out[1]! * x + out[5]! * y + out[9]! * z + 0 ),
		tz = Math.fround( out[2]! * x + out[6]! * y + out[10]! * z + 0 );
	out[12] = tx;
	out[13] = ty;
	out[14] = tz;
}

/*
================
ancestor

The slot depth parents up from slot s, stopping at the holder; NO_SLOT for
depth 0.
================
*/
function ancestor( graph: ParticleGraphState, s: number, depth: number ): number {
	if ( !depth ) return NO_SLOT;
	const parent = graph.parent;
	let result = parent[s]!;
	for ( let n = 1; n < depth && parent[result] !== result; n++ ) result = parent[result]!;
	return result;
}

// ============================================================================

/*
================
linkElement

AF13D0's links for slot s on a tick: keep its origin, turn its position
about the position ancestor, take the matrix ancestor's turn, turn its
velocity, and carry it by the follow ancestor's move. motion adds the
element's own local turn, which only a live element's program runs.
================
*/
function linkElement( graph: ParticleGraphState, s: number, def: ParticleEmitter, motion: boolean ) {
	const position = graph.position, origin = graph.origin, matrix = graph.matrix, at = s * 3;
	const scratch = graph.scratch.matrix;
	if ( def.keepOrigin ) copy3( position, at, origin, at );
	const positionParent = ancestor( graph, s, def.positionDepth ),
		matrixParent = ancestor( graph, s, Math.max( 0, def.matrixDepth ) ),
		velocityParent = ancestor( graph, s, def.velocityDepth ),
		follow = ancestor( graph, s, def.followDepth );
	if ( positionParent >= 0 ) {
		const from = positionParent * 3;
		affineInto(
			graph.delta,
			positionParent * 16,
			position[at]! - origin[from]!,
			position[at + 1]! - origin[from + 1]!,
			position[at + 2]! - origin[from + 2]!,
			position,
			at
		);
		for ( let i = 0; i < 3; i++ ) position[at + i] = Math.fround( position[at + i]! + origin[from + i]! );
	}
	if ( matrixParent >= 0 ) {
		multiplyAt( graph.delta, matrixParent * 16, matrix, s * 16, scratch, 0 );
		matrix.set( scratch, s * 16 );
	}
	if ( motion && def.localMotion ) {
		multiplyAt( matrix, s * 16, graph.local, s * 16, scratch, 0 );
		matrix.set( scratch, s * 16 );
	}
	if ( velocityParent >= 0 ) {
		const velocity = graph.velocity;
		affineInto(
			graph.delta,
			velocityParent * 16,
			velocity[at]!,
			velocity[at + 1]!,
			velocity[at + 2]!,
			velocity,
			at
		);
	}
	if ( follow >= 0 ) {
		const from = follow * 3;
		for ( let i = 0; i < 3; i++ ) {
			position[at + i] = Math.fround( position[at + i]! + position[from + i]! - origin[from + i]! );
		}
	}
}

/*
================
advanceMotion

advanceParticle (particle-program.ts) for slot s by one program frame, on
its emitter's motion program: the plan strips spin and attraction from it
(they run from the slot's shape and the attraction frames), and a graph
element carries no angular velocity, so a frame moves the position by the
velocity and resamples the random scale on its frames.
================
*/
function advanceMotion( graph: ParticleGraphState, s: number, program: ParticleProgram, table: Float32Array ) {
	const from = graph.frames[s]!, frame = from + 1, position = graph.position, at = s * 3;
	if ( !Number.isSafeInteger( frame ) || frame > MAX_PROGRAM_FRAME ) throw Error( "Invalid particle frame" );
	for ( let axis = 0; axis < 3; axis++ ) {
		position[at + axis] = Math.fround( position[at + axis]! + graph.velocity[at + axis]! );
	}
	if ( program.scale && program.scaleFrames?.includes( frame ) ) {
		graph.index = (graph.index + 1) % table.length;
		sampleScaleInto( program, table[graph.index]!, graph.scale, at );
	}
	graph.frames[s] = frame;
}

/*
================
groupEntry

The groupTotals entry of the emission group of emitter n that parent
slot parent leads.
================
*/
function groupEntry( graph: ParticleGraphState, n: number, parent: number ): number {
	const plan = graph.plan, up = plan.graph[n]!.parent;
	return plan.groups[n]! + (up < 0 ? 0 : parent - plan.base[up]!);
}

/*
================
removeOrder

Slot s leaves emitter n's birth order (its release).
================
*/
function removeOrder( graph: ParticleGraphState, n: number, s: number ): void {
	const orders = graph.orders, first = graph.plan.orders[n]!, count = graph.orderCounts[n]!;
	let k = 0;
	while ( k < count && orders[first + k] !== s ) k++;
	if ( k === count ) throw Error( "Particle slot outside its birth order" );
	orders.copyWithin( first + k, first + k + 1, first + count );
	graph.orderCounts[n] = count - 1;
}

/*
================
release

AF2AC0: slot s, of emitter n, has died and holds no children, so its
identity is released; a dead parent whose last child this was follows.
The released set is the same in any order, and slot reuse depends only
on that set. An emitter's unreleased elements are the groups its emitting
children emit from (removeOrder ends them).
================
*/
function release( graph: ParticleGraphState, s: number, n: number ): void {
	const plan = graph.plan, flags = graph.flags, children = graph.children;
	for ( ;; ) {
		flags[s]! |= SLOT_RELEASED;
		if ( plan.orders[n]! >= 0 ) removeOrder( graph, n, s );
		const parent = graph.parent[s]!;
		children[parent]!--;
		n = plan.graph[n]!.parent;
		if ( n < 0 || flags[parent]! & (SLOT_ALIVE | SLOT_RELEASED) || children[parent] ) return;
		s = parent;
	}
}

/*
================
shiftSlots

A region crossing moves every element's position, origin and previous
position, and the holder's position, by the shift.
================
*/
function shiftSlots( graph: ParticleGraphState, shiftX: number, shiftZ: number ): void {
	const plan = graph.plan, position = graph.position, origin = graph.origin, previous = graph.previousPosition;
	for ( let n = 0; n < plan.graph.length; n++ ) {
		const base = plan.base[n]!, count = graph.counts[n]!;
		for ( let b = 0; b < count; b++ ) {
			const s = base + b, at = s * 3;
			if ( !(graph.flags[s]! & SLOT_OCCUPIED) ) continue;
			position[at]! += shiftX;
			position[at + 2]! += shiftZ;
			origin[at]! += shiftX;
			origin[at + 2]! += shiftZ;
			previous[at]! += shiftX;
			previous[at + 2]! += shiftZ;
		}
	}
	position[ROOT_SLOT * 3]! += shiftX;
	position[ROOT_SLOT * 3 + 2]! += shiftZ;
}

/*
================
retireSlots

The start of a tick for every live element: its previous position and,
for a drawn emitter, its tick pose; its age on the clock (a loop moves its
clock birth on); and its death at the end of its lifetime. AF2A40 returns
capacity on retirement, even while descendants retain the dead parent.
================
*/
function retireSlots( graph: ParticleGraphState, tick: number, start: number, stop: number, keepRoots: boolean ) {
	const plan = graph.plan, graphDefs = plan.graph, flags = graph.flags, position = graph.position;
	for ( let n = 0; n < graphDefs.length; n++ ) {
		const def = graphDefs[n]!, base = plan.base[n]!, count = graph.counts[n]!, snapshots = plan.snapshots[n]!;
		for ( let b = 0; b < count; b++ ) {
			const s = base + b;
			if ( !(flags[s]! & SLOT_ALIVE) ) continue;
			copy3( position, s * 3, graph.previousPosition, s * 3 );
			if ( snapshots >= 0 ) snapshotSlot( graph, s, snapshots + b );
			const clockBirth = graph.clockBirth[s]!;
			if (
				(def.loop || keepRoots && def.parent < 0 && (start + tick) / TICKS_PER_SECOND < stop) &&
				tick - clockBirth >= def.frames
			) graph.clockBirth[s] = clockBirth + Math.floor( (tick - clockBirth) / def.frames ) * def.frames;
			const age = graph.age[s] = tick - graph.clockBirth[s]!;
			if ( age >= def.frames ) {
				flags[s]! &= ~SLOT_ALIVE;
				if ( def.emission ) {
					const entry = groupEntry( graph, n, graph.parent[s]! );
					graph.groupTotals[entry] = Math.fround( graph.groupTotals[entry]! - 1 );
				}
				copy3( position, s * 3, graph.origin, s * 3 );
				setIdentityAt( graph.delta, s * 16 );
			}
		}
	}
}

/*
================
snapshotSlot

The tick pose (particle-presentation.ts snapshotParticleTick) of slot s
at the start of its latest tick, at snapshot entry entry: the scale,
program rotation and element frame its records continue.
================
*/
function snapshotSlot( graph: ParticleGraphState, s: number, entry: number ): void {
	copy3( graph.scale, s * 3, graph.previousScale, entry * 3 );
	copy16( graph.rotation, s * 16, graph.previousRotation, entry * 16 );
	copy16( graph.matrix, s * 16, graph.previousMatrix, entry * 16 );
	graph.flags[s]! |= SLOT_SNAPSHOT;
}

/*
================
releaseSlots

Release identity only after the last descendant lets it go.
================
*/
function releaseSlots( graph: ParticleGraphState ): void {
	const plan = graph.plan, flags = graph.flags;
	for ( let n = 0; n < plan.graph.length; n++ ) {
		const base = plan.base[n]!, count = graph.counts[n]!;
		for ( let b = 0; b < count; b++ ) {
			const s = base + b, f = flags[s]!;
			if ( (f & (SLOT_OCCUPIED | SLOT_ALIVE | SLOT_RELEASED)) === SLOT_OCCUPIED && !graph.children[s] ) {
				release( graph, s, n );
			}
		}
	}
}

/*
================
emitGroups

Emitter n's emission on tick: each live group (its parent emitter's
unreleased elements in birth order, or the holder's one group) emits by
its accumulator, and each birth reserves the first released slot, else the
next new one, before any is born.
================
*/
function emitGroups( graph: ParticleGraphState, n: number, tick: number ): void {
	const plan = graph.plan, def = plan.graph[n]!, up = def.parent, base = plan.base[n]!;
	const groups = up < 0 ? 1 : graph.orderCounts[up]!, first = up < 0 ? 0 : plan.orders[up]!;
	for ( let k = 0; k < groups; k++ ) {
		const parent = up < 0 ? ROOT_SLOT : graph.orders[first + k]!;
		if ( !(graph.flags[parent]! & SLOT_ALIVE) ) continue;
		const entry = groupEntry( graph, n, parent );
		const emitted = particleEmission( def.emission!, graph.groupTotals[entry]!, graph.age[parent]! );
		graph.groupTotals[entry] = emitted.total;
		for ( let count = 0; count < emitted.emitted; count++ ) {
			const used = graph.counts[n]!;
			let slot = used;
			for ( let b = 0; b < used; b++ ) {
				if ( graph.flags[base + b]! & SLOT_RELEASED ) {
					slot = b;
					break;
				}
			}
			if ( slot >= plan.slots[n]! ) throw Error( "Particle live capacity exceeded" );
			const s = base + slot;
			// A released parent held by a fixed-birth child (born from it after
			// its release) would outlive its slot; the catalogue has none.
			if ( graph.children[s] ) throw Error( "Particle slot reused while it holds children" );
			// Reserve distinct slots for simultaneous births before evaluation.
			graph.flags[s] = 0;
			graph.birth[s] = tick;
			graph.parent[s] = parent;
			graph.counts[n] = Math.max( used, slot + 1 );
		}
	}
}

/*
================
findSiblings

Each live element of emitter n gets its predecessor of the same parent in
birth order (scratch.siblings): slots are recycled, so serials alone keep
birth order. Emission births occur on the current tick, so earlier
serials cannot have a later birth. scratch.latest is left holding each
parent's latest element, for the births that follow.
================
*/
function findSiblings( graph: ParticleGraphState, n: number ): void {
	const plan = graph.plan, up = plan.graph[n]!.parent, scratch = graph.scratch;
	const parentBase = up < 0 ? 0 : plan.base[up]!, latest = scratch.latest;
	latest.fill( NO_SLOT, 0, up < 0 ? 1 : plan.slots[up]! );
	const first = plan.orders[n]!, count = graph.orderCounts[n]!;
	for ( let k = 0; k < count; k++ ) {
		const s = graph.orders[first + k]!;
		if ( !(graph.flags[s]! & SLOT_ALIVE) ) continue;
		const at = up < 0 ? 0 : graph.parent[s]! - parentBase;
		scratch.siblings[s] = latest[at]!;
		latest[at] = s;
	}
}

/*
================
bornSlot

Slot s, emitter n's slot b, is born on tick from slot parent (sibling its
predecessor, or NO_SLOT): its birth sample at the parent, the parent's
frame and position, and the velocity it inherits.
================
*/
function bornSlot(
	graph: ParticleGraphState,
	n: number,
	b: number,
	parent: number,
	sibling: number,
	table: Float32Array
): void {
	const plan = graph.plan, def = plan.graph[n]!, scratch = graph.scratch, sample = scratch.sample;
	const s = plan.base[n]! + b, at = s * 3, from = parent * 3;
	const position = graph.position, velocity = graph.velocity, matrix = graph.matrix;
	const program: ParticleProgram = def.commands ? NO_PROGRAM : def.program ?? NO_PROGRAM;
	copy16( matrix, parent * 16, scratch.basis, 0 );
	graph.index = sampleParticle( program, table, graph.index, scratch.basis, sample );
	const inherited = scratch.inherited;
	copy3( velocity, from, inherited, 0 );
	for ( let i = 0; i < 3; i++ ) {
		position[at + i] = Math.fround( (sample.position as Float64Array)[i]! + position[from + i]! );
	}
	copy3( position, at, graph.previousPosition, at );
	copy3( sample.velocity as Float64Array, 0, velocity, at );
	copy3( sample.scale as Float64Array, 0, graph.scale, at );
	if ( program.shape ) graph.rotation.set( program.shape, s * 16 );
	else setIdentityAt( graph.rotation, s * 16 );
	graph.flags[s] = SLOT_OCCUPIED | SLOT_ALIVE;
	// A fixed birth's slot comes into use when it is born.
	graph.counts[n] = Math.max( graph.counts[n]!, b + 1 );
	graph.link[s] = 0;
	graph.serials[s] = ++graph.serial;
	graph.clockBirth[s] = graph.birth[s]!;
	graph.age[s] = 0;
	graph.frames[s] = 0;
	graph.children[s] = 0;
	graph.parent[s] = parent;
	copy16( matrix, parent * 16, matrix, s * 16 );
	setIdentityAt( graph.delta, s * 16 );
	copy3( position, from, graph.origin, at );
	setIdentityAt( graph.local, s * 16 );
	setIdentityAt( graph.shape, s * 16 );
	if ( plan.sibling[n] === 1 ) scratch.latest[def.parent < 0 ? 0 : parent - plan.base[def.parent]!] = s;
	graph.children[parent]!++;
	const children = plan.emittingChildren[n]!;
	for ( let c = 0; c < children.length; c++ ) graph.groupTotals[plan.groups[children[c]!]! + b] = 0;
	const order = plan.orders[n]!;
	if ( order >= 0 ) graph.orders[order + graph.orderCounts[n]!++] = s;
	if ( def.matrixDepth < 0 ) {
		// The frame keeps only its translation.
		for ( let i = 0; i < 12; i++ ) matrix[s * 16 + i] = i % 5 === 0 ? 1 : 0;
		matrix[s * 16 + 15] = 1;
	}
	if ( def.matrix && !def.commands ) matrix.set( def.matrix, s * 16 );
	const follow = ancestor( graph, s, def.followDepth );
	for ( let i = 0; i < 3; i++ ) {
		inherited[i] = Math.fround( inherited[i]! - (follow >= 0 ? velocity[follow * 3 + i]! : 0) );
		position[at + i] = Math.fround( position[at + i]! + inherited[i]! );
		if ( def.commands || !def.program?.cone ) velocity[at + i] = Math.fround( velocity[at + i]! + inherited[i]! );
	}
	if ( !def.commands && def.program?.vectors ) {
		vectors( graph, s, def.program.vectors, 0, sibling, plan.vectorFrames[n] );
	}
}

/*
================
liveSlot

The per-frame steps of slot s, emitter n's slot b, while it lives (age
below its lifetime), born from slot parent with predecessor sibling: its
holder links, commands, update program, keyframes and frame turn.
================
*/
function liveSlot(
	graph: ParticleGraphState,
	n: number,
	s: number,
	parent: number,
	sibling: number,
	table: Float32Array
): void {
	const plan = graph.plan, def = plan.graph[n]!, age = graph.age[s]!;
	// Fixed per emitter (particle-graph-plan.ts rootLink).
	graph.link[s] = plan.rootLink[n]!;
	commands( graph, s, n, age, table, sibling );
	const program = def.commands ? undefined : def.program;
	if ( program?.rVelocity && onFrame( plan.rotationFrames[n]!, age ) ) graph.local.set( program.rVelocity, s * 16 );
	if ( program?.spin && onFrame( plan.spinFrames[n]!, age ) ) graph.shape.set( program.spin, s * 16 );
	if ( program?.attraction !== undefined && onFrame( plan.attractionFrames[n]!, age ) ) {
		attract( graph, s, parent, program.attraction );
	}
	const at = Math.min( age, def.scales.length - 1 );
	if ( at >= 0 ) setVector( def.scales[at]!, graph.scale, s * 3 );
	const pos = !def.commands && def.positions[Math.min( age, def.positions.length - 1 )];
	if ( pos ) {
		const position = graph.position, to = s * 3, from = parent * 3;
		affineInto( graph.matrix, parent * 16, pos[0]!, pos[1]!, pos[2]!, position, to );
		for ( let i = 0; i < 3; i++ ) position[to + i] = Math.fround( position[to + i]! + position[from + i]! );
	}
	const rot = !def.commands && def.rotations[Math.min( age, def.rotations.length - 1 )];
	if ( rot ) graph.matrix.set( rot, s * 16 );
}

/*
================
keptDelta

The frame's turn this tick of slot s, whose frame was scratch.previous at
the tick's start.
================
*/
function keptDelta( graph: ParticleGraphState, s: number ): void {
	const scratch = graph.scratch;
	inverseInto( scratch.previous, scratch.inverse );
	multiplyAt( graph.matrix, s * 16, scratch.inverse, 0, graph.delta, s * 16 );
}

/*
================
tickEmitter

Emitter n on tick, after retirement and release: its emission, then every
slot with a birth in slot order - births, live updates, and the links of
expired elements that still hold children.
================
*/
function tickEmitter(
	graph: ParticleGraphState,
	n: number,
	tick: number,
	start: number,
	stop: number,
	table: Float32Array
): void {
	const plan = graph.plan, def = plan.graph[n]!, base = plan.base[n]!, scratch = graph.scratch;
	const flags = graph.flags, indexedSiblings = plan.sibling[n] === 1;
	if ( def.emission && (start + tick) / TICKS_PER_SECOND < stop ) emitGroups( graph, n, tick );
	if ( indexedSiblings ) findSiblings( graph, n );
	const births = def.emission ? graph.counts[n]! : def.births.length;
	const up = def.parent, upBase = up < 0 ? 0 : plan.base[up]!, upCount = up < 0 ? 0 : graph.counts[up]!;
	for ( let b = 0; b < births; b++ ) {
		const s = base + b, birth = graph.birth[s]!, elapsed = tick - birth;
		const existing = (flags[s]! & SLOT_OCCUPIED) !== 0;
		const age = existing ? graph.age[s]! : def.loop && elapsed >= 0 ? elapsed % def.frames : elapsed;
		if ( age < 0 || (!existing && (start + birth) / TICKS_PER_SECOND >= stop) ) continue;
		let parent: number;
		if ( def.emission ) parent = graph.parent[s]!;
		else if ( up < 0 ) parent = ROOT_SLOT;
		else {
			const at = def.parents[b]!;
			parent = at >= 0 && at < upCount && flags[upBase + at]! & SLOT_OCCUPIED ? upBase + at : NO_SLOT;
		}
		if ( parent < 0 ) continue;
		const keptMatrix = def.keepMatrix && existing;
		if ( keptMatrix ) copy16( graph.matrix, s * 16, scratch.previous, 0 );
		let sibling = NO_SLOT;
		if ( def.emission ) {
			if ( indexedSiblings ) {
				sibling = !existing ?
					scratch.latest[up < 0 ? 0 : parent - upBase]! :
					flags[s]! & SLOT_ALIVE ?
					scratch.siblings[s]! :
					NO_SLOT;
			}
		} else if ( b > 0 && def.parents[b - 1] === def.parents[b] && flags[s - 1]! & SLOT_OCCUPIED ) sibling = s - 1;
		if ( !existing ) {
			bornSlot( graph, n, b, parent, sibling, table );
		} else if ( age < def.frames ) {
			linkElement( graph, s, def, true );
			advanceMotion( graph, s, plan.motion[n]!, table );
			if ( def.shapeMotion ) {
				multiplyAt( graph.rotation, s * 16, graph.shape, s * 16, scratch.matrix, 0 );
				graph.rotation.set( scratch.matrix, s * 16 );
			}
			if ( !def.commands && def.program?.vectors ) {
				vectors( graph, s, def.program.vectors, age, sibling, plan.vectorFrames[n] );
			}
		}
		if ( age < def.frames ) {
			liveSlot( graph, n, s, parent, sibling, table );
			if ( keptMatrix ) keptDelta( graph, s );
			graph.frames[s] = age;
			if ( birth === tick ) {
				copy3( graph.position, s * 3, graph.previousPosition, s * 3 );
				const snapshots = plan.snapshots[n]!;
				if ( snapshots >= 0 ) snapshotSlot( graph, s, snapshots + b );
			}
		} else if ( graph.children[s]! > 0 ) {
			// AF2A40 retires an expired element from its effect's tick lists but
			// leaves it on the matrix list (+0xB0) that AF13D0 walks until its
			// last child is released (AF2AC0): its links still carry it, and its
			// children follow it, at the holder.
			linkElement( graph, s, def, false );
			if ( keptMatrix ) keptDelta( graph, s );
			else setIdentityAt( graph.delta, s * 16 );
		} else {
			copy3( graph.position, s * 3, graph.origin, s * 3 );
			setIdentityAt( graph.delta, s * 16 );
		}
	}
}

/*
================
writeTickRecords

The records and ribbon tick frames of every drawn emitter's slots, from
the graph's state after a tick or a region shift (particle-records.ts);
an element that rides its holder records its links.
================
*/
function writeTickRecords( graph: ParticleGraphState ): void {
	const plan = graph.plan, flags = graph.flags;
	for ( let n = 0; n < plan.graph.length; n++ ) {
		if ( !plan.draws[n] ) continue;
		const base = plan.base[n]!, count = graph.counts[n]!;
		const snapshots = plan.snapshots[n]!, records = plan.records[n]!, ribbons = plan.ribbons[n]!;
		for ( let b = 0; b < count; b++ ) {
			const s = base + b, f = flags[s]!;
			if ( !(f & SLOT_ALIVE) ) {
				if ( records >= 0 ) clearRecord( graph.records, records + b );
				if ( ribbons >= 0 ) hideRibbonTick( graph.ribbonTicks, ribbons + b );
				continue;
			}
			const snapshot = f & SLOT_SNAPSHOT ? snapshots + b : -1, link = graph.link[s]!;
			if ( records >= 0 ) writeGraphRecord( graph.records, records + b, graph, s, snapshot, link );
			if ( ribbons >= 0 ) writeRibbonTick( graph.ribbonTicks, ribbons + b, graph, s, snapshot, link );
		}
	}
	graph.revision++;
}

/*
================
advanceParticleGraph

time and stop are actor seconds. A fresh history fixes its clock's start
on its first call (graphStartTick); every later call reads time against it.
================
*/
export function advanceParticleGraph(
	history: ParticleGraphState,
	graph: readonly ParticleEmitter[],
	time: number,
	transform: Float32Array,
	table: Float32Array,
	stop = Infinity,
	shiftX = 0,
	shiftZ = 0,
	keepRoots = false
): void {
	const actorTick = Math.floor( time * TICKS_PER_SECOND + 1e-6 );
	if ( history.frame < 0 && Number.isSafeInteger( actorTick ) ) {
		history.startTick = graphStartTick( graph, actorTick, keepRoots );
	}
	const start = history.startTick, plan = history.plan;
	if ( plan.graph !== graph ) throw Error( "Particle graph plan of another graph" );
	const target = Math.min( actorTick - start, plan.horizon );
	if ( target < history.frame || !Number.isSafeInteger( target ) ) throw Error( "Invalid particle graph time" );
	// Only a region crossing shifts the origin. Every other call walked every
	// retained element slot to add zero: a skill trace showed it as the
	// largest single cost of its effects (0.8 ms of every frame).
	const shifted = shiftX !== 0 || shiftZ !== 0;
	if ( shifted ) shiftSlots( history, shiftX, shiftZ );
	// The holder basis advances only with a tick: linked elements take the
	// holder's turn from the root's delta on the next tick, so a call that
	// runs no tick (render frames outnumber 20 Hz ticks) must not consume it,
	// or the turn is lost and they keep their birth frame (an arrow's markers
	// stayed pointing where the hand held it at READY).
	if ( target === history.frame ) {
		if ( shifted ) writeTickRecords( history );
		return;
	}
	const scratch = history.scratch, matrix = history.matrix, position = history.position;
	const origin = history.origin, oldRoot = scratch.oldRoot;
	copy3( position, ROOT_SLOT * 3, oldRoot, 0 );
	copy16( matrix, ROOT_SLOT * 16, scratch.previous, 0 );
	matrix.set( transform, ROOT_SLOT * 16 );
	matrix[ROOT_SLOT * 16 + 12] = matrix[ROOT_SLOT * 16 + 13] = matrix[ROOT_SLOT * 16 + 14] = 0;
	inverseInto( scratch.previous, scratch.inverse );
	multiplyAt( matrix, ROOT_SLOT * 16, scratch.inverse, 0, history.delta, ROOT_SLOT * 16 );
	for ( let tick = history.frame + 1; tick <= target; tick++ ) {
		if ( history.frame < 0 ) copyTranslation( transform, origin, ROOT_SLOT * 3 );
		else copy3( oldRoot, 0, origin, ROOT_SLOT * 3 );
		copyTranslation( transform, position, ROOT_SLOT * 3 );
		if ( tick > history.frame + 1 ) {
			copy3( position, ROOT_SLOT * 3, origin, ROOT_SLOT * 3 );
			setIdentityAt( history.delta, ROOT_SLOT * 16 );
		}
		history.age[ROOT_SLOT] = tick;
		retireSlots( history, tick, start, stop, keepRoots );
		releaseSlots( history );
		for ( let n = 0; n < graph.length; n++ ) tickEmitter( history, n, tick, start, stop, table );
	}
	history.frame = target;
	writeTickRecords( history );
}
