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
		end?: number;
		step?: number;
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
			if ( op.flags !== 1 ) throw Error( "Unsupported absolute sphere position" );
			result.sphere = vector( op.parameter?.value, 3 );
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
			result.scaleFrames = op.step === 1 ? [ 0, 1 ] : [ 0 ];
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
sampleScale
================
*/
function sampleScale( program: ParticleProgram, at: number ) {
	const points = program.scale!;
	let right = points.findIndex( p => p.time >= at );
	if ( right < 0 ) right = points.length - 1;
	const a = points[Math.max( 0, right - 1 )]!,
		b = points[right]!,
		t = a === b ? 0 : Math.max( 0, Math.min( 1, (at - a.time) / (b.time - a.time) ) );
	return a.value.map( ( n, i ) => Math.fround( n * (1 - t) + b.value[i]! * t ) );
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
	const random = () => {
		index = (index + 1) % table.length;
		return table[index]!;
	};
	const position = [ 0, 0, 0 ], velocity = [ 0, 0, 0 ];
	let scale = [ 1, 1, 1 ];
	for ( const op of program.order ?? [ "SetSpherePos", "SetGraphRandomScale", "SetConeVel" ] ) {
		if ( op === "SetSpherePos" && program.sphere ) {
			let attempts = 0;
			do {
				for ( let i = 0; i < 3; i++ ) position[i] = Math.fround( (random() - 0.5) * 2 );
				if ( ++attempts > 5000 ) throw Error( "Invalid particle random stream" );
			} while ( position.reduce( ( n, v ) => n + v * v, 0 ) > 1 );
			for ( let i = 0; i < 3; i++ ) position[i] = Math.fround( position[i]! * program.sphere[i]! );
		}
		if ( op === "SetGraphRandomScale" && program.scale ) scale = sampleScale( program, random() );
		if ( op === "SetConeVel" && program.cone ) {
			const speed = Math.fround( random() * (program.cone[1]! - program.cone[0]!) + program.cone[0]! ),
				polar = Math.fround( random() * program.cone[2]! ),
				azimuth = Math.fround( random() * 6.2831854820251465 );
			const x = Math.fround( -Math.sin( polar ) * speed );
			let v = [
				Math.fround( x * Math.cos( azimuth ) ),
				Math.fround( Math.cos( polar ) * speed ),
				Math.fround( x * Math.sin( azimuth ) )
			];
			if ( program.coneFlags === 2 && transform ) {
				v = [ 0, 1, 2 ].map( i =>
					Math.fround(
						v[0]! * transform[i]! + v[1]! * transform[4 + i]! + v[2]! * transform[8 + i]! +
							transform[12 + i]!
					)
				);
			}
			velocity[0] = v[0]!;
			velocity[1] = v[1]!;
			velocity[2] = v[2]!;
		}
		if ( op === "ConeForce" && program.coneForce ) {
			const speed = Math.fround(
					random() * (program.coneForce[1]! - program.coneForce[0]!) + program.coneForce[0]!
				),
				polar = Math.fround( random() * program.coneForce[2]! ),
				azimuth = Math.fround( random() * 6.2831854820251465 );
			const x = Math.fround( -Math.sin( polar ) * speed );
			let v = [
				Math.fround( x * Math.cos( azimuth ) ),
				Math.fround( Math.cos( polar ) * speed ),
				Math.fround( x * Math.sin( azimuth ) )
			];
			if ( program.coneForceFlags === 2 && transform ) {
				v = [ 0, 1, 2 ].map( i =>
					Math.fround(
						v[0]! * transform[i]! + v[1]! * transform[4 + i]! + v[2]! * transform[8 + i]! +
							transform[12 + i]!
					)
				);
			}
			velocity[0] = Math.fround( velocity[0]! + v[0]! );
			velocity[1] = Math.fround( velocity[1]! + v[1]! );
			velocity[2] = Math.fround( velocity[2]! + v[2]! );
		}
		if ( op === "SetConePos" && program.conePos ) {
			const dist = Math.fround( random() * (program.conePos[1]! - program.conePos[0]!) + program.conePos[0]! ),
				polar = Math.fround( random() * program.conePos[2]! ),
				azimuth = Math.fround( random() * 6.2831854820251465 );
			const x = Math.fround( -Math.sin( polar ) * dist );
			let p = [
				Math.fround( x * Math.cos( azimuth ) ),
				Math.fround( Math.cos( polar ) * dist ),
				Math.fround( x * Math.sin( azimuth ) )
			];
			if ( (program.conePosFlags === 2 || program.conePosFlags === 7) && transform ) {
				p = [ 0, 1, 2 ].map( i =>
					Math.fround( p[0]! * transform[i]! + p[1]! * transform[4 + i]! + p[2]! * transform[8 + i]! )
				);
			}
			if ( program.conePosFlags === 7 && transform ) {
				p[0] = Math.fround( p[0]! + transform[12]! );
				p[1] = Math.fround( p[1]! + transform[13]! );
				p[2] = Math.fround( p[2]! + transform[14]! );
			}
			position[0] = Math.fround( position[0]! + p[0]! );
			position[1] = Math.fround( position[1]! + p[1]! );
			position[2] = Math.fround( position[2]! + p[2]! );
		}
	}
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
			const dist = Math.hypot( dx, dy, dz );
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
/*
================
placeParticle
================
*/
export function placeParticle( state: ParticleInstance, palette: Float32Array, offset: number, spin = false ) {
	if ( spin ) {
		const result = new Float32Array( 16 );
		multiply( palette.subarray( offset, offset + 16 ), state.rotation, result );
		palette.set( result, offset );
		return;
	}
	for ( let i = 0; i < 3; i++ ) {
		for ( let row = 0; row < 3; row++ ) palette[offset + i * 4 + row]! *= state.scale[i]!;
		palette[offset + 12 + i]! += state.position[i]!;
	}
}
