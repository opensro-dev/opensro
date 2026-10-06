/*
===========================================================================

particle-streams.test.mjs - incremental graph records across actor lifetimes

Uses real graph ticks and copies only dirty records, as the GPU uploader does.
Unused capacity must stay empty; replacing a row must retire its old tail.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createParticleGraph, advanceParticleGraph } = await import(
	"../../src/engine/foundation/animation/particle-graph.ts"
);
const {
	beginParticleFrame,
	createParticleStream,
	endParticleFrame,
	writeParticleRow
} = await import( "../../src/engine/runtime/renderer/characters/particle-streams.ts" );
const { PARTICLE_RECORD, recordLive } = await import( "../../src/engine/foundation/animation/particle-records.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

const CAPACITY = 1024;

/*
================
fixture
================
*/
function fixture() {
	const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const graph = [ {
		parent: -1,
		parents: [ 0, 0, 0 ],
		births: [ 0, 1, 2 ],
		frames: 4,
		keepMatrix: true,
		keepOrigin: true,
		positionDepth: 0,
		matrixDepth: 0,
		velocityDepth: 0,
		followDepth: 0,
		scales: [],
		positions: [],
		rotations: []
	} ];
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [],
		particleGraph: graph,
		primitives: [ {
			name: "emitter",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity,
			image: -1,
			particleEmitter: 0,
			emission: { capacity: CAPACITY, births: [], lifetime: 1 },
			geometry: { positions: new Float32Array(), indices: new Uint32Array(), transform: identity }
		} ]
	};
	const stream = createParticleStream( model, 0, 1 );
	const gpuRecords = new Float32Array( stream.records.length );
	const random = { table: Float32Array.of( .25, .5, .75 ), index: 0 };
	const history = () => ({ graph: createParticleGraph( graph, 0 ), matrices: [], programs: [] });
	/*
	================
	frame
	================
	*/
	function frame( owner, time, origin = 1, active = true ) {
		advanceParticleGraph( owner.graph, graph, time, identity, random.table );
		beginParticleFrame( stream, undefined, Number( active ) );
		if ( active ) {
			writeParticleRow( stream, 0, {
				actor: {
					gid: 1,
					model: "effect",
					pose: { regionId: origin, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
					clip: "effect",
					time,
					loop: false,
					scale: 1
				},
				history: owner,
				pose: undefined,
				opacity: 1,
				origin
			}, random );
		}
		const start = stream.dirtyStart, end = stream.dirtyEnd;
		if ( start < end ) {
			gpuRecords.set(
				stream.records.subarray( start * PARTICLE_RECORD, end * PARTICLE_RECORD ),
				start * PARTICLE_RECORD
			);
		}
		endParticleFrame( stream );
		const live =
			Array.from( { length: CAPACITY }, ( _, i ) => recordLive( gpuRecords, i ) ).filter( Boolean ).length;
		assert.equal( live, stream.live, "uploaded visibility matches the frame's live count" );
		return Math.max( 0, end - start );
	}
	return { history, frame, stream };
}

test("graph uploads used slots and retains live counts between ticks", () => {
	const f = fixture(), owner = f.history();
	assert.equal( f.frame( owner, 0 ), CAPACITY, "initial upload initializes all GPU storage" );
	assert.equal( f.frame( owner, .01 ), 0 );
	assert.equal( f.stream.live, 1 );
	assert.equal( f.frame( owner, .05 ), 2 );
	assert.equal( f.frame( owner, .06 ), 0 );
	assert.equal( f.stream.live, 2 );
	assert.equal( f.frame( owner, .1 ), 3 );
	assert.equal( f.frame( owner, .1, 2 ), 3, "region rebasing invalidates records at the same tick" );
	assert.equal( f.frame( owner, .3, 2 ), 3 );
	assert.equal( f.stream.live, 0 );
});

test("a replacement graph clears its predecessor's tail and a retired row stays empty", () => {
	const f = fixture(), first = f.history(), replacement = f.history();
	f.frame( first, .1 );
	assert.equal( f.stream.live, 3 );
	assert.equal( f.frame( replacement, 0 ), 3 );
	assert.equal( f.stream.live, 1 );
	assert.equal( f.frame( replacement, .01 ), 0 );
	f.frame( replacement, .01, 1, false );
	assert.equal( f.stream.live, 0 );
	f.frame( replacement, .01 );
	assert.equal( f.stream.live, 1 );
});
