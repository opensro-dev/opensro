/*
===========================================================================

movement-cast-hold.test.mjs - what a command that may stop the walk does to it

The server acts on a command where the command finds it. Which point that is
on the local walk depends on who leads the walk (movement.ts WalkLead):

	client-led  a ground click: the client walks one delivery ahead of the
	            server, so the server acts where the client stood at the
	            press. The walk is held there and the server's settle lands
	            on the held point.
	server-led  a chase: the client walks one delivery behind the server, so
	            the server acts where the client will stand when the answer
	            arrives. The walk goes on, and the settle or the re-planned
	            leg lands under it.

Every expectation below is derived from that timeline with a one-way
delivery of LATENCY_MS, never from what the code happens to do.

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

// The local run speed (units/s), the catch-up cap a lapsed hold may not
// exceed, the worker step and the one-way delivery time of the model.
const RUN_SPEED = 50;
const CATCHUP_SPEED_FACTOR = 1.3;
const STEP_MS = 16;
const LATENCY_MS = 150;
const STEP_UNITS = RUN_SPEED * STEP_MS / 1000;

/*
================
walkTo

The server's walk of the local player toward x along z = 100 (0xB738).
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
x

The local player's x.
================
*/
function x( m ) {
	return defined( m.state().pose ).x;
}

/*
================
stepsUntil

Steps m from `from` to `to` and returns the smallest and largest distance it
moved in one step: a stall shows as a step of zero, a jump as one far longer
than walking allows.
================
*/
function stepsUntil( m, from, to ) {
	let smallest = Infinity, largest = 0, previous = x( m );
	for ( let now = from; now <= to; now += STEP_MS ) {
		m.step( now );
		const moved = Math.abs( x( m ) - previous );
		smallest = Math.min( smallest, moved );
		largest = Math.max( largest, moved );
		previous = x( m );
	}
	return { smallest, largest };
}

/*
================
clickRun

A client-led walk: the player clicked the ground at time 0 and is 1 s into
the run (x = 150). The server starts the same run when the click reaches
it, at LATENCY_MS, so it stands at serverOfClickRun; its receipt arrives
one round trip after the click and keeps the prediction.
================
*/
function clickRun( to = 1500 ) {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.request( { ...start, x: to }, 0 );
	stepsUntil( m, STEP_MS, 2 * LATENCY_MS );
	m.receive( receipt( start, LATENCY_MS, LATENCY_MS, to ), 2 * LATENCY_MS, GID );
	stepsUntil( m, 2 * LATENCY_MS + STEP_MS, 1000 );
	m.step( 1000 );
	return m;
}

/*
================
receipt

The receipt of click 1: the server's segment from `from` (started at
startedAtMs on its clock, sampled at serverTimeMs) to x = to.
================
*/
function receipt( from, serverTimeMs, startedAtMs, to = 1500 ) {
	return new TextEncoder().encode( JSON.stringify( {
		v: 1,
		id: 1,
		gid: GID,
		accepted: true,
		serverTimeMs,
		world: {
			spawn: { ...start, x: to },
			moveSegment: { from, startedAtMs, arrivesAtMs: startedAtMs + (to - from.x) / RUN_SPEED * 1000 }
		}
	} ) );
}

// Where the server stands on the click run at local time now.
const serverOfClickRun = now => 100 + RUN_SPEED * (now - LATENCY_MS) / 1000;

/*
================
chase

A server-led walk: the server started a run at -LATENCY_MS and its leg
reached the client at time 0; the client is 1 s into replaying it (x = 150)
while the server stands at serverOfChase.
================
*/
function chase( to = 1500 ) {
	const m = createMovement( () => {} );
	m.seed( start );
	m.native( walkTo( to ), 0, GID );
	stepsUntil( m, STEP_MS, 1000 );
	m.step( 1000 );
	return m;
}

// Where the server stands on the chase at local time now.
const serverOfChase = now => 100 + RUN_SPEED * (now + LATENCY_MS) / 1000;

// ============================================================================
// Client-led walks: the press holds

test("a press on a click run holds it where the server will stop it", () => {
	const m = clickRun();
	m.holdForCast( 1000 );
	const held = x( m );
	assert.ok( Math.abs( held - 150 ) < 1e-6, "held at " + held );
	// The command reaches the server at 1000 + LATENCY_MS: it stands at 150.
	assert.ok( Math.abs( serverOfClickRun( 1000 + LATENCY_MS ) - held ) < 1e-6 );
	const waiting = stepsUntil( m, 1016, 1000 + 2 * LATENCY_MS );
	assert.equal( waiting.largest, 0, "the held walk kept moving" );
	// The settle arrives one round trip after the press and moves nothing.
	m.correct( { ...start, x: 150 }, 1000 + 2 * LATENCY_MS );
	assert.equal( x( m ), 150, "the settle moved the player" );
	assert.equal( stepsUntil( m, 1316, 4000 ).largest, 0, "a settled hold resumed walking" );
});

test("a hold no answer ever ends rejoins the walk without a jump", () => {
	const m = clickRun();
	m.holdForCast( 1000 );
	// The hold lapses at 2.5 s; the click run ends at x = 1500 after 28 s.
	const { largest } = stepsUntil( m, 1016, 28016 );
	assert.ok( largest <= STEP_UNITS * CATCHUP_SPEED_FACTOR + 1e-6, "jumped " + largest );
	assert.ok( Math.abs( x( m ) - 1500 ) < 1e-6, "arrived at " + x( m ) );
});

test("a refused hold resumes at walking speed, one delivery behind the server", () => {
	const m = clickRun();
	m.holdForCast( 1000 );
	// B245 [2, code] arrives one round trip after the press; the server
	// never stopped.
	const refusedAt = 1000 + 2 * LATENCY_MS;
	stepsUntil( m, 1016, refusedAt );
	m.castRefused( refusedAt );
	let from = refusedAt + STEP_MS;
	for ( const now of [ refusedAt + 32, 2004, 5012, 20004 ] ) {
		const { largest } = stepsUntil( m, from, now );
		assert.ok( largest <= STEP_UNITS + 1e-6, `walked ${largest} in one step before ${now}` );
		m.step( now );
		// Server-led from here: the server's position one delivery ago.
		const replica = serverOfClickRun( now - LATENCY_MS );
		assert.ok( Math.abs( x( m ) - replica ) < 1e-6, `at ${now}: ${x( m )} vs the replica ${replica}` );
		from = now + STEP_MS;
	}
});

test("after a refusal the walk follows the server: a later press keeps walking and its settle lands exactly", () => {
	const m = clickRun();
	m.holdForCast( 1000 );
	m.castRefused( 1000 + 2 * LATENCY_MS );
	stepsUntil( m, 1316, 2000 );
	m.step( 2000 );
	// The second command reaches the server at 2000 + LATENCY_MS and settles
	// it there; the settle arrives one delivery later.
	m.holdForCast( 2000 );
	const settledAt = 2000 + 2 * LATENCY_MS, settle = serverOfClickRun( 2000 + LATENCY_MS );
	const walking = stepsUntil( m, 2016, settledAt );
	assert.ok( walking.smallest > 0, "the walk stopped at the press" );
	m.step( settledAt );
	assert.ok( Math.abs( x( m ) - settle ) < 1e-6, `stood at ${x( m )}, settled at ${settle}` );
	m.correct( { ...start, x: settle }, settledAt );
	assert.ok( Math.abs( x( m ) - settle ) < 1e-6 );
});

test("a refusal after the server arrived walks the rest of the way", () => {
	// A 2 s click run to x = 200; held at x = 150 after 1 s.
	const m = clickRun( 200 );
	m.holdForCast( 1000 );
	m.castRefused( 3000 );
	m.step( 3000 );
	assert.equal( x( m ), 150, "the refusal teleported the player" );
	const { largest } = stepsUntil( m, 3016, 5000 );
	assert.ok( largest <= STEP_UNITS + 1e-6, "jumped " + largest );
	assert.ok( Math.abs( x( m ) - 200 ) < 1e-6, "stopped at " + x( m ) );
});

// ============================================================================
// Server-led walks: the press keeps walking

test("a press during a chase keeps walking and the settle lands under the player", () => {
	const m = chase();
	m.holdForCast( 1000 );
	// The command reaches the server at 1000 + LATENCY_MS, in range: it
	// settles there, and the settle arrives one delivery later.
	const settledAt = 1000 + 2 * LATENCY_MS, settle = serverOfChase( 1000 + LATENCY_MS );
	const walking = stepsUntil( m, 1016, settledAt );
	assert.ok( walking.smallest > 0, "the chase stopped at the press" );
	m.step( settledAt );
	assert.ok( Math.abs( x( m ) - settle ) < 1e-6, `stood at ${x( m )}, settled at ${settle}` );
	m.correct( { ...start, x: settle }, settledAt );
	assert.ok( Math.abs( x( m ) - settle ) < 1e-6, "the settle moved the player" );
});

test("skill spam during a chase never stops the walk", () => {
	const m = chase();
	let previous = x( m );
	for ( let now = 1016; now <= 4000; now += STEP_MS ) {
		// A press about every 100 ms, as in the production recording.
		if ( now % 96 === 8 ) m.holdForCast( now );
		m.step( now );
		const moved = x( m ) - previous;
		assert.ok( Math.abs( moved - STEP_UNITS ) < 1e-6, `moved ${moved} at ${now}` );
		previous = x( m );
	}
	m.step( 4000 );
	// Still the server's position one delivery ago: no walking time was lost.
	assert.ok( Math.abs( x( m ) - serverOfChase( 4000 - LATENCY_MS ) ) < 1e-6, "lagged to " + x( m ) );
});

test("a re-planned chase leg continues from under the player", () => {
	const m = chase();
	m.holdForCast( 1000 );
	// Out of range: the server re-plans from where the command found it and
	// the new leg (no source block) arrives one delivery later.
	const answeredAt = 1000 + 2 * LATENCY_MS;
	stepsUntil( m, 1016, answeredAt );
	m.step( answeredAt );
	const before = x( m );
	assert.ok( Math.abs( before - serverOfChase( 1000 + LATENCY_MS ) ) < 1e-6 );
	m.native( walkTo( 1400 ), answeredAt, GID );
	assert.ok( Math.abs( x( m ) - before ) < 1e-6, "the new leg moved the player" );
	const { smallest, largest } = stepsUntil( m, answeredAt + STEP_MS, 3000 );
	assert.ok( smallest > 0 && largest <= STEP_UNITS + 1e-6, `steps ${smallest}..${largest}` );
});

test("a refusal during a chase changes nothing", () => {
	const m = chase();
	m.holdForCast( 1000 );
	m.castRefused( 1000 + 2 * LATENCY_MS );
	const { smallest, largest } = stepsUntil( m, 1016, 1500 );
	assert.ok( Math.abs( smallest - STEP_UNITS ) < 1e-6 && Math.abs( largest - STEP_UNITS ) < 1e-6 );
});

// ============================================================================
// Receipts decide who leads

/*
================
unansweredClick

A click run 1 s old whose receipt has not arrived yet.
================
*/
function unansweredClick() {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.request( { ...start, x: 1500 }, 0 );
	stepsUntil( m, STEP_MS, 1000 );
	m.step( 1000 );
	return m;
}

test("a receipt that keeps the prediction ahead leaves the walk client-led", () => {
	const m = unansweredClick();
	// The server started at LATENCY_MS; its answer arrives late, at 1 s.
	m.receive( receipt( start, LATENCY_MS, LATENCY_MS ), 1000, GID );
	m.holdForCast( 1300 );
	const held = x( m );
	assert.equal( stepsUntil( m, 1316, 1600 ).largest, 0, "an ahead prediction was not held at " + held );
});

test("a receipt that finds the prediction behind makes the walk server-led", () => {
	const m = unansweredClick();
	// The server is far ahead of the prediction (it stands at x = 400).
	m.receive( receipt( { ...start, x: 400 }, 1000, 1000 ), 1000, GID );
	m.holdForCast( 1016 );
	assert.ok( stepsUntil( m, 1032, 1400 ).smallest > 0, "a walk behind the server was held" );
});

// ============================================================================

test("a refusal without a hold changes nothing", () => {
	const m = clickRun();
	m.castRefused( 1000 );
	const { smallest, largest } = stepsUntil( m, 1016, 1500 );
	assert.ok( Math.abs( smallest - STEP_UNITS ) < 1e-6 && Math.abs( largest - STEP_UNITS ) < 1e-6 );
});

test("a hold without a walk does nothing", () => {
	const m = createMovement( () => {} );
	m.seed( start );
	m.holdForCast( 0 );
	m.step( 5000 );
	assert.deepEqual( m.state().pose, { ...start } );
});
