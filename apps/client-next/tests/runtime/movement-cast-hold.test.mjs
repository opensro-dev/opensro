/*
===========================================================================

movement-cast-hold.test.mjs - a targeted command stops the local walk

The server settles its walk where a targeted skill or attack finds it and
corrects the player there (B2F5). The local walk stops at the same point
when the command is sent, so the correction no longer pulls the player back
by speed x RTT. A hold the server never settles follows its walk again.

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

The server's walk of the local player toward x along z = 100 (0xB021).
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

test("a hold the server never settles follows the server's walk again", () => {
	const m = walking();
	m.holdForCast( 1000 );
	for ( let now = 1016; now <= 3000; now += 16 ) m.step( now );
	// The server walked on: 3 s at 50 units/s from x = 100.
	assert.ok( Math.abs( defined( m.state().pose ).x - 250 ) < 1e-6, "lapsed at " + m.state().pose?.x );
});

test("a hold without a walk does nothing", () => {
	const m = createMovement( () => {} );
	m.seed( start );
	m.holdForCast( 0 );
	m.step( 5000 );
	assert.deepEqual( m.state().pose, { ...start } );
});
