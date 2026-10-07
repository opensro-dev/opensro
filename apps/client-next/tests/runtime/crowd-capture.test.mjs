/*
===========================================================================

crowd-capture.test.mjs - recorded inputs retain their values and lifetimes

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import {
	createCrowdCapture,
	decodeCrowdValue,
	encodeCrowdValue,
	installCrowdCapture
} from "../../tools/perf/core/crowd-capture.mjs";

/*
================
actor
================
*/
function actor() {
	return {
		gid: 1,
		model: "/assets/body.glb",
		pose: { x: 10, y: -0, z: 20 },
		optional: undefined,
		attachment: { rotation: new Float32Array( [ 1, -0, .25 ] ) },
		layers: [ { weight: .5 } ]
	};
}

test("capture copies mutable actors and preserves typed/optional values through JSON", () => {
	const input = actor();
	const expected = structuredClone( input );
	const capture = createCrowdCapture( { seed: 42 } );
	capture.append( { atMs: 100, actors: [ input ] } );
	input.pose.x = 90;
	input.attachment.rotation[0] = 7;
	input.layers[0].weight = 0;
	const saved = JSON.parse( JSON.stringify( capture.finish() ) );
	assert.deepEqual( decodeCrowdValue( saved.frames[0] ).actors, [ expected ] );
	assert.equal( saved.reason, "stopped" );
});

test("limits stop before a partial frame and clocks cannot reverse", () => {
	const frame = { atMs: 1, actors: [ actor() ] };
	const count = createCrowdCapture( {}, { maxFrames: 1 } );
	assert.equal( count.append( frame ), true );
	assert.equal( count.append( { ...frame, atMs: 2 } ), false );
	assert.equal( count.finish().reason, "frame-limit" );
	const bytes = createCrowdCapture( {}, { maxBytes: 4 } );
	assert.equal( bytes.append( frame ), false );
	assert.equal( bytes.finish().frames.length, 0 );
	assert.equal( bytes.finish().reason, "byte-limit" );
	const clock = createCrowdCapture( {} );
	clock.append( frame );
	assert.throws( () => clock.append( frame ), /clock/ );
	assert.throws( () => clock.append( { ...frame, atMs: 0 } ), /clock/ );
});

test("unsupported input fails explicitly instead of becoming a different value", () => {
	for ( const input of [ NaN, Infinity, new Map(), new Date(), new Uint8Array( 1 ), { $crowd: "undefined" } ] ) {
		assert.throws( () => encodeCrowdValue( input ) );
	}
	const cycle = {};
	cycle.self = cycle;
	assert.throws( () => encodeCrowdValue( cycle ), /nesting/ );
	for ( const value of [ "1", null, 1e100 ] ) {
		assert.throws( () => decodeCrowdValue( { $crowd: "float32", values: [ value ] } ), /float32/ );
	}
});

test("observer forwards calls, skips missing clocks, and restores original hooks", () => {
	const seen = [];
	const probe = {
		begin( id ) {
			seen.push( [ "begin", id ] );
		},
		movement( sample ) {
			seen.push( [ "movement", sample.atMs ] );
		},
		end() {
			seen.push( [ "end" ] );
		}
	};
	const original = { ...probe };
	const target = /** @type {any} */ ({
		__worldProbeFrameProfiler: probe,
		__benchRuntime: {
			characterActors: () => [ actor() ],
			camera: () => ({ yaw: 1 })
		}
	});
	const capture = installCrowdCapture( {}, {}, target );
	probe.begin( 1 );
	probe.end();
	probe.begin( 2 );
	probe.movement( { atMs: 250 } );
	probe.end();
	const saved = capture.finish();
	assert.equal( saved.failure, null );
	assert.equal( saved.frames.length, 1 );
	assert.equal( decodeCrowdValue( saved.frames[0] ).atMs, 250 );
	assert.deepEqual( seen, [ [ "begin", 1 ], [ "end" ], [ "begin", 2 ], [ "movement", 250 ], [ "end" ] ] );
	assert.deepEqual( probe, original );
	assert.equal( target.__crowdCapture, undefined );
});
