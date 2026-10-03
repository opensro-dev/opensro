/*
===========================================================================

movement-cast-hold.test.mjs - a targeted command stops the local walk

The server settles its walk where a targeted skill or attack finds it and
corrects the player there (B2F5). The local walk stops at the same point
when the command is sent, so the correction no longer pulls the player back
by speed x RTT. A hold the server never settles (a refused command, or no
answer) rejoins the server's walk without a jump.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const start = { regionId: 257, x: 100, y: 10, z: 100, angle: 0 };
const GID = 7;

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
	p.writeInt16LE( 10, 9 );
	p.writeInt16LE( 100, 11 );
	return p;
}

// The local run speed, and the catch-up cap a rejoin may not exceed.
const RUN_SPEED = 50;
const CATCHUP_SPEED_FACTOR = 1.3;
const STEP_MS = 16;

/*
================
stepsUntil

Steps m from `from` to `to` and returns the largest distance it moved in
one step: a jump shows as one step far longer than walking allows.
================
*/
function stepsUntil( m, from, to ) {
	let largest = 0, previous = defined( m.state().pose ).x;
	for ( let now = from; now <= to; now += STEP_MS ) {
		m.step( now );
		const x = defined( m.state().pose ).x;
		largest = Math.max( largest, Math.abs( x - previous ) );
		previous = x;
	}
	return largest;
}

/*
================
walking

A local player 1 s into a run at 50 units/s (x = 150).
================
*/
function walking() {
	const m = createMovement( () => {} );
	m.seed( start );
	m.native( walkTo( 1500 ), 0, GID );
	for ( let now = 16; now <= 1000; now += 16 ) m.step( now );
	m.step( 1000 );
	return m;
}

test("a held walk stays where the command left it until the settle lands there", () => {
	const m = walking();
	m.holdForCast( 1000 );
	const held = defined( m.state().pose ).x;
	assert.ok( Math.abs( held - 150 ) < 1e-6, "held at " + held );
	// A round trip later the walk would have run on to x = 165.
	for ( let now = 1016; now <= 1300; now += 16 ) m.step( now );
	assert.equal( defined( m.state().pose ).x, held, "the held walk kept moving" );
	m.correct( { ...start, x: 150 }, 1300 );
	m.step( 1316 );
	assert.equal( defined( m.state().pose ).x, 150, "the settle moved the player" );
	for ( let now = 1332; now <= 4000; now += 16 ) m.step( now );
	assert.equal( defined( m.state().pose ).x, 150, "a settled hold resumed walking" );
});

test("a hold the server never settles rejoins the server's walk without a jump", () => {
	const m = walking();
	m.holdForCast( 1000 );
	// The hold lapses at 2.5 s with the server at x = 225, the player at 150.
	const largest = stepsUntil( m, 1016, 28016 );
	assert.ok( largest <= RUN_SPEED * CATCHUP_SPEED_FACTOR * STEP_MS / 1000 + 1e-6, "jumped " + largest );
	// The server's run ends at x = 1500 after 28 s; the player arrives with it.
	assert.ok( Math.abs( defined( m.state().pose ).x - 1500 ) < 1e-6, "arrived at " + m.state().pose?.x );
});

test("a refused command rejoins the server's walk at once, without a jump", () => {
	const m = walking();
	m.holdForCast( 1000 );
	// B245 [2, 0x3004] one round trip later: no MP, the server never stopped.
	stepsUntil( m, 1016, 1100 );
	m.castRefused( 1100 );
	const largest = stepsUntil( m, 1116, 28016 );
	assert.ok( largest <= RUN_SPEED * CATCHUP_SPEED_FACTOR * STEP_MS / 1000 + 1e-6, "jumped " + largest );
	assert.ok( Math.abs( defined( m.state().pose ).x - 1500 ) < 1e-6, "arrived at " + m.state().pose?.x );
});

test("a refusal after the server arrived walks the rest of the way", () => {
	const m = createMovement( () => {} );
	m.seed( start );
	// A 2 s run to x = 200; held at x = 150 after 1 s.
	m.native( walkTo( 200 ), 0, GID );
	for ( let now = 16; now <= 1000; now += 16 ) m.step( now );
	m.holdForCast( 1000 );
	m.castRefused( 3000 );
	m.step( 3000 );
	assert.equal( defined( m.state().pose ).x, 150, "the refusal teleported the player" );
	const largest = stepsUntil( m, 3016, 5000 );
	assert.ok( largest <= RUN_SPEED * CATCHUP_SPEED_FACTOR * STEP_MS / 1000 + 1e-6, "jumped " + largest );
	assert.ok( Math.abs( defined( m.state().pose ).x - 200 ) < 1e-6, "stopped at " + m.state().pose?.x );
});

test("a refusal without a hold changes nothing", () => {
	const m = walking();
	m.castRefused( 1000 );
	const largest = stepsUntil( m, 1016, 1500 );
	assert.ok( largest <= RUN_SPEED * STEP_MS / 1000 + 1e-6, "a running walk was disturbed: " + largest );
});

test("a hold without a walk does nothing", () => {
	const m = createMovement( () => {} );
	m.seed( start );
	m.holdForCast( 0 );
	m.step( 5000 );
	assert.deepEqual( m.state().pose, { ...start } );
});
