/*
===========================================================================

particle-graph.ts - the EFP element tree, ticked at the native 20 Hz

One chronological owner updates invisible parents before their child
groups. Retained expired elements supply identity and state to their
descendants, never new draws.

Every value keeps the native float32 stores and evaluation order. The
tick loop runs for every effect on screen, so it writes into each
element's own arrays and the graph's scratch instead of allocating: an
allocation trace showed it producing 300 MB of garbage in 18 s, which the
main thread then paid for in collection pauses.

===========================================================================
*/

import {
	particleEmission,
	type ParticleEmitter as EmissionParameters
} from "@/engine/foundation/animation/particle-emission";
import { identity } from "@/engine/foundation/rendering/world-math";
import { multiply } from "@/engine/foundation/math/pose-math";
import {
	initializeParticle,
	advanceParticle,
	type ParticleInstance,
	type ParticleProgram,
	type ParticleVectorCommand
} from "@/engine/foundation/animation/particle-program";
import { snapshotParticleTick, continueRotation } from "@/engine/foundation/animation/particle-presentation";

export interface ParticleEmitter {
	readonly emission?: EmissionParameters;
	readonly capacity?: number;
	readonly loop?: boolean;
	readonly commands?: readonly {
		readonly name: string;
		readonly frames: readonly number[];
		readonly program: ParticleProgram;
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

interface ParticleGroup {
	readonly parent: ParticleElement;
	total: number;
}

export interface ParticleElement {
	serial: number;
	born: number;
	clockBirth: number;
	age: number;
	alive: boolean;
	children: number;
	released: boolean;
	group?: ParticleGroup;
	readonly previousPosition: number[];
	readonly state: ParticleInstance;
	readonly matrix: Float32Array;
	readonly delta: Float32Array;
	readonly origin: number[];
	readonly local: Float32Array;
	readonly shape: Float32Array;
	readonly parent: ParticleElement;
}

/*
================
ParticleGraphScratch

Per-graph working storage for the tick loop. Owned by the graph state so
no module holds mutable state.
================
*/
interface ParticleGraphScratch {
	readonly matrix: Float32Array;
	readonly inverse: Float32Array;
	readonly previous: Float32Array;
	readonly oldRoot: number[];
	readonly vector: number[];
	// The update program per emitter with spin and attraction removed; those
	// run from the element's shape and the attraction frames instead.
	readonly motion: Map<ParticleEmitter, ParticleProgram>;
}

export interface ParticleGraphState {
	serial: number;
	frame: number;
	index: number;
	readonly births: number[][];
	readonly parents: (ParticleElement | undefined)[][];
	readonly groups: Map<ParticleElement, ParticleGroup>[];
	readonly elements: (ParticleElement | undefined)[][];
	readonly root: ParticleElement;
	readonly scratch: ParticleGraphScratch;
}

// A2E980: whole 50 ms effect ticks.
const TICKS_PER_SECOND = 20;

// ============================================================================

/*
================
affineInto

out = m * (x, y, z, 1), each component stored as float32. out may be the
source vector's own array: all three inputs are read first.
================
*/
function affineInto( m: ArrayLike<number>, x: number, y: number, z: number, out: number[] ): void {
	const a = Math.fround( m[0]! * x + m[4]! * y + m[8]! * z + m[12]! ),
		b = Math.fround( m[1]! * x + m[5]! * y + m[9]! * z + m[13]! ),
		c = Math.fround( m[2]! * x + m[6]! * y + m[10]! * z + m[14]! );
	out[0] = a;
	out[1] = b;
	out[2] = c;
}

/*
================
copy3
================
*/
function copy3( from: ArrayLike<number>, to: number[] ): void {
	to[0] = from[0]!;
	to[1] = from[1]!;
	to[2] = from[2]!;
}

/*
================
setIdentity
================
*/
function setIdentity( m: Float32Array ): void {
	m.fill( 0 );
	m[0] =
		m[5] =
		m[10] =
		m[15] =
			1;
}

/*
================
inverseInto

Affine inverse into out. The 3x3 is stored as float32 first, and the
translation is formed from those stored values, as the native does.
================
*/
function inverseInto( m: Float32Array, out: Float32Array ): void {
	const a = m[0]!, b = m[4]!, c = m[8]!, d = m[1]!, e = m[5]!, f = m[9]!, g = m[2]!, h = m[6]!, i = m[10]!;
	const det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g);
	if ( Math.abs( det ) < 1e-12 ) throw Error( "Singular particle motion matrix" );
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
================
*/
function ancestor( element: ParticleElement, depth: number ): ParticleElement | undefined {
	if ( !depth ) return;
	let result = element.parent;
	for ( let n = 1; n < depth && result.parent !== result; n++ ) result = result.parent;
	return result;
}

/*
================
attract

AF4410/4A: velocity += (element.position - source.position) * signed
strength / distance.
================
*/
function attract( element: ParticleElement, source: ParticleElement, strength: number ): void {
	const position = element.state.position, from = source.state.position, velocity = element.state.velocity;
	const d0 = position[0]! - from[0]!, d1 = position[1]! - from[1]!, d2 = position[2]! - from[2]!;
	const length = Math.hypot( d0, d1, d2 );
	// Not a negated test: a NaN distance skips the pull, as native does.
	if ( !(length > 1e-6) ) return;
	velocity[0] = Math.fround( velocity[0]! + d0 * strength / length );
	velocity[1] = Math.fround( velocity[1]! + d1 * strength / length );
	velocity[2] = Math.fround( velocity[2]! + d2 * strength / length );
}

/*
================
commands
================
*/
function commands(
	element: ParticleElement,
	def: ParticleEmitter,
	age: number,
	history: ParticleGraphState,
	table: Float32Array,
	sibling?: ParticleElement
): void {
	const list = def.commands;
	if ( !list ) return;
	for ( let c = 0; c < list.length; c++ ) {
		const command = list[c]!;
		if ( !command.frames.includes( age ) ) continue;
		const program = command.program;
		if ( program.vectors ) {
			// A command's vectors all run on the frame the command fires.
			vectors( element, program.vectors, age, history.scratch.vector, sibling, true );
			continue;
		}
		if ( program.rVelocity ) {
			element.local.set( program.rVelocity );
			continue;
		}
		if ( program.spin ) {
			element.shape.set( program.spin );
			continue;
		}
		if ( program.shape ) {
			element.state.rotation.set( program.shape );
			continue;
		}
		if ( program.attraction !== undefined ) {
			attract( element, element.parent, program.attraction );
			continue;
		}
		const flags = program.coneFlags ?? program.coneForceFlags ?? 0;
		if ( flags === 3 && !sibling ) continue;
		const basis = flags === 1 ? element.matrix : flags === 3 ? sibling!.matrix : element.parent.matrix;
		const sample = initializeParticle(
			{ ...program, coneFlags: flags ? 2 : 0, coneForceFlags: flags ? 2 : 0 },
			table,
			history.index,
			basis
		);
		history.index = sample.index;
		if ( program.sphere ) {
			element.state.position = sample.state.position.map( ( v, i ) =>
				Math.fround( v + element.parent.state.position[i]! )
			);
		}
		if ( program.conePos ) {
			element.state.position = sample.state.position.map( ( v, i ) =>
				Math.fround( v + element.state.position[i]! )
			);
		}
		if ( program.cone ) element.state.velocity = sample.state.velocity;
		if ( program.coneForce ) {
			element.state.velocity = sample.state.velocity.map( ( v, i ) =>
				Math.fround( v + element.state.velocity[i]! )
			);
		}
		if ( program.scale ) element.state.scale = sample.state.scale;
	}
}

/*
================
vectors

Apply SetPosition / SetVelocity / Force operations scheduled on frame,
or every operation when everyFrame (a command firing). work is scratch.
================
*/
function vectors(
	element: ParticleElement,
	ops: readonly ParticleVectorCommand[],
	frame: number,
	work: number[],
	sibling: ParticleElement | undefined,
	everyFrame = false
): void {
	for ( let o = 0; o < ops.length; o++ ) {
		const op = ops[o]!;
		if ( !everyFrame && !op.frames.includes( frame ) ) continue;
		const flag = op.flags;
		copy3( op.value, work );
		if ( op.name === "SetPosition" ) {
			if ( (flag === 2 || flag === 5) && !sibling ) continue;
			const base = flag === 1 || flag === 4 || flag === 7 ?
				element.parent :
				flag === 2 || flag === 5 ?
				sibling :
				undefined;
			if ( flag >= 3 ) {
				affineInto( flag >= 6 ? element.parent.matrix : element.matrix, work[0]!, work[1]!, work[2]!, work );
			}
			if ( base ) element.state.position = [ ...base.state.position ];
			const position = element.state.position;
			for ( let i = 0; i < 3; i++ ) position[i] = Math.fround( position[i]! + work[i]! );
		} else {
			if ( flag === 3 && !sibling ) continue;
			if ( flag ) {
				affineInto(
					flag === 1 ? element.matrix : flag === 2 ? element.parent.matrix : sibling!.matrix,
					work[0]!,
					work[1]!,
					work[2]!,
					work
				);
			}
			const velocity = element.state.velocity, force = op.name === "Force";
			for ( let i = 0; i < 3; i++ ) velocity[i] = Math.fround( work[i]! + (force ? velocity[i]! : 0) );
		}
	}
}

/*
================
motionProgram

The program advanceParticle runs for an element of def: its update
program without spin and attraction. Built once per emitter.
================
*/
function motionProgram( history: ParticleGraphState, def: ParticleEmitter ): ParticleProgram {
	let program = history.scratch.motion.get( def );
	if ( !program ) {
		program = { ...(def.commands ? {} : def.program), spin: undefined, attraction: undefined };
		history.scratch.motion.set( def, program );
	}
	return program;
}

/*
================
tickHorizon

The last tick a graph can change: unbounded when any emitter loops or
emits, else the latest birth plus its lifetime.
================
*/
function tickHorizon( graph: readonly ParticleEmitter[] ): number {
	let last = 0;
	for ( let n = 0; n < graph.length; n++ ) {
		const def = graph[n]!;
		if ( def.loop || def.emission ) return Infinity;
		for ( let b = 0; b < def.births.length; b++ ) last = Math.max( last, def.births[b]! + def.frames );
	}
	return last;
}

// ============================================================================

/*
================
createParticleGraph
================
*/
export function createParticleGraph( graph: readonly ParticleEmitter[], index: number ): ParticleGraphState {
	const root = {
		previousPosition: [ 0, 0, 0 ],
		serial: 0,
		born: 0,
		clockBirth: 0,
		age: 0,
		alive: true,
		children: 0,
		released: false,
		state: { position: [ 0, 0, 0 ], velocity: [ 0, 0, 0 ], scale: [ 1, 1, 1 ], rotation: identity(), frame: 0 },
		matrix: identity(),
		delta: identity(),
		origin: [ 0, 0, 0 ],
		local: identity(),
		shape: identity()
	} as unknown as ParticleElement;
	Object.assign( root, { parent: root } );
	return {
		serial: 0,
		frame: -1,
		index,
		elements: graph.map( () => [] ),
		births: graph.map( d => d.emission ? [] : [ ...d.births ] ),
		parents: graph.map( () => [] ),
		groups: graph.map( d => d.parent < 0 ? new Map( [ [ root, { parent: root, total: 0 } ] ] ) : new Map() ),
		root,
		scratch: {
			matrix: identity(),
			inverse: identity(),
			previous: identity(),
			oldRoot: [ 0, 0, 0 ],
			vector: [ 0, 0, 0 ],
			motion: new Map()
		}
	};
}

/*
================
advanceParticleGraph
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
	const target = Math.min( Math.floor( time * TICKS_PER_SECOND + 1e-6 ), tickHorizon( graph ) );
	if ( target < history.frame || !Number.isSafeInteger( target ) ) throw Error( "Invalid particle graph time" );
	const scratch = history.scratch, elements = history.elements;
	// Only a region crossing shifts the origin. Every other call walked every
	// retained element slot to add zero: a skill trace showed it as the
	// largest single cost of its effects (0.8 ms of every frame).
	for ( let n = 0; (shiftX !== 0 || shiftZ !== 0) && n < elements.length; n++ ) {
		const rows = elements[n]!;
		for ( let b = 0; b < rows.length; b++ ) {
			const e = rows[b];
			if ( !e ) continue;
			e.state.position[0]! += shiftX;
			e.state.position[2]! += shiftZ;
			e.origin[0]! += shiftX;
			e.origin[2]! += shiftZ;
			e.previousPosition[0]! += shiftX;
			e.previousPosition[2]! += shiftZ;
		}
	}
	const root = history.root;
	root.state.position[0]! += shiftX;
	root.state.position[2]! += shiftZ;
	const oldRoot = scratch.oldRoot;
	copy3( root.state.position, oldRoot );
	scratch.previous.set( root.matrix );
	root.matrix.set( transform );
	root.matrix[12] = root.matrix[13] = root.matrix[14] = 0;
	inverseInto( scratch.previous, scratch.inverse );
	multiply( root.matrix, scratch.inverse, root.delta );
	for ( let tick = history.frame + 1; tick <= target; tick++ ) {
		if ( history.frame < 0 ) copy3( transform.subarray( 12, 15 ), root.origin );
		else copy3( oldRoot, root.origin );
		copy3( transform.subarray( 12, 15 ), root.state.position );
		if ( tick > history.frame + 1 ) {
			copy3( root.state.position, root.origin );
			setIdentity( root.delta );
		}
		root.age = tick;
		// AF2A40 returns capacity on retirement, even while descendants retain the
		// dead parent. Release identity only after the last descendant lets it go.
		for ( let n = 0; n < graph.length; n++ ) {
			const rows = elements[n]!, def = graph[n]!;
			for ( let b = 0; b < rows.length; b++ ) {
				const element = rows[b];
				if ( !element || !element.alive ) continue;
				copy3( element.state.position, element.previousPosition );
				snapshotParticleTick( element.state, element.matrix );
				if (
					(def.loop || keepRoots && def.parent < 0 && tick / TICKS_PER_SECOND < stop) &&
					tick - element.clockBirth >= def.frames
				) element.clockBirth += Math.floor( (tick - element.clockBirth) / def.frames ) * def.frames;
				element.age = tick - element.clockBirth;
				if ( element.age >= def.frames ) {
					element.alive = false;
					if ( element.group ) element.group.total = Math.fround( element.group.total - 1 );
					copy3( element.state.position, element.origin );
					setIdentity( element.delta );
				}
			}
		}
		let released = true;
		while ( released ) {
			released = false;
			for ( let n = 0; n < elements.length; n++ ) {
				const rows = elements[n]!;
				for ( let b = 0; b < rows.length; b++ ) {
					const element = rows[b];
					if ( !element || element.alive || element.children || element.released ) continue;
					element.released = true;
					element.parent.children--;
					for ( let g = 0; g < history.groups.length; g++ ) history.groups[g]!.delete( element );
					released = true;
				}
			}
		}
		for ( let n = 0; n < graph.length; n++ ) {
			const def = graph[n]!, rows = elements[n]!, births = history.births[n]!;
			if ( def.emission && tick / TICKS_PER_SECOND < stop ) {
				for ( const group of history.groups[n]!.values() ) {
					if ( !group.parent.alive ) continue;
					const emitted = particleEmission( def.emission, group.total, group.parent.age );
					group.total = emitted.total;
					for ( let count = 0; count < emitted.emitted; count++ ) {
						let slot = rows.length;
						for ( let s = 0; s < rows.length; s++ ) {
							if ( rows[s]?.released ) {
								slot = s;
								break;
							}
						}
						if ( slot >= (def.capacity ?? def.births.length) ) {
							throw Error( "Particle live capacity exceeded" );
						}
						// Reserve distinct slots for simultaneous births before evaluation.
						rows[slot] = undefined;
						births[slot] = tick;
						history.parents[n]![slot] = group.parent;
						rows.length = Math.max( rows.length, slot + 1 );
					}
				}
			}
			for ( let b = 0; b < births.length; b++ ) {
				const birth = births[b]!,
					elapsed = tick - birth,
					existing = rows[b],
					age = existing ? existing.age : def.loop && elapsed >= 0 ? elapsed % def.frames : elapsed;
				if ( age < 0 || (!existing && birth / TICKS_PER_SECOND >= stop) ) continue;
				const parent = def.emission ?
					history.parents[n]![b] :
					def.parent < 0 ?
					root :
					elements[def.parent]![def.parents[b]!];
				if ( !parent ) continue;
				let element = rows[b];
				const keptMatrix = def.keepMatrix && !!element;
				if ( keptMatrix ) scratch.previous.set( element!.matrix );
				// The old sorted list was consumed only at its final element. Select the
				// same greatest serial directly, including later equal-serial entries.
				let sibling: ParticleElement | undefined;
				if ( def.emission ) {
					for ( let s = 0; s < rows.length; s++ ) {
						const e = rows[s];
						if (
							e && e !== element && e.alive && e.parent === parent && e.born <= birth &&
							(!element || e.serial < element.serial) && (!sibling || e.serial >= sibling.serial)
						) sibling = e;
					}
				} else if ( b > 0 && def.parents[b - 1] === def.parents[b] ) sibling = rows[b - 1];
				if ( !element ) {
					const sample = initializeParticle(
						def.commands ? {} : def.program ?? {},
						table,
						history.index,
						parent.matrix
					);
					history.index = sample.index;
					const state = sample.state, inherited = [ ...parent.state.velocity ];
					state.position = state.position.map( ( v, i ) => Math.fround( v + parent.state.position[i]! ) );
					element = {
						previousPosition: [ ...state.position ],
						serial: ++history.serial,
						born: birth,
						clockBirth: birth,
						age: 0,
						alive: true,
						children: 0,
						released: false,
						group: def.emission ? history.groups[n]!.get( parent ) : undefined,
						state,
						matrix: parent.matrix.slice(),
						delta: identity(),
						origin: [ ...parent.state.position ],
						local: identity(),
						shape: identity(),
						parent
					};
					rows[b] = element;
					parent.children++;
					for ( let child = 0; child < graph.length; child++ ) {
						if ( graph[child]!.parent === n && graph[child]!.emission ) {
							history.groups[child]!.set( element, { parent: element, total: 0 } );
						}
					}
					if ( def.matrixDepth < 0 ) {
						const translation = element.matrix.slice( 12, 15 );
						element.matrix.set( identity() );
						element.matrix.set( translation, 12 );
					}
					if ( def.matrix ) element.matrix.set( def.matrix );
					const follow = ancestor( element, def.followDepth );
					for ( let i = 0; i < 3; i++ ) {
						inherited[i] = Math.fround( inherited[i]! - (follow?.state.velocity[i] ?? 0) );
						state.position[i] = Math.fround( state.position[i]! + inherited[i]! );
						if ( def.commands || !def.program?.cone ) {
							state.velocity[i] = Math.fround( state.velocity[i]! + inherited[i]! );
						}
					}
					element.state.angularVelocity = undefined;
					if ( !def.commands && def.program?.vectors ) {
						vectors( element, def.program.vectors, 0, scratch.vector, sibling );
					}
				} else if ( age < def.frames ) {
					const state = element.state;
					if ( def.keepOrigin ) copy3( state.position, element.origin );
					const positionParent = ancestor( element, def.positionDepth ),
						matrixParent = ancestor( element, Math.max( 0, def.matrixDepth ) ),
						velocityParent = ancestor( element, def.velocityDepth ),
						follow = ancestor( element, def.followDepth );
					if ( positionParent ) {
						const origin = positionParent.origin, position = state.position;
						affineInto(
							positionParent.delta,
							position[0]! - origin[0]!,
							position[1]! - origin[1]!,
							position[2]! - origin[2]!,
							position
						);
						for ( let i = 0; i < 3; i++ ) position[i] = Math.fround( position[i]! + origin[i]! );
					}
					if ( matrixParent ) {
						multiply( matrixParent.delta, element.matrix, scratch.matrix );
						element.matrix.set( scratch.matrix );
					}
					if ( def.localMotion ) {
						multiply( element.matrix, element.local, scratch.matrix );
						element.matrix.set( scratch.matrix );
					}
					if ( velocityParent ) {
						const velocity = state.velocity;
						affineInto( velocityParent.delta, velocity[0]!, velocity[1]!, velocity[2]!, velocity );
					}
					if ( follow ) {
						for ( let i = 0; i < 3; i++ ) {
							state.position[i] = Math.fround(
								state.position[i]! + follow.state.position[i]! - follow.origin[i]!
							);
						}
					}
					history.index = advanceParticle(
						state,
						motionProgram( history, def ),
						state.frame + 1,
						table,
						history.index
					);
					if ( def.shapeMotion ) {
						multiply( state.rotation, element.shape, scratch.matrix );
						state.rotation.set( scratch.matrix );
					}
					if ( !def.commands && def.program?.vectors ) {
						vectors( element, def.program.vectors, age, scratch.vector, sibling );
					}
				}
				if ( age < def.frames ) {
					commands( element, def, age, history, table, sibling );
					const program = def.commands ? undefined : def.program;
					if ( program?.rVelocity && (program.rotationFrames ?? [ 0 ]).includes( age ) ) {
						element.local.set( program.rVelocity );
					}
					if ( program?.spin && (program.spinFrames ?? [ 0 ]).includes( age ) ) {
						element.shape.set( program.spin );
					}
					if ( program?.attraction !== undefined && (program.attractionFrames ?? [ 0 ]).includes( age ) ) {
						attract( element, parent, program.attraction );
					}
					const at = Math.min( age, def.scales.length - 1 );
					// Every particle owns a fresh scale array (initializeParticle,
					// sampleScale), so the frame's scale is written into it.
					if ( at >= 0 ) copy3( def.scales[at]!, element.state.scale );
					const pos = def.positions[Math.min( age, def.positions.length - 1 )];
					if ( pos ) {
						const position = element.state.position, from = parent.state.position;
						affineInto( parent.matrix, pos[0]!, pos[1]!, pos[2]!, position );
						for ( let i = 0; i < 3; i++ ) position[i] = Math.fround( position[i]! + from[i]! );
					}
					const rot = def.rotations[Math.min( age, def.rotations.length - 1 )];
					if ( rot ) element.matrix.set( rot );
					if ( keptMatrix ) {
						inverseInto( scratch.previous, scratch.inverse );
						multiply( element.matrix, scratch.inverse, element.delta );
					}
					element.state.frame = age;
					if ( element.born === tick ) {
						copy3( element.state.position, element.previousPosition );
						snapshotParticleTick( element.state, element.matrix );
					}
				} else {
					copy3( element.state.position, element.origin );
					setIdentity( element.delta );
				}
			}
		}
	}
	history.frame = target;
}

/*
================
particleElementMatrix

Presentation-only bounded prediction: exact tick poses are unchanged. No
random draws, command execution, births or retirement occur here. The
element frame continues its last tick rotation the same way
(particle-presentation.ts, deliberate smoothing deviation).
================
*/
export function particleElementMatrix(
	element: ParticleElement,
	out: Float32Array,
	offset: number,
	fraction = 0,
	work?: Float64Array
): void {
	const blend = Math.max( 0, Math.min( 1, fraction ) ), previous = element.state.previous;
	if ( previous && blend > 0 ) continueRotation( previous.matrix, element.matrix, blend, out, offset, work );
	else out.set( element.matrix, offset );
	for ( let axis = 0; axis < 3; axis++ ) {
		out[offset + 12 + axis] = element.state.position[axis]! +
			(element.state.position[axis]! - element.previousPosition[axis]!) * blend;
	}
}
