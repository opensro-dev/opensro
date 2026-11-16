/*
===========================================================================

particle-graph-plan.ts - what an EFP element tree's topology fixes for every tick

The particle graph (particle-graph.ts) ticks every effect on screen at
20 Hz, and much of what each tick looked up per element depends only on
the authored emitters: which ancestors' links carry the holder, on which
frames a command fires, the program a random-sample command samples,
which child emitters emit from an element. The plan holds those, built
once per model, so the tick reads tables instead of walking ancestors,
searching frame lists and building objects.

The plan is immutable; a graph state is created with the plan of its
model (createParticleGraph) and every tick reads it.

===========================================================================
*/

import type { ParticleEmitter } from "@/engine/foundation/animation/particle-graph";
import { rootLinkFlags, type LinkNode } from "@/engine/foundation/animation/particle-holder-link";
import {
	SPHERE_POS_PARENT,
	SPHERE_POS_SIBLING,
	type ParticleProgram
} from "@/engine/foundation/animation/particle-program";

/*
================
ParticleGraphPlan

Per emitter n of graph:

- rootLink[n]: the holder links its elements carry (rootLinkFlags of its
  position, follow and matrix ancestors). An element's ancestor at depth d
  is the element of the emitter d parents up, or the holder (the graph
  root) when the chain ends first, and each ancestor's links are its own
  emitter's: so the links are fixed per emitter.
- motion[n]: the program advanceParticle runs for its elements, its update
  program without spin and attraction (those run from the element's shape
  and the attraction frames instead).
- commandFrames[n][c], vectorFrames[n][o]: frame membership of command c
  and of update vector o; rotationFrames, spinFrames, attractionFrames[n]:
  of the update program's SetRVelocity, SetShapeRotVel and Attraction
  (frame 0 when the program names none). A table holds 1 at each frame.
- samples[n][c]: the program a random-sample command c samples, its cone
  and sphere bases replaced by the basis the tick passes (undefined for a
  command that samples nothing).
- emittingChildren[n]: the child emitters that emit from its elements, in
  emitter order.
- sibling[n]: 1 when its commands read the element's predecessor.
- horizon: the last tick the graph can change.
================
*/
export interface ParticleGraphPlan {
	readonly graph: readonly ParticleEmitter[];
	readonly rootLink: Int32Array;
	readonly motion: readonly ParticleProgram[];
	readonly commandFrames: readonly (readonly Uint8Array[])[];
	readonly vectorFrames: readonly (readonly Uint8Array[])[];
	readonly rotationFrames: readonly Uint8Array[];
	readonly spinFrames: readonly Uint8Array[];
	readonly attractionFrames: readonly Uint8Array[];
	readonly samples: readonly (readonly (ParticleProgram | undefined)[])[];
	readonly emittingChildren: readonly (readonly number[])[];
	readonly sibling: Uint8Array;
	readonly horizon: number;
}

// The frame list of a program step that names none: its first frame.
const FIRST_FRAME = [ 0 ] as const;

/*
================
frameTable

1 at every frame of frames; frames that are not whole and non-negative
can never equal an element age and are left out.
================
*/
function frameTable( frames: readonly number[] ): Uint8Array {
	let length = 0;
	for ( let i = 0; i < frames.length; i++ ) {
		const frame = frames[i]!;
		if ( Number.isSafeInteger( frame ) && frame >= 0 ) length = Math.max( length, frame + 1 );
	}
	const table = new Uint8Array( length );
	for ( let i = 0; i < frames.length; i++ ) {
		const frame = frames[i]!;
		if ( Number.isSafeInteger( frame ) && frame >= 0 ) table[frame] = 1;
	}
	return table;
}

/*
================
onFrame

True when table holds frame (frameTable).
================
*/
export function onFrame( table: Uint8Array, frame: number ): boolean {
	return frame >= 0 && frame < table.length && table[frame] === 1;
}

/*
================
programNeedsSibling

Only sibling-relative commands consume the predecessor. Avoid building
an index for ordinary independent particles; their random cursor and
command execution remain unchanged.
================
*/
function programNeedsSibling( program: ParticleProgram | undefined ): boolean {
	if ( !program ) return false;
	if ( program.coneFlags === 3 || program.coneForceFlags === 3 ) return true;
	if ( program.sphere && program.sphereFlags === SPHERE_POS_SIBLING ) return true;
	return program.vectors?.some( command =>
		command.name === "SetPosition" ?
			command.flags % 3 === 2 || command.flags >= 9 :
			command.flags === 3
	) ?? false;
}

/*
================
needsSibling
================
*/
function needsSibling( def: ParticleEmitter ): boolean {
	if ( !def.emission ) return false;
	if ( programNeedsSibling( def.program ) ) return true;
	return def.commands?.some( command => {
		const flags = command.flags ?? 0;
		return !!command.rotations && flags === 3 ||
			!!command.positions && (flags % 3 === 2 || flags >= 9) || programNeedsSibling( command.program );
	} ) ?? false;
}

/*
================
sampleProgram

The program a command samples when it fires (particle-graph.ts commands),
or undefined when its program runs another step. The tick passes the cone
basis itself and adds the sphere base, so the sample takes them as the
parent's.
================
*/
function sampleProgram( command: NonNullable<ParticleEmitter["commands"]>[number] ): ParticleProgram | undefined {
	const program = command.program;
	if (
		program.vectors || program.rVelocity || program.spin || program.shape || program.attraction !== undefined
	) return;
	const coneFlags = program.coneFlags ?? program.coneForceFlags ?? 0;
	return {
		...program,
		coneFlags: coneFlags ? 2 : 0,
		coneForceFlags: coneFlags ? 2 : 0,
		sphereFlags: SPHERE_POS_PARENT
	};
}

/*
================
ancestorEmitter

The emitter depth parents up from emitter n, or -1 for the holder when
the chain ends first; undefined when depth names none. The chain stops at
the holder as particle-graph.ts ancestor() stops at the root.
================
*/
function ancestorEmitter( graph: readonly ParticleEmitter[], n: number, depth: number ): number | undefined {
	if ( !depth ) return;
	let result = graph[n]!.parent;
	for ( let d = 1; d < depth && result >= 0; d++ ) result = graph[result]!.parent;
	return result;
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

/*
================
createParticleGraphPlan

Emitters name parents before themselves (particle-graph.ts ticks parents
first), so each emitter's links are found from its ancestors' already.
================
*/
export function createParticleGraphPlan( graph: readonly ParticleEmitter[] ): ParticleGraphPlan {
	const rootLink = new Int32Array( graph.length ), root: LinkNode = { rootLink: 0 };
	const node = ( emitter: number | undefined ): LinkNode | undefined =>
		emitter === undefined ? undefined : emitter < 0 ? root : { rootLink: rootLink[emitter]! };
	for ( let n = 0; n < graph.length; n++ ) {
		const def = graph[n]!;
		if ( def.parent >= n ) throw Error( "Particle emitter after its child" );
		rootLink[n] = rootLinkFlags(
			node( ancestorEmitter( graph, n, def.positionDepth ) ),
			node( ancestorEmitter( graph, n, def.followDepth ) ),
			def.matrixDepth > 0 ? node( ancestorEmitter( graph, n, def.matrixDepth ) ) : undefined,
			root
		);
	}
	return {
		graph,
		rootLink,
		motion: graph.map( def => ({ ...(def.commands ? {} : def.program), spin: undefined, attraction: undefined }) ),
		commandFrames: graph.map( def => (def.commands ?? []).map( command => frameTable( command.frames ) ) ),
		vectorFrames: graph.map( def => (def.program?.vectors ?? []).map( op => frameTable( op.frames ) ) ),
		rotationFrames: graph.map( def => frameTable( def.program?.rotationFrames ?? FIRST_FRAME ) ),
		spinFrames: graph.map( def => frameTable( def.program?.spinFrames ?? FIRST_FRAME ) ),
		attractionFrames: graph.map( def => frameTable( def.program?.attractionFrames ?? FIRST_FRAME ) ),
		samples: graph.map( def => (def.commands ?? []).map( sampleProgram ) ),
		emittingChildren: graph.map( ( _, n ) => {
			const children: number[] = [];
			for ( let child = 0; child < graph.length; child++ ) {
				if ( graph[child]!.parent === n && graph[child]!.emission ) children.push( child );
			}
			return children;
		} ),
		sibling: Uint8Array.from( graph, def => needsSibling( def ) ? 1 : 0 ),
		horizon: tickHorizon( graph )
	};
}
