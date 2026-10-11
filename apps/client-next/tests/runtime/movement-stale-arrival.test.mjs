/*
===========================================================================

movement-stale-arrival.test.mjs - a stop that crosses the next walk

The client leads its own walks by one delivery, so it can arrive, click on
and send the next walk before the server's stop for the first one reaches
it. That stop stands where the new walk left from, and the server will walk
the new request from there: the walk keeps its progress. Restarting it from
the stop snapped the player back 27 units in report BR-261010-2006-9E09.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { product } from "../helpers/navigation-fixture.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const start = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const RUN_SPEED = 50;
const STEP_MS = 16;
// The first walk's length in time, and how long the second walked before
// the first's stop arrived.
const FIRST_WALK_MS = 2000;
const CROSSED_MS = 270;

/*
================
crossed

A player that walked 100 units east, arrived, sent a second walk east from
the arrival point and walked it CROSSED_MS before the first walk's stop
came. Returns the movement, the arrival point and the time.
================
*/
function crossed() {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	m.request( { ...start, x: 200 }, 0 );
	let now = 0;
	for ( ; now <= FIRST_WALK_MS + 100; now += STEP_MS ) m.step( now );
	const arrival = defined( m.state().pose );
	assert.ok( Math.abs( arrival.x - 200 ) < 1, `arrived (x ${arrival.x})` );
	m.request( { ...start, x: 400 }, now );
	const sent = now;
	for ( ; now <= sent + CROSSED_MS; now += STEP_MS ) m.step( now );
	return { m, arrival, now };
}

test("the previous walk's stop at the new walk's start keeps its progress", () => {
	const { m, arrival, now } = crossed();
	const before = defined( m.state().pose ).x;
	assert.ok( before > arrival.x + RUN_SPEED * 0.2, `the second walk was under way (x ${before})` );
	m.correct( { ...arrival, x: 200, z: 100 }, now );
	const after = defined( m.state().pose ).x;
	// The correction advances the walk to its own time first: at most one step on.
	assert.ok(
		after >= before && after - before < RUN_SPEED * STEP_MS / 1000 * 2,
		`no snap back (${before} -> ${after})`
	);
	// Within a round trip (the receipt's time) it walks on at run speed.
	for ( let t = now + STEP_MS; t <= now + 500; t += STEP_MS ) m.step( t );
	const walked = defined( m.state().pose ).x - after;
	assert.ok( Math.abs( walked - RUN_SPEED / 2 ) < 2, `walks on at run speed (${walked} in 0.5 s)` );
});

test("a correction anywhere else still moves the player", () => {
	const { m, now } = crossed();
	m.correct( { ...start, x: 150, z: 100 }, now );
	assert.ok( Math.abs( defined( m.state().pose ).x - 150 ) < 0.5, "the correction is authoritative" );
});

test("a stop at a walk's start that is no arrival is not kept", () => {
	// The walk did not leave from an earlier walk's goal, so a stop there is
	// the server holding the player (a root, a stun, a refusal).
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	m.request( { ...start, x: 400 }, 0 );
	let now = 0;
	for ( ; now <= CROSSED_MS; now += STEP_MS ) m.step( now );
	m.correct( { ...start }, now );
	assert.ok( Math.abs( defined( m.state().pose ).x - start.x ) < 0.5, "the stop holds the player" );
});

test("a kept walk the server never walks returns to the stop", () => {
	const { m, now } = crossed();
	m.correct( { ...start, x: 200, z: 100 }, now );
	// No receipt for the kept walk arrives: past a round trip and the grace
	// the player walks back to the stop the server sent.
	for ( let t = now + STEP_MS; t <= now + 3000; t += STEP_MS ) m.step( t );
	assert.ok(
		Math.abs( defined( m.state().pose ).x - 200 ) < 1,
		`back at the stop (x ${defined( m.state().pose ).x})`
	);
});
