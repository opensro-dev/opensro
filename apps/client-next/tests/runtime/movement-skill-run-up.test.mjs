/*
===========================================================================

movement-skill-run-up.test.mjs - a skill press's predicted run-up

An admitted press on a target beyond reach walks the caster into reach on
the server (58D8F0). The client starts that walk at the press
(predictApproach with approach) so it is not one delivery behind the
server when the cast begins. The server's walk stops at its own combat
spacing, short of the predicted goal, and is adopted by heading; a refused
press walks back to the start the server kept.

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
const GID = 7;
// The run speed (units/s), the worker step and the one-way delivery.
const RUN_SPEED = 50;
const STEP_MS = 16;
const LATENCY_MS = 150;

/*
================
walkTo

The server's unsourced walk of the local player toward x along z = 100.
================
*/
function walkTo( x ) {
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( GID );
	p[4] = 1;
	p.writeUInt16LE( 257, 5 );
	p.writeInt16LE( x, 7 );
	p.writeInt16LE( 0, 9 );
	p.writeInt16LE( 100, 11 );
	return p;
}

/*
================
pressed

A caster at rest that pressed a skill on a target 300 units east at time 0
and ran its predicted run-up until the server's answer, one round trip on.
================
*/
function pressed() {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	assert.ok( m.predictApproach( { ...start, x: 400 }, 0, true ) );
	for ( let now = STEP_MS; now <= 2 * LATENCY_MS; now += STEP_MS ) m.step( now );
	return m;
}

/*
================
x
================
*/
function x( m ) {
	return defined( m.state().pose ).x;
}

test("the server's shorter run-up is adopted without stepping back", () => {
	const m = pressed(), progressed = x( m );
	assert.ok( progressed > start.x + RUN_SPEED * 0.25, `run-up began at the press (x ${progressed})` );
	assert.equal( m.approachPredictedSince(), 0 );
	// The server stops at its combat spacing, short of the predicted goal.
	m.native( walkTo( 380 ), 2 * LATENCY_MS, GID );
	assert.equal( m.approachPredictedSince(), undefined );
	assert.ok( Math.abs( x( m ) - progressed ) < 1, "adoption kept the progress made in flight" );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 8000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 380 ) < 1, `ends at the server's goal (x ${x( m )})` );
});

test("a server walk the other way is not the run-up", () => {
	const m = pressed();
	m.native( walkTo( 20 ), 2 * LATENCY_MS, GID );
	assert.equal( m.approachPredictedSince(), undefined );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 8000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 20 ) < 1, `follows the server's walk (x ${x( m )})` );
});

test("a refused press walks the run-up back to the start", () => {
	const m = pressed();
	m.castRefused( 2 * LATENCY_MS );
	assert.equal( m.approachPredictedSince(), undefined );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 4000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - start.x ) < 1, `back at the start (x ${x( m )})` );
});

test("a cast without a walk ends the run-up at the start", () => {
	const m = pressed();
	m.endPrediction( 2 * LATENCY_MS );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 4000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - start.x ) < 1, `back at the start (x ${x( m )})` );
});

test("a pickup prediction is not a skill run-up", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	assert.ok( m.predictApproach( { ...start, x: 400 }, 0 ) );
	assert.equal( m.approachPredictedSince(), undefined );
});
