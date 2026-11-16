/*
===========================================================================

particle-graph-commands.ts - what one EFP element's commands do on a tick

AF4410 runs an element's scheduled operations: rotations and BAN frames,
positions in the twelve origin and basis modes, velocity and force
vectors, attraction, and the random-sample commands that draw a cone or
sphere sample from the shared table. The particle graph tick
(particle-graph.ts) decides when an element's commands fire; this module
is what they do to the element's slot (particle-graph-slots.ts), with the
native float32 stores and evaluation order, allocating nothing.

===========================================================================
*/

import { multiply } from "@/engine/foundation/math/pose-math";
import {
	sampleParticle,
	SPHERE_POS_PARENT,
	SPHERE_POS_SELF,
	SPHERE_POS_SIBLING,
	type ParticleVectorCommand
} from "@/engine/foundation/animation/particle-program";
import { hypot3 } from "@/engine/foundation/math/hypot";
import { onFrame } from "@/engine/foundation/animation/particle-graph-plan";
import { NO_SLOT, type ParticleGraphState } from "@/engine/foundation/animation/particle-graph-slots";

/*
================
affineInto

out[outAt..] = m[mAt..] * (x, y, z, 1), each component stored as float32.
out may hold the source vector: all three inputs are read first.
================
*/
export function affineInto(
	m: Float32Array,
	mAt: number,
	x: number,
	y: number,
	z: number,
	out: Float64Array,
	outAt: number
): void {
	const a = Math.fround( m[mAt]! * x + m[mAt + 4]! * y + m[mAt + 8]! * z + m[mAt + 12]! ),
		b = Math.fround( m[mAt + 1]! * x + m[mAt + 5]! * y + m[mAt + 9]! * z + m[mAt + 13]! ),
		c = Math.fround( m[mAt + 2]! * x + m[mAt + 6]! * y + m[mAt + 10]! * z + m[mAt + 14]! );
	out[outAt] = a;
	out[outAt + 1] = b;
	out[outAt + 2] = c;
}

/*
================
copy3

A slot vector to another. The tick reads typed arrays and authored plain
arrays apart (setVector; particle-graph.ts copyTranslation): V8 keeps one
record of the array kinds a function reads, and the tick's inner functions
are inlined where that record decides their speed.
================
*/
export function copy3( from: Float64Array, fromAt: number, to: Float64Array, toAt: number ): void {
	to[toAt] = from[fromAt]!;
	to[toAt + 1] = from[fromAt + 1]!;
	to[toAt + 2] = from[fromAt + 2]!;
}

/*
================
setVector

An authored vector (a command's or keyframe's value) into to[toAt..].
================
*/
export function setVector( value: readonly number[], to: Float64Array, toAt: number ): void {
	to[toAt] = value[0]!;
	to[toAt + 1] = value[1]!;
	to[toAt + 2] = value[2]!;
}

/*
================
copy16
================
*/
export function copy16( from: Float32Array, fromAt: number, to: Float32Array, toAt: number ): void {
	for ( let i = 0; i < 16; i++ ) to[toAt + i] = from[fromAt + i]!;
}

/*
================
attract

AF4410/4A: velocity += (element.position - source.position) * signed
strength / distance.
================
*/
export function attract( graph: ParticleGraphState, s: number, source: number, strength: number ): void {
	const position = graph.position, velocity = graph.velocity, at = s * 3, from = source * 3;
	const d0 = position[at]! - position[from]!,
		d1 = position[at + 1]! - position[from + 1]!,
		d2 = position[at + 2]! - position[from + 2]!;
	const length = hypot3( d0, d1, d2 );
	// Not a negated test: a NaN distance skips the pull, as native does.
	if ( !(length > 1e-6) ) return;
	velocity[at] = Math.fround( velocity[at]! + d0 * strength / length );
	velocity[at + 1] = Math.fround( velocity[at + 1]! + d1 * strength / length );
	velocity[at + 2] = Math.fround( velocity[at + 2]! + d2 * strength / length );
}

/*
================
commands

The commands of emitter n that fire on age for slot s (sibling its
predecessor, or NO_SLOT). A random-sample command samples the plan's
program for it (particle-graph-plan.ts samples) and writes the sample into
the slot.
================
*/
export function commands(
	graph: ParticleGraphState,
	s: number,
	n: number,
	age: number,
	table: Float32Array,
	sibling: number
) {
	const plan = graph.plan, list = plan.graph[n]!.commands;
	if ( !list ) return;
	const frames = plan.commandFrames[n]!, samples = plan.samples[n]!, scratch = graph.scratch;
	const matrix = graph.matrix, position = graph.position, velocity = graph.velocity, parent = graph.parent[s]!;
	for ( let c = 0; c < list.length; c++ ) {
		const command = list[c]!;
		if ( !onFrame( frames[c]!, age ) ) continue;
		const flags = command.flags ?? 0;
		const rotation = command.rotations?.[Math.min( age, command.rotations.length - 1 )];
		if ( rotation ) {
			// AF4581: BAN mode 2 is frame * parent in native row storage.
			// Our transposed storage reverses those operands.
			if ( flags === 3 && sibling < 0 ) continue;
			if ( flags === 0 ) matrix.set( rotation, s * 16 );
			else {
				// The authored rotation is a plain array: multiply, with the
				// basis frame copied out of its slot.
				copy16( matrix, (flags === 1 ? s : flags === 2 ? parent : sibling) * 16, scratch.basis, 0 );
				// AF4559 appends BAN frames; AF448A prepends constant rotations.
				if ( flags === 1 && command.name === "SetBANRot" ) multiply( rotation, scratch.basis, scratch.matrix );
				else multiply( scratch.basis, rotation, scratch.matrix );
				matrix.set( scratch.matrix, s * 16 );
			}
			continue;
		}
		const at = command.positions?.[Math.min( age, command.positions.length - 1 )];
		if ( at ) {
			setPosition( graph, s, at, flags, sibling );
			continue;
		}
		const program = command.program;
		if ( program.vectors ) {
			// A command's vectors all run on the frame the command fires.
			vectors( graph, s, program.vectors, age, sibling );
			continue;
		}
		if ( program.rVelocity ) {
			graph.local.set( program.rVelocity, s * 16 );
			continue;
		}
		if ( program.spin ) {
			graph.shape.set( program.spin, s * 16 );
			continue;
		}
		if ( program.shape ) {
			graph.rotation.set( program.shape, s * 16 );
			continue;
		}
		if ( program.attraction !== undefined ) {
			attract( graph, s, parent, program.attraction );
			continue;
		}
		const coneFlags = program.coneFlags ?? program.coneForceFlags ?? 0;
		if ( coneFlags === 3 && sibling < 0 ) continue;
		// 0x39 runs only with a sibling and draws no random numbers otherwise.
		const sphereFlags = program.sphereFlags ?? SPHERE_POS_PARENT;
		if ( program.sphere && sphereFlags === SPHERE_POS_SIBLING && sibling < 0 ) continue;
		const basis = coneFlags === 1 ? s : coneFlags === 3 ? sibling : parent, sample = scratch.sample;
		copy16( matrix, basis * 16, scratch.basis, 0 );
		graph.index = sampleParticle( samples[c]!, table, graph.index, scratch.basis, sample );
		const to = s * 3, sampled = sample.position as Float64Array, pushed = sample.velocity as Float64Array;
		if ( program.sphere ) {
			const base = (sphereFlags === SPHERE_POS_SELF ? s : sphereFlags === SPHERE_POS_SIBLING ? sibling : parent) *
				3;
			for ( let i = 0; i < 3; i++ ) position[to + i] = Math.fround( sampled[i]! + position[base + i]! );
		}
		if ( program.conePos ) {
			for ( let i = 0; i < 3; i++ ) position[to + i] = Math.fround( sampled[i]! + position[to + i]! );
		}
		if ( program.cone ) copy3( pushed, 0, velocity, to );
		if ( program.coneForce ) {
			for ( let i = 0; i < 3; i++ ) velocity[to + i] = Math.fround( pushed[i]! + velocity[to + i]! );
		}
		if ( program.scale ) copy3( sample.scale as Float64Array, 0, graph.scale, to );
	}
}

/*
================
setPosition

AF4410 modes 0..11 select an additive origin and an optional matrix.
BAN position commands use the same modes as constant positions.
================
*/
function setPosition(
	graph: ParticleGraphState,
	s: number,
	value: readonly number[],
	flag: number,
	sibling: number
): void {
	const originMode = flag % 3;
	if ( (originMode === 2 || flag >= 9) && sibling < 0 ) return;
	const work = graph.scratch.vector, parent = graph.parent[s]!;
	setVector( value, work, 0 );
	if ( flag >= 3 ) {
		const basis = flag < 6 ? s : flag < 9 ? parent : sibling;
		affineInto( graph.matrix, basis * 16, work[0]!, work[1]!, work[2]!, work, 0 );
	}
	const base = originMode === 1 ? parent : originMode === 2 ? sibling : NO_SLOT;
	const position = graph.position, at = s * 3;
	if ( base >= 0 ) copy3( position, base * 3, position, at );
	for ( let i = 0; i < 3; i++ ) position[at + i] = Math.fround( position[at + i]! + work[i]! );
}

/*
================
vectors

Apply SetPosition / SetVelocity / Force operations scheduled on frame by
their frame tables (particle-graph-plan.ts vectorFrames), or every
operation without tables (a command firing).
================
*/
export function vectors(
	graph: ParticleGraphState,
	s: number,
	ops: readonly ParticleVectorCommand[],
	frame: number,
	sibling: number,
	frames?: readonly Uint8Array[]
): void {
	const work = graph.scratch.vector, velocity = graph.velocity, at = s * 3;
	for ( let o = 0; o < ops.length; o++ ) {
		const op = ops[o]!;
		if ( frames && !onFrame( frames[o]!, frame ) ) continue;
		const flag = op.flags;
		setVector( op.value, work, 0 );
		if ( op.name === "SetPosition" ) {
			setPosition( graph, s, op.value, flag, sibling );
		} else {
			if ( flag === 3 && sibling < 0 ) continue;
			if ( flag ) {
				const basis = flag === 1 ? s : flag === 2 ? graph.parent[s]! : sibling;
				affineInto( graph.matrix, basis * 16, work[0]!, work[1]!, work[2]!, work, 0 );
			}
			const force = op.name === "Force";
			for ( let i = 0; i < 3; i++ ) velocity[at + i] = Math.fround( work[i]! + (force ? velocity[at + i]! : 0) );
		}
	}
}
