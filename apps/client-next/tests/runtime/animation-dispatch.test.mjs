/*
===========================================================================

animation-dispatch.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createAnimationDispatch, animationActivation } = {
	...(await load( "src/engine/foundation/animation/animation-dispatch.ts" )),
	...(await load( "src/engine/foundation/animation/animation-activation.ts" ))
};
const layer = ( activation, time, loop = true, lane = "timed", weight = 1 ) => ({
	activation,
	time,
	loop,
	lane,
	weight,
	clip: "stand"
});
test("movement installation rate advances pose and keyed events together with fractional carry", () => {
	const owner = createAnimationDispatch(),
		activation = animationActivation( 0 ),
		row = { ...layer( activation, 0 ), rate: 1.3 };
	for ( let i = 0; i < 9; i++ ) owner.step( [ row ], 1, () => 1000 );
	const sample = owner.step( [ row ], 1, () => 1000 )[0];
	assert.equal( sample.elapsedMs, 13 );
	assert.equal( sample.layer.time, .013 );
	assert.throws( () => owner.step( [ { ...row, rate: NaN } ], 1, () => 1000 ), /installation/ );
});
test("same clip installations keep distinct cursors and event dispatch precedes timed dispatch", () => {
	const owner = createAnimationDispatch(), a = animationActivation( 0 ), b = animationActivation( 0 );
	const rows = owner.step( [ layer( a, 0 ), layer( b, .1, false, "event" ) ], 100, () => 1000 );
	assert.equal( rows[0].activation, b );
	assert.equal( rows[1].activation, a );
	assert.deepEqual( rows.map( r => r.ranges ), [ [ [ 0, 100 ] ], [ [ 0, 100 ] ] ] );
	const next = owner.step( [ layer( a, 0 ) ], 50, () => 1000 );
	assert.deepEqual( next[0].ranges, [ [ 100, 150 ] ] );
	const returned = owner.step( [ layer( a, 0 ), layer( b, .2, false, "event" ) ], 0, () => 1000 );
	assert.deepEqual( returned[0].ranges, [ [ 0, 200 ] ] );
});
test("event and timed loops both retain complete cycles from their own cursor", () => {
	const owner = createAnimationDispatch(), event = animationActivation( 0 ), timed = animationActivation( 0 );
	const rows = owner.step( [ layer( event, 2.1, true, "event" ), layer( timed, 2.1 ) ], 2100, () => 1000 );
	assert.deepEqual( rows[0].ranges, [ [ 0, 1000 ], [ 0, 1000 ], [ 0, 100 ] ] );
	// AE05D0 accumulates each installation's own cursor; the group clock reaches
	// AE0450 but only a state-0 installation rescales its phase from it.
	assert.deepEqual( rows[1].ranges, [ [ 0, 1000 ], [ 0, 1000 ], [ 0, 100 ] ] );
	const stopped = owner.step( [ layer( event, 2.1, true, "event" ), layer( timed, 2.1 ) ], 0, () => 1000 );
	assert.deepEqual( stopped.map( r => r.ranges ), [ [], [] ] );
	owner.reset();
	assert.deepEqual( owner.step( [ layer( timed, 0 ) ], 100, () => 1000 )[0].ranges, [ [ 0, 100 ] ] );
});
test("finite event ranges clamp and zero-duration installations have no dispatch", () => {
	const owner = createAnimationDispatch(), a = animationActivation( 0 );
	assert.deepEqual( owner.step( [ layer( a, 2, false, "event" ) ], 0, () => 1000 )[0].ranges, [ [ 0, 1000 ] ] );
	assert.deepEqual( owner.step( [ layer( a, 3, false, "event" ) ], 0, () => 1000 )[0].ranges, [] );
	assert.deepEqual( owner.step( [ layer( a, 3 ) ], 0, () => 0 ), [] );
	assert.throws( () => owner.step( [ layer( a, 0 ) ], .1, () => 1000 ), /delta/ );
	assert.throws( () => owner.step( [ layer( a, 0 ), layer( a, 0 ) ], 0, () => 1000 ), /Duplicate/ );
});

test("a run-to-idle blend dispatches only the time that elapsed", () => {
	const owner = createAnimationDispatch(), run = animationActivation( 0 ), idle = animationActivation( 1 );
	const duration = clip => clip === "run" ? 600 : 4000;
	const runLayer = weight => ({ ...layer( run, 0, true, "timed", weight ), clip: "run" });
	const idleLayer = weight => ({ ...layer( idle, 0, true, "timed", weight ), clip: "idle" });
	owner.step( [ runLayer( 1 ) ], 350, duration );
	let dispatched = 0;
	for ( let ms = 4; ms < 200; ms += 4 ) {
		const rows = owner.step( [ runLayer( 1 - ms / 200 ), idleLayer( ms / 200 ) ], 4, duration );
		for ( const row of rows.filter( r => r.activation === run ) ) {
			for ( const [from, to] of row.ranges ) dispatched += to - from;
		}
	}
	// A changing weighted duration must not move the run phase. The outgoing clip
	// travels exactly the 49 steps of 4 ms that elapsed, not a fabricated sweep.
	assert.equal( dispatched, 196 );
});

test("blend weight changes never move an installation phase", () => {
	const owner = createAnimationDispatch(), a = animationActivation( 0 ), b = animationActivation( 0 );
	const duration = clip => clip === "short" ? 1000 : 2000;
	const layers = w => [ { ...layer( a, 0, true, "timed", 1 - w ), clip: "short" }, {
		...layer( b, 0, true, "timed", w ),
		clip: "long"
	} ];
	assert.deepEqual( owner.step( layers( 0 ), 600, duration ).map( r => r.ranges ), [ [ [ 0, 600 ] ], [ [
		0,
		600
	] ] ] );
	// A full weight swing with no elapsed time dispatches nothing at all.
	assert.deepEqual( owner.step( layers( 1 ), 0, duration ).map( r => r.ranges ), [ [], [] ] );
	// Both installations keep advancing in their own time, whatever the weights.
	assert.deepEqual( owner.step( layers( 1 ), 300, duration ).map( r => r.ranges ), [ [ [ 600, 900 ] ], [ [
		600,
		900
	] ] ] );
});

test("a cursor landing exactly on the duration continues without repeating a cycle", () => {
	// AE05F0 wraps on `cursor > length` while AE05BC retains `cursor % length`, so
	// the original dispatches a whole extra cycle when a cursor lands exactly on the
	// length, re-triggering every keyed effect in the clip. It rarely lands there at
	// 60 Hz - a truncated 16 ms delta divides few durations - while a 4 ms delta at
	// 240 Hz divides most, reproducing the rule at a frequency the original never
	// exhibited. Deliberate presentation deviation: wrap on `>=` so the cursor and
	// the retained value stay consistent.
	const owner = createAnimationDispatch(), a = animationActivation( 0 ), d = () => 1000;
	assert.deepEqual( owner.step( [ layer( a, 0 ) ], 996, d )[0].ranges, [ [ 0, 996 ] ] );
	assert.deepEqual( owner.step( [ layer( a, 0 ) ], 4, d )[0].ranges, [ [ 996, 1000 ] ] );
	assert.deepEqual( owner.step( [ layer( a, 0 ) ], 0, d )[0].ranges, [] );
	assert.deepEqual( owner.step( [ layer( a, 0 ) ], 4, d )[0].ranges, [ [ 0, 4 ] ] );
	assert.deepEqual( owner.step( [ layer( a, 0 ) ], 4, d )[0].ranges, [ [ 4, 8 ] ] );
});
