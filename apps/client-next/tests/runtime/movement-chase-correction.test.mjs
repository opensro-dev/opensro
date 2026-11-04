/*
===========================================================================

movement-chase-correction.test.mjs - a server stop correction mid-chase

BR-261031-1337-33A7 (#2368): the report shows 55 server corrections of
5-19 units while the reporter holds a full-HP target with zero casts and
one denied skill press. The press goes out in "approach" mode and the
client predicts the run-up locally (predictApproach); the cast fires only
when the approach settles in range. A B2F5 stop correction landing
mid-chase passes through movement.correct, which must not cancel the
predicted approach outright: the chase has no pending click-walk request
to resume from, and gameplay drops the chase the moment
approachPredictedSince() goes undefined, so nothing ever re-aims the leg
and the cast never sends.

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
// The run speed (units/s) and the worker step.
const RUN_SPEED = 50;
const STEP_MS = 16;

/*
================
x
================
*/
function x( m ) {
	return defined( m.state().pose ).x;
}

test("a stop correction mid-chase does not cancel the predicted approach", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	// The leg stops at x 176 (admission 24 short of a target at x 200).
	assert.ok( m.predictApproach( { ...start, x: 176 }, 0, true ) );
	for ( let now = STEP_MS; now <= 160; now += STEP_MS ) m.step( now );
	const progressed = x( m );
	// The server's stop correction pulls the pose back (5-19 units in the
	// report), mid-chase, before range is reached.
	m.correct( { ...start, x: Math.round( progressed ) - 8 }, 160 );
	assert.notEqual(
		m.approachPredictedSince(),
		undefined,
		"the predicted approach survives a mid-chase correction"
	);
	// Stepping on must still walk the leg to its stop point.
	for ( let now = 160 + STEP_MS; now <= 5000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 176 ) < 1, `the leg still reaches its stop point (x ${x( m )})` );
});
