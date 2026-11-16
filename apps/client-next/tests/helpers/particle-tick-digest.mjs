/*
===========================================================================

particle-tick-digest.mjs - a digest of every particle graph tick in the catalogue

The oracle for changes to the particle graph tick
(src/engine/foundation/animation/particle-graph.ts) that must not change
its results: every effect of /assets/effects/programs.json with a particle
graph runs through three scenarios, and after every call a sha256 takes in
the whole graph state (clock, random cursor, root, every slot below each
emitter's row length with its links, group totals in group order) and the
tick record writeGraphRecord makes of every live slot of a drawn emitter
(a call that runs no tick and shifts nothing adds the clock and cursor
only; scenarioDigest says why). Each (scenario, effect) keeps the first
16 hex digits. Three mutations of the tick, each alone, change hundreds
of effects' digests: one fround dropped in linkElement, the emission
groups walked in reverse, one random draw not consumed in commands.

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
const { createParticleGraph, advanceParticleGraph } = await load( "foundation/animation/particle-graph.ts" );
const { particleRandomTable } = await load( "foundation/animation/particle-program.ts" );
const { writeGraphRecord, PARTICLE_RECORD } = await load( "foundation/animation/particle-records.ts" );
const { ROTATION_WORK } = await load( "foundation/animation/particle-presentation.ts" );

export const DIGEST_PATH = new URL( "../fixtures/particle-tick-digest.json", import.meta.url );
export const SCENARIOS = [ "S1", "S2", "S3" ];

// Hex digits kept of each (scenario, effect) sha256.
const DIGEST_DIGITS = 16;
// The holder's region shift in S2, and when it happens.
const REGION_SHIFT = -1920;
const SHIFT_SECONDS = 10;
// Marks in the value stream: an empty slot, a missing value.
const EMPTY = -1e300;
const NONE = -2e300;

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

Every field of one element; parents and groups by serial (the root is 0).
================
*/
function pushElement( values, element ) {
	const state = element.state, previous = state.previous;
	push( values, element.serial );
	push( values, element.born );
	push( values, element.clockBirth );
	push( values, element.age );
	push( values, element.alive ? 1 : 0 );
	push( values, element.released ? 1 : 0 );
	push( values, element.children );
	push( values, element.rootLink );
	push( values, element.parent.serial );
	push( values, element.group ? element.group.parent.serial : NONE );
	push( values, element.group ? element.group.total : NONE );
	pushList( values, element.previousPosition );
	pushList( values, element.origin );
	pushNarrow( values, element.matrix );
	pushNarrow( values, element.delta );
	pushNarrow( values, element.local );
	pushNarrow( values, element.shape );
	push( values, state.frame );
	pushList( values, state.position );
	pushList( values, state.velocity );
	pushList( values, state.scale );
	pushNarrow( values, state.rotation );
	pushNarrow( values, state.angularVelocity );
	if ( !previous ) push( values, NONE );
	else {
		pushList( values, previous.position );
		pushList( values, previous.scale );
		pushNarrow( values, previous.rotation );
		pushNarrow( values, previous.matrix );
	}
}

/*
================
pushGraph

The graph state after one call, and the tick record of every live slot of
a drawn emitter.
================
*/
function pushGraph( values, history, drawn, link, records, work ) {
	push( values, history.frame );
	push( values, history.startTick );
	push( values, history.index );
	push( values, history.serial );
	pushElement( values, history.root );
	for ( let n = 0; n < history.elements.length; n++ ) {
		const rows = history.elements[n];
		push( values, rows.length );
		for ( let b = 0; b < rows.length; b++ ) {
			const element = rows[b];
			if ( !element ) push( values, EMPTY );
			else pushElement( values, element );
			if ( element?.alive && drawn[n] ) {
				records.fill( 0 );
				writeGraphRecord( records, 0, element, work, link );
				pushNarrow( values, records );
			}
		}
		pushList( values, history.births[n] );
		const parents = history.parents[n];
		push( values, parents.length );
		for ( let b = 0; b < parents.length; b++ ) push( values, parents[b] ? parents[b].serial : EMPTY );
		push( values, history.groups[n].size );
		for ( const group of history.groups[n].values() ) {
			push( values, group.parent.serial );
			push( values, group.total );
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
	const drawn = graph.map( ( _, n ) => model.primitives.some( p => p.particleEmitter === n ) );
	const history = createParticleGraph( graph, pathCursor( path, table.length ) );
	const link = {
		root: history.root,
		follow: new Float32Array( 20 ),
		turned: new Float32Array( 16 ),
		shift: [ 0, 0, 0 ]
	};
	const records = new Float32Array( PARTICLE_RECORD ),
		work = new Float64Array( ROTATION_WORK ),
		transform = new Float32Array( 16 );
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
		} else pushGraph( values, history, drawn, link, records, work );
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
