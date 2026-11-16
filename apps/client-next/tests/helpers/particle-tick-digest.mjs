/*
===========================================================================

particle-tick-digest.mjs - a digest of every particle graph tick in the catalogue

The oracle for changes to the particle graph tick
(src/engine/foundation/animation/particle-graph.ts) that must not change
its results: every effect of /assets/effects/programs.json with a particle
graph runs through three scenarios, and after every call a sha256 takes in
the whole graph state (clock, random cursor, root, every slot below each
emitter's slots in use with its links, group totals in group order, the
tick pose of a drawn emitter's elements) and the tick record of every live
slot of a drawn emitter: the one the tick wrote (particle-records.ts
writeGraphRecord), which a particle stream draws (a call that runs no tick
and shifts nothing adds the clock and cursor only; scenarioDigest says
why). Each (scenario, effect) keeps the first 16 hex digits. Three
mutations of the tick, each alone, change hundreds of effects' digests:
one fround dropped in linkElement, the emission groups walked in reverse,
one random draw not consumed in commands.

What no reader has is left out: the program position of a graph record
(the pass draws none for graph streams), and a released element's parent
and group, which its slot stops naming once their slots are reused
(marked RELEASED). The catalogue's graphs are all emission emitters but
one without births; fixed-birth emitters are held by the runtime tests.

	S1  a still holder, roots kept, a late start (1000.37 s), 60 Hz calls
	    for 20 s
	S2  a holder that turns and moves every call, calls 7 to 40 ms apart
	    with a 130 ms gap every 50th call (several ticks in one call), and
	    a region shift of -1920 in x at 10 s
	S3  a still holder, roots kept, the emission stopped at 5 s, 60 Hz
	    calls for 12 s

An effect whose tick throws keeps the error message in its digest and
ends that scenario there.

tools/particle-tick-digest.mjs writes tests/fixtures/particle-tick-digest.json;
tests/runtime/particle-tick-digest.test.mjs holds the tick to it.

===========================================================================
*/
import "./native-source-loader.mjs";
import { createHash } from "node:crypto";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

const SOURCE = new URL( "../../src/engine/", import.meta.url );

/*
================
load
================
*/
async function load( path ) {
	return import( new URL( path, SOURCE ).href );
}

const { createEffectPrograms } = await load( "runtime/assets/worker/effects/program/program.ts" );
const { advanceParticleGraph } = await load( "foundation/animation/particle-graph.ts" );
const { createParticleGraph, ROOT_SLOT, SLOT_ALIVE, SLOT_OCCUPIED, SLOT_RELEASED, SLOT_SNAPSHOT } = await load(
	"foundation/animation/particle-graph-slots.ts"
);
const { createParticleGraphPlan } = await load( "foundation/animation/particle-graph-plan.ts" );
const { particleRandomTable } = await load( "foundation/animation/particle-program.ts" );
const { writeGraphRecord, PARTICLE_RECORD } = await load( "foundation/animation/particle-records.ts" );

export const DIGEST_PATH = new URL( "../fixtures/particle-tick-digest.json", import.meta.url );
export const SCENARIOS = [ "S1", "S2", "S3" ];

// Hex digits kept of each (scenario, effect) sha256.
const DIGEST_DIGITS = 16;
// The holder's region shift in S2, and when it happens.
const REGION_SHIFT = -1920;
const SHIFT_SECONDS = 10;
// Marks in the value stream: an empty slot, a missing value, a released
// element's parent and group.
const EMPTY = -1e300;
const NONE = -2e300;
const RELEASED = -3e300;

/*
================
pathCursor

The random cursor an effect starts from: FNV-1a of its path, on the table.
================
*/
function pathCursor( path, length ) {
	let hash = 0x811c9dc5;
	for ( let i = 0; i < path.length; i++ ) hash = Math.imul( hash ^ path.charCodeAt( i ), 0x01000193 ) >>> 0;
	return hash % length;
}

/*
================
holderMatrix

A holder turned by yaw about y and placed at (x, y, z), column-major.
================
*/
function holderMatrix( out, yaw, x, y, z ) {
	const c = Math.cos( yaw ), s = Math.sin( yaw );
	out.set( [ c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, x, y, z, 1 ] );
	return out;
}

/*
================
scenarioCalls

The calls of one scenario: each { time, shiftX, yaw, x, y, z }, and the
scenario's stop and keepRoots.
================
*/
function scenarioCalls( name ) {
	const calls = [];
	if ( name === "S1" || name === "S3" ) {
		const start = name === "S1" ? 1000.37 : 0, count = name === "S1" ? 20 * 60 : 12 * 60;
		for ( let k = 0; k < count; k++ ) {
			calls.push( { time: start + k / 60, shiftX: 0, yaw: 0.7, x: 6123.5, y: 12.25, z: 815.75 } );
		}
		return { calls, stop: name === "S1" ? Infinity : 5, keepRoots: true };
	}
	let time = 0, shifted = false;
	for ( let k = 0; time < 20; k++ ) {
		const crossing = !shifted && time >= SHIFT_SECONDS;
		if ( crossing ) shifted = true;
		calls.push( {
			time,
			shiftX: crossing ? REGION_SHIFT : 0,
			yaw: 0.02 * k,
			x: 3200.5 + 0.31 * k + (shifted ? REGION_SHIFT : 0),
			y: 4 + 0.5 * Math.sin( 0.1 * k ),
			z: 910.25 + 0.17 * k
		} );
		time += k % 50 === 49 ? 0.13 : (7 + (k * 13) % 34) / 1000;
	}
	return { calls, stop: Infinity, keepRoots: false };
}

/*
================
createValues

The values of one call, in two growable lanes hashed as their bytes: wide
(float64) for scalars and vectors, which are doubles (a region shift adds
without rounding), and narrow (float32) for the Float32Array matrices and
records, which halves the bytes hashed.
================
*/
function createValues() {
	return { wide: new Float64Array( 1 << 16 ), wideLength: 0, narrow: new Float32Array( 1 << 18 ), narrowLength: 0 };
}

/*
================
grown

lane, or a copy at least twice as large when count more values do not fit
after its first used values.
================
*/
function grown( lane, used, count ) {
	if ( used + count <= lane.length ) return lane;
	let size = lane.length * 2;
	while ( size < used + count ) size *= 2;
	const copy = new lane.constructor( size );
	copy.set( lane.subarray( 0, used ) );
	return copy;
}

/*
================
push
================
*/
function push( values, value ) {
	values.wide = grown( values.wide, values.wideLength, 1 );
	values.wide[values.wideLength++] = value;
}

/*
================
pushList

A list's length and values, or NONE when it is missing.
================
*/
function pushList( values, list ) {
	if ( !list ) {
		push( values, NONE );
		return;
	}
	values.wide = grown( values.wide, values.wideLength, list.length + 1 );
	values.wide[values.wideLength++] = list.length;
	values.wide.set( list, values.wideLength );
	values.wideLength += list.length;
}

/*
================
pushNarrow

A Float32Array's values in the narrow lane, its length (or NONE when it
is missing) in the wide one.
================
*/
function pushNarrow( values, list ) {
	push( values, list ? list.length : NONE );
	if ( !list ) return;
	values.narrow = grown( values.narrow, values.narrowLength, list.length );
	values.narrow.set( list, values.narrowLength );
	values.narrowLength += list.length;
}

/*
================
pushElement

Every field of slot s, an element of emitter n (the holder for n -1);
parents and groups by serial (the holder is 0). A released element's
parent and group are marked RELEASED: its slot no longer names them once
their slots are reused. Only an element a primitive draws (drawn) has a
tick pose.
================
*/
function pushElement( values, history, s, n, drawn ) {
	const flags = history.flags[s], released = (flags & SLOT_RELEASED) !== 0, def = history.plan.graph[n];
	const parent = history.parent[s];
	push( values, history.serials[s] );
	push( values, history.birth[s] );
	push( values, history.clockBirth[s] );
	push( values, history.age[s] );
	push( values, flags & SLOT_ALIVE ? 1 : 0 );
	push( values, released ? 1 : 0 );
	push( values, history.children[s] );
	push( values, history.link[s] );
	push( values, released ? RELEASED : history.serials[parent] );
	if ( !def?.emission ) {
		push( values, NONE );
		push( values, NONE );
	} else if ( released ) {
		push( values, RELEASED );
		push( values, RELEASED );
	} else {
		push( values, history.serials[parent] );
		push( values, history.groupTotals[groupEntry( history, n, parent )] );
	}
	pushList( values, history.previousPosition.subarray( s * 3, s * 3 + 3 ) );
	pushList( values, history.origin.subarray( s * 3, s * 3 + 3 ) );
	pushNarrow( values, history.matrix.subarray( s * 16, s * 16 + 16 ) );
	pushNarrow( values, history.delta.subarray( s * 16, s * 16 + 16 ) );
	pushNarrow( values, history.local.subarray( s * 16, s * 16 + 16 ) );
	pushNarrow( values, history.shape.subarray( s * 16, s * 16 + 16 ) );
	push( values, history.frames[s] );
	pushList( values, history.position.subarray( s * 3, s * 3 + 3 ) );
	pushList( values, history.velocity.subarray( s * 3, s * 3 + 3 ) );
	pushList( values, history.scale.subarray( s * 3, s * 3 + 3 ) );
	pushNarrow( values, history.rotation.subarray( s * 16, s * 16 + 16 ) );
	// A graph element has no angular velocity.
	pushNarrow( values, undefined );
	if ( !drawn ) return;
	if ( !(flags & SLOT_SNAPSHOT) ) {
		push( values, NONE );
		return;
	}
	const entry = history.plan.snapshots[n] + s - history.plan.base[n];
	pushList( values, history.previousScale.subarray( entry * 3, entry * 3 + 3 ) );
	pushNarrow( values, history.previousRotation.subarray( entry * 16, entry * 16 + 16 ) );
	pushNarrow( values, history.previousMatrix.subarray( entry * 16, entry * 16 + 16 ) );
}

/*
================
groupEntry

The groupTotals entry of emitter n's group that parent slot parent leads.
================
*/
function groupEntry( history, n, parent ) {
	const plan = history.plan, up = plan.graph[n].parent;
	return plan.groups[n] + (up < 0 ? 0 : parent - plan.base[up]);
}

/*
================
pushRecord

The tick record of live slot s, emitter n's slot b: the one the tick
wrote, or for an emitter only a ribbon draws, the one writeGraphRecord
makes of it.
================
*/
function pushRecord( values, history, n, b, s, records ) {
	const plan = history.plan, first = plan.records[n];
	if ( first >= 0 ) {
		pushNarrow(
			values,
			history.records.subarray( (first + b) * PARTICLE_RECORD, (first + b + 1) * PARTICLE_RECORD )
		);
		return;
	}
	const snapshot = history.flags[s] & SLOT_SNAPSHOT ? plan.snapshots[n] + b : -1;
	records.fill( 0 );
	writeGraphRecord( records, 0, history, s, snapshot, history.link[s] );
	pushNarrow( values, records );
}

/*
================
pushGraph

The graph state after one call, and the tick record of every live slot of
a drawn emitter.
================
*/
function pushGraph( values, history, records ) {
	const plan = history.plan, graph = plan.graph;
	push( values, history.frame );
	push( values, history.startTick );
	push( values, history.index );
	push( values, history.serial );
	pushElement( values, history, ROOT_SLOT, -1, false );
	for ( let n = 0; n < graph.length; n++ ) {
		const def = graph[n], base = plan.base[n], count = history.counts[n], drawn = plan.draws[n] !== 0;
		push( values, count );
		for ( let b = 0; b < count; b++ ) {
			const s = base + b, flags = history.flags[s];
			if ( !(flags & SLOT_OCCUPIED) ) push( values, EMPTY );
			else pushElement( values, history, s, n, drawn );
			if ( flags & SLOT_ALIVE && drawn ) pushRecord( values, history, n, b, s, records );
		}
		pushList( values, def.emission ? history.birth.subarray( base, base + count ) : def.births );
		const parents = def.emission ? count : 0;
		push( values, parents );
		for ( let b = 0; b < parents; b++ ) {
			const s = base + b;
			push( values, history.flags[s] & SLOT_RELEASED ? RELEASED : history.serials[history.parent[s]] );
		}
		if ( !def.emission ) {
			// A fixed-birth root emitter has the holder's group, which never emits.
			push( values, def.parent < 0 ? 1 : 0 );
			if ( def.parent < 0 ) {
				push( values, 0 );
				push( values, 0 );
			}
			continue;
		}
		const up = def.parent, groups = up < 0 ? 1 : history.orderCounts[up];
		push( values, groups );
		for ( let k = 0; k < groups; k++ ) {
			const parent = up < 0 ? ROOT_SLOT : history.orders[plan.orders[up] + k];
			push( values, history.serials[parent] );
			push( values, history.groupTotals[groupEntry( history, n, parent )] );
		}
	}
}

/*
================
laneBytes
================
*/
function laneBytes( lane, used ) {
	return new Uint8Array( lane.buffer, 0, used * lane.BYTES_PER_ELEMENT );
}

/*
================
scenarioDigest

The digest of one effect's graph through one scenario. A call that runs
no tick and shifts nothing adds only the clock and the cursor: the graph
changes only at a tick or a region shift, and serializing an unchanged
graph again was most of the cost (two of three 60 Hz calls run no tick).
A state change such a call made would still reach the next tick's values.
================
*/
function scenarioDigest( model, path, scenario, table, values ) {
	const graph = model.particleGraph, hash = createHash( "sha256" );
	const plan = createParticleGraphPlan( graph, model.primitives );
	const history = createParticleGraph( graph, pathCursor( path, table.length ), plan );
	const records = new Float32Array( PARTICLE_RECORD ), transform = new Float32Array( 16 );
	for ( const call of scenario.calls ) {
		const frame = history.frame;
		values.wideLength = values.narrowLength = 0;
		try {
			advanceParticleGraph(
				history,
				graph,
				call.time,
				holderMatrix( transform, call.yaw, call.x, call.y, call.z ),
				table,
				scenario.stop,
				call.shiftX,
				0,
				scenario.keepRoots
			);
		} catch ( error ) {
			hash.update( "throw " + (error instanceof Error ? error.message : String( error )) );
			break;
		}
		if ( history.frame === frame && call.shiftX === 0 ) {
			push( values, history.frame );
			push( values, history.startTick );
			push( values, history.index );
		} else pushGraph( values, history, records );
		hash.update( laneBytes( values.wide, values.wideLength ) );
		hash.update( laneBytes( values.narrow, values.narrowLength ) );
	}
	return hash.digest( "hex" ).slice( 0, DIGEST_DIGITS );
}

/*
================
particleTickDigests

{ path: "S1 S2 S3" } for every catalogue effect with a particle graph, in
catalogue order.
================
*/
export function particleTickDigests() {
	const bytes = readPublishedAssetBytesSync( "/assets/effects/programs.json", CLIENT_PUBLIC_ROOT );
	const catalog = JSON.parse( new TextDecoder().decode( bytes ) );
	const decoder = createEffectPrograms(), table = particleRandomTable(), values = createValues();
	const scenarios = SCENARIOS.map( scenarioCalls ), digests = {};
	for ( const path of Object.keys( catalog.effects ) ) {
		const model = decoder.decode( bytes, path ).model;
		if ( !model.particleGraph?.length ) continue;
		digests[path] = scenarios.map( scenario => scenarioDigest( model, path, scenario, table, values ) ).join( " " );
	}
	return digests;
}

/*
================
digestDifferences

The effects whose digest differs from the fixture's (expected). The
fixture is the native catalogue's: on a native build every effect of
either side counts, so an added or lost effect fails as a changed one
does. An expanded build (expanded-build.mjs) publishes effects the
native catalogue lacks; there only the recorded effects count, and each
must still be present with its digest (#3596's rule, as #3618 applies it
to the census). The 1901fa53c release's build:beta failed on 87 such
additions while all 1,043 recorded effects matched.
================
*/
export function digestDifferences( expected, actual, expanded ) {
	const paths = expanded ? Object.keys( expected ) : Object.keys( { ...expected, ...actual } );
	return paths.filter( path => expected[path] !== actual[path] );
}
