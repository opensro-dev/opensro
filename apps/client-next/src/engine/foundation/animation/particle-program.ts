/*
===========================================================================

particle-program.ts - BSR particle programs: decode, birth and tick

A model particle runs its program on whole 50 ms frames (A2E980): birth
samples the CRT random table, then each frame applies velocity, spin,
angular velocity and the random scale graph. Presentation between ticks
lives in particle-presentation.ts.

===========================================================================
*/

import { crtRandomRange } from "@/engine/foundation/math/crt-random";
import { identity } from "@/engine/foundation/rendering/world-math";
import { multiply } from "@/engine/foundation/math/pose-math";
import { particleCommandFrames } from "@/engine/foundation/animation/particle-command-frames";
import { particleRotation, particleCone } from "@/engine/foundation/animation/particle-rotation";
import { hypot3 } from "@/engine/foundation/math/hypot";
/*
SetSpherePos flags (AF4410 opcode 0x37 + flag). 0x37 adds to the element's
own position; the original samples into the position and then adds the
radius times an uninitialised stack temp (the vector 0x1f fills), which no
other op of the element writes. Ported as the evident intent, own position
plus the scaled sample, like the relative cone position 0x1f.
*/
export const SPHERE_POS_SELF = 0;
export const SPHERE_POS_PARENT = 1;
export const SPHERE_POS_SIBLING = 2;
export interface ParticleVectorCommand {
	readonly name: "SetPosition" | "SetVelocity" | "Force";
	readonly flags: number;
	readonly value: readonly number[];
	readonly frames: readonly number[];
}
export interface ParticleProgram {
	readonly vectors?: readonly ParticleVectorCommand[];
	readonly spinFrames?: readonly number[];
	readonly rotationFrames?: readonly number[];
	readonly order?: readonly string[];
	readonly scaleFrames?: readonly number[];
	readonly sphere?: readonly number[];
	readonly sphereFlags?: number;
	readonly cone?: readonly number[];
	readonly coneFlags?: number;
	readonly conePos?: readonly number[];
	readonly conePosFlags?: number;
	readonly coneForce?: readonly number[];
	readonly coneForceFlags?: number;
	readonly rVelocity?: readonly number[];
	readonly attraction?: number;
	readonly attractionFrames?: readonly number[];
	readonly scale?: readonly { readonly time: number; readonly value: readonly number[]; }[];
	readonly spin?: readonly number[];
	readonly shape?: readonly number[];
}
// previous: the state at the start of the latest tick, for presentation only
// (particle-presentation.ts); simulation never reads it.
export interface ParticleInstance {
	position: number[];
	velocity: number[];
	scale: number[];
	rotation: Float32Array;
	frame: number;
	angularVelocity?: Float32Array;
	previous?: import("@/engine/foundation/animation/particle-presentation").ParticleTickPose;
}
export interface ParticleAdvanceContext {
	readonly origin?: ArrayLike<number>;
	readonly transform?: ArrayLike<number>;
	readonly parentTransform?: ArrayLike<number>;
	readonly parentPosition?: ArrayLike<number>;
	readonly siblingTransform?: ArrayLike<number>;
	readonly siblingPosition?: ArrayLike<number>;
}
/*
================
particleRandomTable
================
*/
// CEFSystem AFB6A0 precomputes 5000 CRT samples; AF27E0 advances before reading.
export function particleRandomTable( seed = 1 ) {
	const values = new Float32Array( 5000 );
	for ( let i = 0; i < values.length; i++ ) {
		const r = crtRandomRange( seed, 0, 32768 );
		seed = r.state;
		values[i] = r.value / 32767;
	}
	return values;
}
/*
================
particleProgram
================
*/
export function particleProgram(
	value: readonly {
		name: string;
		flags?: number;
		byte1?: number;
		start?: number;
		spacing?: number;
		end?: number;
		parameter?: { kind: string; value?: unknown; left?: unknown; right?: unknown; };
	}[],
	parameters: readonly { kind: string; value: unknown; }[] = [],
	totalFrames?: number
): ParticleProgram | undefined {
	const result: {
		spinFrames?: number[];
		rotationFrames?: number[];
		vectors?: ParticleVectorCommand[];
		order?: string[];
		scaleFrames?: number[];
		sphere?: number[];
		sphereFlags?: number;
		cone?: number[];
		coneFlags?: number;
		conePos?: number[];
		conePosFlags?: number;
		coneForce?: number[];
		coneForceFlags?: number;
		rVelocity?: number[];
		attraction?: number;
		attractionFrames?: number[];
		scale?: { time: number; value: number[]; }[];
		spin?: number[];
		shape?: number[];
	} = {};
	/*
	================
	vector
	================
	*/
	function vector( v: unknown, n: number ): number[] {
		if ( !Array.isArray( v ) || v.length !== n || v.some( x => typeof x !== "number" || !Number.isFinite( x ) ) ) {
			throw Error( "Invalid particle program parameter" );
		}
		return [ ...v ];
	}
	for ( const op of value ) {
		if ( op.name === "SetPosition" || op.name === "SetVelocity" || op.name === "Force" ) {
			const flags = op.flags ?? 0;
			if ( !Number.isInteger( flags ) || flags < 0 || flags > (op.name === "SetPosition" ? 7 : 3) ) {
				throw Error( "Invalid vector command flags" );
			}
			(result.vectors ??= []).push( {
				name: op.name,
				flags,
				value: vector( op.parameter?.value, 3 ),
				frames: totalFrames === undefined ? [ 0 ] : particleCommandFrames( op, totalFrames )
			} );
		}
		if (
			[
				"SetSpherePos",
				"SetConeVel",
				"SetGraphRandomScale",
				"SetShapeRotVel",
				"ConeForce",
				"Attraction",
				"SetRVelocity",
				"SetConePos"
			].includes( op.name )
		) (result.order ??= []).push( op.name );
		if ( op.name === "SetSpherePos" ) {
			// AF4410 opcodes 0x37..0x39: the flag picks the base the unit-ball
			// sample lands on - the element itself, its parent, or its sibling.
			const flags = op.flags ?? 0;
			if ( !Number.isInteger( flags ) || flags < 0 || flags > SPHERE_POS_SIBLING ) {
				throw Error( "Invalid sphere position flags" );
			}
			result.sphere = vector( op.parameter?.value, 3 );
			result.sphereFlags = flags;
		} else if ( op.name === "SetConeVel" ) {
			if ( !Number.isInteger( op.flags ) || op.flags! < 0 || op.flags! > 3 ) {
				throw Error( "Unsupported relative cone velocity" );
			}
			result.cone = particleCone( op.parameter );
			result.coneFlags = op.flags ?? 0;
		} else if ( op.name === "ConeForce" ) {
			if ( !Number.isInteger( op.flags ) || op.flags! < 0 || op.flags! > 3 ) {
				throw Error( "Unsupported relative cone velocity" );
			}
			result.coneForce = particleCone( op.parameter );
			result.coneForceFlags = op.flags ?? 0;
		} else if ( op.name === "SetConePos" ) {
			if ( op.flags !== 0 && op.flags !== 2 && op.flags !== 7 ) throw Error( "Unsupported cone position flags" );
			result.conePos = particleCone( op.parameter );
			result.conePosFlags = op.flags ?? 0;
		} else if ( op.name === "Attraction" ) {
			if ( typeof op.parameter?.value === "number" && Number.isFinite( op.parameter.value ) ) {
				result.attraction = op.parameter.value;
				if ( totalFrames !== undefined ) result.attractionFrames = particleCommandFrames( op, totalFrames );
			}
		} else if ( op.name === "SetRVelocity" ) {
			result.rVelocity = particleRotation( op.parameter );
			if ( totalFrames !== undefined ) result.rotationFrames = particleCommandFrames( op, totalFrames );
		} else if ( op.name === "SetShapeRot" ) {
			(result.order ??= []).push( op.name );
			result.shape = particleRotation( op.parameter );
		} else if ( op.name === "SetShapeRotVel" ) {
			result.spin = particleRotation( op.parameter );
			if ( totalFrames !== undefined ) result.spinFrames = particleCommandFrames( op, totalFrames );
		} else if ( op.name === "SetGraphRandomScale" ) {
			result.scaleFrames = op.end === 1 ? [ 0, 1 ] : [ 0 ];
			const graphs = parameters.filter( p => p.kind === "BlendScaleGraph" );
			if ( graphs.length !== 1 ) throw Error( "Ambiguous random scale graph" );
			const graph = graphs[0]!.value as { points?: { time: number; value: unknown; }[]; };
			if ( !Array.isArray( graph?.points ) || !graph.points.length || graph.points.length > 4096 ) {
				throw Error( "Invalid random scale graph" );
			}
			result.scale = graph.points.map( ( p, i ) => {
				if (
					!Number.isFinite( p.time ) || p.time < 0 || p.time > 1 ||
					i > 0 && p.time <= graph.points![i - 1]!.time
				) throw Error( "Invalid random scale knot" );
				return { time: p.time, value: vector( p.value, 3 ) };
			} );
		}
	}
	return Object.keys( result ).length ? result : undefined;
}
/*
================
sampleScaleInto

The random scale graph at at, into out[outAt..outAt+2].
================
*/
export function sampleScaleInto( program: ParticleProgram, at: number, out: ParticleVector, outAt: number ): void {
	const points = program.scale!;
	let right = points.length - 1;
	for ( let i = 0; i < points.length; i++ ) {
		if ( points[i]!.time >= at ) {
			right = i;
			break;
		}
	}
	const a = points[Math.max( 0, right - 1 )]!,
		b = points[right]!,
		t = a === b ? 0 : Math.max( 0, Math.min( 1, (at - a.time) / (b.time - a.time) ) );
	for ( let i = 0; i < 3; i++ ) out[outAt + i] = Math.fround( a.value[i]! * (1 - t) + b.value[i]! * t );
}
/*
================
sampleScale
================
*/
function sampleScale( program: ParticleProgram, at: number ): number[] {
	const scale = [ 0, 0, 0 ];
	sampleScaleInto( program, at, scale, 0 );
	return scale;
}
// The birth steps a program without an order of its own runs.
const DEFAULT_ORDER = [ "SetSpherePos", "SetGraphRandomScale", "SetConeVel" ] as const;
/*
================
ParticleVector

Where a sample writes a vector: a plain array or flat slot storage.
================
*/
export type ParticleVector = { [index: number]: number; };
/*
================
ParticleSample

The vectors sampleParticle writes: position, velocity and scale at
position[at..at+2] and so on; work is the caller's three-value scratch.
================
*/
export interface ParticleSample {
	readonly position: ParticleVector;
	readonly velocity: ParticleVector;
	readonly scale: ParticleVector;
	readonly at: number;
	readonly work: ParticleVector;
}
/*
================
coneVector

A cone sample (SetConeVel, ConeForce, SetConePos): speed or distance from
cone[0..1], polar angle up to cone[2], a full azimuth; three draws. With
transform, out is the sample turned by its 3x3, plus its translation when
translate. Writes out[0..2].
================
*/
function coneVector(
	cone: readonly number[],
	table: Float32Array,
	index: number,
	transform: ArrayLike<number> | undefined,
	translate: boolean,
	out: ParticleVector
): number {
	index = (index + 1) % table.length;
	const speed = Math.fround( table[index]! * (cone[1]! - cone[0]!) + cone[0]! );
	index = (index + 1) % table.length;
	const polar = Math.fround( table[index]! * cone[2]! );
	index = (index + 1) % table.length;
	const azimuth = Math.fround( table[index]! * 6.2831854820251465 );
	const x = Math.fround( -Math.sin( polar ) * speed );
	const v0 = Math.fround( x * Math.cos( azimuth ) ),
		v1 = Math.fround( Math.cos( polar ) * speed ),
		v2 = Math.fround( x * Math.sin( azimuth ) );
	if ( !transform ) {
		out[0] = v0;
		out[1] = v1;
		out[2] = v2;
		return index;
	}
	for ( let i = 0; i < 3; i++ ) {
		out[i] = translate ?
			Math.fround( v0 * transform[i]! + v1 * transform[4 + i]! + v2 * transform[8 + i]! + transform[12 + i]! ) :
			Math.fround( v0 * transform[i]! + v1 * transform[4 + i]! + v2 * transform[8 + i]! );
	}
	return index;
}
/*
================
sampleParticle

A program's birth sample (CRT draws from table after index) into out, and
the cursor after it. Allocates nothing, so the particle graph's tick can
sample births and random-sample commands into its own storage.
================
*/
export function sampleParticle(
	program: ParticleProgram,
	table: Float32Array,
	index: number,
	transform: ArrayLike<number> | undefined,
	out: ParticleSample
): number {
	const position = out.position, velocity = out.velocity, scale = out.scale, at = out.at, cone = out.work;
	for ( let i = 0; i < 3; i++ ) {
		position[at + i] = 0;
		velocity[at + i] = 0;
		scale[at + i] = 1;
	}
	const order = program.order ?? DEFAULT_ORDER;
	for ( let o = 0; o < order.length; o++ ) {
		const op = order[o]!;
		// The sibling base (0x39) is skipped without a sibling, and a birth
		// sampled here has none; particle-graph.ts supplies one when it exists.
		if ( op === "SetSpherePos" && program.sphere && program.sphereFlags !== SPHERE_POS_SIBLING ) {
			let attempts = 0, length = 0;
			do {
				for ( let i = 0; i < 3; i++ ) {
					index = (index + 1) % table.length;
					position[at + i] = Math.fround( (table[index]! - 0.5) * 2 );
				}
				if ( ++attempts > 5000 ) throw Error( "Invalid particle random stream" );
				const x = position[at]!, y = position[at + 1]!, z = position[at + 2]!;
				length = 0 + x * x + y * y + z * z;
			} while ( length > 1 );
			for ( let i = 0; i < 3; i++ ) position[at + i] = Math.fround( position[at + i]! * program.sphere[i]! );
		}
		if ( op === "SetGraphRandomScale" && program.scale ) {
			index = (index + 1) % table.length;
			sampleScaleInto( program, table[index]!, scale, at );
		}
		if ( op === "SetConeVel" && program.cone ) {
			index = coneVector(
				program.cone,
				table,
				index,
				program.coneFlags === 2 ? transform : undefined,
				true,
				cone
			);
			for ( let i = 0; i < 3; i++ ) velocity[at + i] = cone[i]!;
		}
		if ( op === "ConeForce" && program.coneForce ) {
			index = coneVector(
				program.coneForce,
				table,
				index,
				program.coneForceFlags === 2 ? transform : undefined,
				true,
				cone
			);
			for ( let i = 0; i < 3; i++ ) velocity[at + i] = Math.fround( velocity[at + i]! + cone[i]! );
		}
		if ( op === "SetConePos" && program.conePos ) {
			const turned = program.conePosFlags === 2 || program.conePosFlags === 7;
			index = coneVector( program.conePos, table, index, turned ? transform : undefined, false, cone );
			if ( program.conePosFlags === 7 && transform ) {
				for ( let i = 0; i < 3; i++ ) cone[i] = Math.fround( cone[i]! + transform[12 + i]! );
			}
			for ( let i = 0; i < 3; i++ ) position[at + i] = Math.fround( position[at + i]! + cone[i]! );
		}
	}
	return index;
}
/*
================
initializeParticle
================
*/
export function initializeParticle(
	program: ParticleProgram,
	table: Float32Array,
	index: number,
	transform?: ArrayLike<number>
) {
	const position = [ 0, 0, 0 ], velocity = [ 0, 0, 0 ], scale = [ 1, 1, 1 ];
	index = sampleParticle( program, table, index, transform, { position, velocity, scale, at: 0, work: [ 0, 0, 0 ] } );
	const angularVelocity = program.rVelocity ? Float32Array.from( program.rVelocity ) : undefined;
	return {
		index,
		state: {
			position,
			velocity,
			scale,
			rotation: program.shape ? Float32Array.from( program.shape ) : identity(),
			frame: 0,
			angularVelocity
		} as ParticleInstance
	};
}
/*
================
advanceParticle
================
*/
// Position and shape rotation are element state, separate from billboard view.
export function advanceParticle(
	state: ParticleInstance,
	program: ParticleProgram,
	frame: number,
	table?: Float32Array,
	index = 0,
	context?: ParticleAdvanceContext
) {
	if ( !Number.isSafeInteger( frame ) || frame < state.frame || frame > 1200 ) {
		throw Error( "Invalid particle frame" );
	}
	// Scratch only for a spinning particle: the graph strips spin, and most
	// elements have no angular velocity, so most calls allocate nothing.
	const spin = program.spin ? Float32Array.from( program.spin ) : null,
		scratch = spin || state.angularVelocity ? new Float32Array( 16 ) : null;
	for ( let n = state.frame; n < frame; n++ ) {
		if (
			program.attraction !== undefined &&
			(program.attractionFrames === undefined || program.attractionFrames.includes( n ))
		) {
			const ref = context?.origin ?? [ 0, 0, 0 ];
			const dx = ref[0]! - state.position[0]!,
				dy = ref[1]! - state.position[1]!,
				dz = ref[2]! - state.position[2]!;
			const dist = hypot3( dx, dy, dz );
			if ( dist > 1e-6 ) {
				const k = program.attraction / dist;
				state.velocity[0] = Math.fround( state.velocity[0]! + dx * k );
				state.velocity[1] = Math.fround( state.velocity[1]! + dy * k );
				state.velocity[2] = Math.fround( state.velocity[2]! + dz * k );
			}
		}
		for ( let axis = 0; axis < 3; axis++ ) {
			state.position[axis] = Math.fround( state.position[axis]! + state.velocity[axis]! );
		}
		if ( spin ) {
			multiply( state.rotation, spin, scratch! );
			state.rotation.set( scratch! );
		}
		if ( state.angularVelocity ) {
			multiply( state.rotation, state.angularVelocity, scratch! );
			state.rotation.set( scratch! );
		}
		if ( program.scale && program.scaleFrames?.includes( n + 1 ) && table ) {
			index = (index + 1) % table.length;
			state.scale = sampleScale( program, table[index]! );
		}
	}
	state.frame = frame;
	return index;
}
