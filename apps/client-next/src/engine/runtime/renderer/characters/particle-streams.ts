/*
===========================================================================

particle-streams.ts - the CPU half of emitted particle presentation

An emitted primitive's batch draws one instance per particle slot, and
the GPU presentation pass (device/particle-shader.ts) writes every slot's
instance and palette each frame from the slot's tick record
(foundation/animation/particle-records.ts). This module keeps a batch's
records current: it rewrites a slot only when its tick state changed (a
graph tick, a program particle's tick, a birth, a death, a region
crossing or a moving birth matrix) and fills the per-frame actor rows.

Ticks stay native: program particles advance here exactly as before,
only when drawn and only to their whole tick.

===========================================================================
*/

import type { CharacterActor, CharacterModel, CharacterPrimitive } from "@/engine/contracts/character";
import type { ParticleGraphState } from "@/engine/foundation/animation/particle-graph";
import { advanceParticle, type ParticleInstance } from "@/engine/foundation/animation/particle-program";
import {
	PARTICLE_TICKS_PER_SECOND,
	ROTATION_WORK,
	snapshotParticleTick
} from "@/engine/foundation/animation/particle-presentation";
import {
	ACTOR_FRACTION,
	ACTOR_OPACITY,
	ACTOR_PALETTE,
	ACTOR_TIME,
	emittedMatrixMatches,
	hideRecord,
	PARTICLE_ACTOR,
	PARTICLE_RECORD,
	particleView,
	recordLive,
	writeEmittedRecord,
	writeGraphRecord
} from "@/engine/foundation/animation/particle-records";
import { cameraAxes } from "@/engine/foundation/rendering/effect-billboard";
import type { ParticlePresentation } from "../internal/gpu-contract";

/*
================
ParticleHistory

An actor's particle simulation (characters.ts owns it for the actor's
lifetime): its graph, or its primitives' birth matrices and programs.
================
*/
export interface ParticleHistory {
	readonly graph?: ParticleGraphState;
	readonly matrices: readonly (Float32Array | undefined)[];
	readonly programs: readonly (ParticleInstance | undefined)[][];
}

/*
================
ParticleRandom

The shared native random table and its cursor; program ticks draw from it.
================
*/
export interface ParticleRandom {
	readonly table: Float32Array;
	index: number;
}

/*
================
ParticleRow

One actor's inputs for writeParticleRow. The caller keeps one
(createParticleRow) and fills it per actor, so the frame allocates nothing.
================
*/
export interface ParticleRow {
	actor: CharacterActor | undefined;
	history: ParticleHistory | undefined;
	// The pose an own-emission particle's palette starts from.
	pose: { palette( primitive: CharacterPrimitive, out: Float32Array, offset: number ): void; } | undefined;
	opacity: number;
	origin: number;
}

/*
================
createParticleRow
================
*/
export function createParticleRow(): ParticleRow {
	return { actor: undefined, history: undefined, pose: undefined, opacity: 1, origin: 0 };
}

/*
================
ParticleStream

A batch primitive's presentation input, with what its records were last
written from: per actor, its history, graph tick and origin. A row whose
actor changes (new batch membership) has a new history and is rewritten.
================
*/
export interface ParticleStream extends ParticlePresentation {
	readonly primitive: CharacterPrimitive;
	readonly index: number;
	// Graph particles start from the model's bind scale.
	readonly graphPalette?: Float32Array;
	readonly owners: (ParticleHistory | undefined)[];
	readonly ticks: Float64Array;
	readonly origins: Float64Array;
	readonly work: Float64Array;
	readonly basis: Float64Array;
	// Live particles the last frame drew (the frame probe's count).
	live: number;
}

/*
================
createParticleStream

The stream of model's primitive index for rows actors. Every slot starts
empty and every record dirty.
================
*/
export function createParticleStream( model: CharacterModel, index: number, rows: number ): ParticleStream {
	const primitive = model.primitives[index]!, emission = primitive.emission;
	if ( !emission ) throw Error( "Particle stream requires an emission" );
	// The pass writes one palette matrix a slot (particle-shader.ts).
	if ( primitive.joints.length !== 1 ) throw Error( "Emitted primitive must have one joint" );
	const slots = emission.capacity ?? emission.births.length;
	let graphPalette: Float32Array | undefined;
	if ( model.particleGraph ) {
		graphPalette = new Float32Array( 16 );
		for ( let axis = 0; axis < 3; axis++ ) graphPalette[axis * 5] = model.nodes[0]!.scale[axis]!;
		graphPalette[15] = 1;
	}
	return {
		rows,
		slots,
		graph: !!model.particleGraph,
		view: particleView( primitive.billboard ),
		lifetime: emission.lifetime,
		loop: !!emission.loop,
		frames: primitive.materialFrames,
		records: new Float32Array( rows * slots * PARTICLE_RECORD ),
		actors: new Float32Array( rows * PARTICLE_ACTOR ),
		axes: new Float32Array( 12 ),
		dirtyStart: 0,
		dirtyEnd: rows * slots,
		primitive,
		index,
		graphPalette,
		owners: new Array( rows ).fill( undefined ),
		ticks: new Float64Array( rows ).fill( NaN ),
		origins: new Float64Array( rows ).fill( NaN ),
		work: new Float64Array( ROTATION_WORK ),
		basis: new Float64Array( 9 ),
		live: 0
	};
}

/*
================
markDirty
================
*/
function markDirty( stream: ParticleStream, start: number, end: number ): void {
	stream.dirtyStart = Math.min( stream.dirtyStart, start );
	stream.dirtyEnd = Math.max( stream.dirtyEnd, end );
}

/*
================
writeGraphRow

A graph changes only at its ticks and region crossings: the actor's
records are rewritten then, and otherwise left alone.
================
*/
function writeGraphRow( stream: ParticleStream, index: number, history: ParticleHistory, origin: number ): void {
	const graph = history.graph!, base = index * stream.slots;
	const elements = graph.elements[stream.primitive.particleEmitter!]!;
	if ( elements.length > stream.slots ) throw Error( "Particle graph outside its emission capacity" );
	let live = 0;
	for ( let b = 0; b < elements.length; b++ ) if ( elements[b]?.alive ) live++;
	stream.live += live;
	if (
		stream.owners[index] === history && stream.ticks[index] === graph.frame && stream.origins[index] === origin
	) return;
	for ( let b = 0; b < stream.slots; b++ ) {
		const element = elements[b];
		if ( element?.alive ) writeGraphRecord( stream.records, base + b, element, stream.work );
		else hideRecord( stream.records, base + b );
	}
	stream.owners[index] = history;
	stream.ticks[index] = graph.frame;
	stream.origins[index] = origin;
	markDirty( stream, base, base + stream.slots );
}

/*
================
writeEmittedRow

A primitive's own emission: births at fixed times, each drawn at its
birth matrix. A drawn program particle advances to its whole tick here
(the last step after a snapshot, so the pass can continue it).
================
*/
function writeEmittedRow(
	stream: ParticleStream,
	index: number,
	actor: CharacterActor,
	history: ParticleHistory,
	random: ParticleRandom
): void {
	const primitive = stream.primitive, emission = primitive.emission!, program = primitive.particleProgram;
	const base = index * stream.slots, records = stream.records;
	const matrices = history.matrices[stream.index]!, programs = history.programs[stream.index]!;
	const fresh = stream.owners[index] !== history;
	stream.owners[index] = history;
	for ( let b = 0; b < stream.slots; b++ ) {
		const slot = base + b, birth = emission.births[b], at = b * 16;
		const elapsed = birth === undefined ? -1 : actor.time - birth,
			age = emission.loop && elapsed >= 0 ? elapsed % emission.lifetime : elapsed;
		if (
			birth === undefined || age < 0 || age >= emission.lifetime || !Number.isFinite( matrices[at + 15] ) ||
			actor.emissionEnd !== undefined && birth >= actor.emissionEnd
		) {
			if ( recordLive( records, slot ) ) {
				hideRecord( records, slot );
				markDirty( stream, slot, slot + 1 );
			}
			continue;
		}
		stream.live++;
		const particle = programs[b];
		let advanced = false;
		if ( particle && program ) {
			const tick = Math.floor( age * PARTICLE_TICKS_PER_SECOND );
			if ( tick > particle.frame ) {
				// Advance to the tick before, record it, then take the last
				// step: the same sequential steps as one call.
				if ( tick - 1 > particle.frame ) {
					random.index = advanceParticle( particle, program, tick - 1, random.table, random.index );
				}
				snapshotParticleTick( particle );
				random.index = advanceParticle( particle, program, tick, random.table, random.index );
				advanced = true;
			}
		}
		if (
			!fresh && !advanced && recordLive( records, slot ) && emittedMatrixMatches( records, slot, matrices, at )
		) {
			continue;
		}
		writeEmittedRecord( records, slot, matrices, at, birth, particle, stream.work );
		markDirty( stream, slot, slot + 1 );
	}
}

/*
================
writeParticleRow

Actor index's row for this frame, and its slots' records where their tick
state changed.
================
*/
export function writeParticleRow(
	stream: ParticleStream,
	index: number,
	row: ParticleRow,
	random: ParticleRandom
): void {
	const actor = row.actor, history = row.history;
	if ( !actor || !history ) throw Error( "Incomplete particle row" );
	const at = index * PARTICLE_ACTOR, actors = stream.actors, graph = history.graph;
	actors[at + ACTOR_TIME] = actor.time;
	actors[at + ACTOR_OPACITY] = row.opacity;
	if ( stream.graphPalette ) {
		if ( !graph ) throw Error( "Graph particle stream without its graph" );
		actors[at + ACTOR_FRACTION] = actor.time * PARTICLE_TICKS_PER_SECOND - graph.frame;
		actors.set( stream.graphPalette, at + ACTOR_PALETTE );
		writeGraphRow( stream, index, history, row.origin );
		return;
	}
	if ( !row.pose ) throw Error( "Emitted particle stream without its pose" );
	actors[at + ACTOR_FRACTION] = 0;
	row.pose.palette( stream.primitive, actors, at + ACTOR_PALETTE );
	writeEmittedRow( stream, index, actor, history, random );
}

/*
================
beginParticleFrame

Start a frame: no particles counted yet, and the view mode's camera basis
from view (the world view matrix the billboards face).
================
*/
export function beginParticleFrame( stream: ParticleStream, view: Float32Array | undefined ): void {
	stream.live = 0;
	const mode = stream.primitive.billboard;
	if ( mode !== "camera" && mode !== "y" ) return;
	if ( !view ) throw Error( "Missing effect camera basis" );
	cameraAxes( view, mode, stream.basis );
	for ( let column = 0; column < 3; column++ ) {
		for ( let row = 0; row < 3; row++ ) stream.axes[column * 4 + row] = stream.basis[column * 3 + row]!;
	}
}

/*
================
endParticleFrame

The device has taken this frame's dirty records.
================
*/
export function endParticleFrame( stream: ParticleStream ): void {
	stream.dirtyStart = stream.rows * stream.slots;
	stream.dirtyEnd = 0;
}
