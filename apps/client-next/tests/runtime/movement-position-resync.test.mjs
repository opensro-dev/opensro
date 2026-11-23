/*
===========================================================================

movement-position-resync.test.mjs - the 5 s resync mid-walk keeps the walk

The server's 0x3028 position resync (#3723, position_resync.go) reaches
the character itself every 5 s while it runs. It is a source reseed:
authoritative progress while travel remains active, never a stop. On the
local player it went through the stop correction (movement.correct), so
an answered walk froze where the resync landed and the next resync, 5 s
later, slid the character to the goal the server had walked it to.

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
const GID = 7;
const start = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const goal = { ...start, x: 400 };
// The run speed (units/s) and the worker step.
const RUN_SPEED = 50;
const STEP_MS = 16;
// The walk lasts 6 s at RUN_SPEED; the resync lands 2 s in.
const WALK_MS = 6000;
const RESYNC_AT_MS = 2000;

/*
================
receipt

The server's acceptance of command id: it walks from start to goal.
================
*/
function receipt( id ) {
	return new TextEncoder().encode( JSON.stringify( {
		v: 1,
		id,
		gid: GID,
		accepted: true,
		serverTimeMs: 0,
		world: { spawn: goal, moveSegment: { from: start, startedAtMs: 0, arrivesAtMs: WALK_MS } }
	} ) );
}

/*
================
walking

A movement owner running an answered walk from start to goal.
================
*/
function walking() {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	m.request( goal, 0 );
	m.receive( receipt( 1 ), 0, GID );
	for ( let now = STEP_MS; now <= RESYNC_AT_MS; now += STEP_MS ) m.step( now );
	return m;
}

test("a position resync mid-walk reseeds the walk and it still reaches its goal", () => {
	const m = walking();
	const x = defined( m.state().pose ).x;
	assert.ok( x > start.x + 50 && x < goal.x - 50, `mid-walk at the resync (x ${x})` );
	// The server's own progress, a little behind the local walk.
	m.correct( { ...start, x: Math.round( x ) - 4 }, RESYNC_AT_MS, true );
	m.step( RESYNC_AT_MS + STEP_MS );
	assert.ok( defined( m.state().pose ).x > x - 4, "the walk goes on from the resynced pose" );
	for ( let now = RESYNC_AT_MS + STEP_MS; now <= WALK_MS + 1000; now += STEP_MS ) m.step( now );
	assert.ok(
		Math.abs( defined( m.state().pose ).x - goal.x ) < 1,
		`the walk reaches its goal (x ${m.state().pose?.x})`
	);
});

test("a position resync while standing places the character, as a correction does", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	m.correct( { ...start, x: 130 }, 100, true );
	for ( let now = 100 + STEP_MS; now <= 2000; now += STEP_MS ) m.step( now );
	assert.equal( defined( m.state().pose ).x, 130 );
});

test("a stop correction mid-walk still stops the walk", () => {
	const m = walking();
	const x = Math.round( defined( m.state().pose ).x );
	m.correct( { ...start, x }, RESYNC_AT_MS );
	for ( let now = RESYNC_AT_MS + STEP_MS; now <= WALK_MS + 1000; now += STEP_MS ) m.step( now );
	assert.equal( defined( m.state().pose ).x, x, "B2F5 halts the mover (775B50)" );
});
