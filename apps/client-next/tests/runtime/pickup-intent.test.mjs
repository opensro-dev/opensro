/*
===========================================================================

pickup-intent.test.mjs - repeated input through the shipped command owner

Delay server replies while submitting shortcut-equivalent and direct pickup
commands. Only one request may race the grant; explicit replacement and the
authoritative release must leave the next action usable.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
pickupFixture
================
*/
function pickupFixture() {
	/** @type {import('../../src/engine/contracts/world').EntityState} */
	const local = {
		gid: 1,
		refObjId: 1907,
		name: "actor",
		kind: "player",
		regionId: 0x62a8,
		x: 100,
		y: 20,
		z: 100,
		heading: 0
	};
	/** @type {import('../../src/engine/contracts/world').EntityState} */
	const item = { ...local, gid: 7, kind: "ground-item", name: "gold" };
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	return { game, local, item, sent };
}

test("repeated pickup intent is coalesced before reply and during approach", () => {
	const { game, local, item, sent } = pickupFixture();
	const request = { kind: /** @type {const} */ ("pickup"), gid: item.gid };
	assert.ok( game.command( request, 0, item, local ) );
	for ( let i = 1; i < 5; i++ ) assert.equal( game.command( request, i, item, local ), null );
	assert.equal( sent.length, 1 );
	assert.deepEqual( [ ...sent[0].payload ], [ 1, 2, 1, 7, 0, 0, 0 ] );
	assert.equal( game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 5 ), true );
	assert.equal( game.command( request, 6, item, local ), null );
	assert.throws( () => game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2 ) }, 7 ) );
	assert.equal( game.command( request, 8, item, local ), null, "malformed release cannot change intent" );
	assert.equal( game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 0 ) }, 9 ), true );
	assert.ok( game.command( request, 10, item, local ), "partial stack or refused pickup can retry" );
	assert.equal( sent.length, 2 );
	game.dispose();
});

test("cancel, despawn, replacement and scene lifecycle retire pickup intent", () => {
	for ( const reason of [ "cancel", "move", "despawn", "replacement", "travel", "bootstrap" ] ) {
		const { game, local, item, sent } = pickupFixture();
		const request = { kind: /** @type {const} */ ("pickup"), gid: item.gid };
		game.command( request, 0, item, local );
		if ( reason === "cancel" ) game.command( { kind: "cancel" }, 1, undefined, local );
		if ( reason === "move" ) {
			game.command( { kind: "move", destination: { ...local, x: 120, angle: 0 } }, 1, undefined, local );
		}
		if ( reason === "despawn" ) game.entityLifecycle( { kind: "despawn", gid: item.gid } );
		if ( reason === "replacement" ) game.command( { kind: "pickup", gid: 8 }, 1, { ...item, gid: 8 }, local );
		if ( reason === "travel" ) game.resetWorld();
		if ( reason === "bootstrap" ) game.bootstrap( {} );
		if ( reason === "travel" || reason === "bootstrap" ) game.seed( local );
		const count = sent.length;
		assert.ok( game.command( request, 2, item, local ), reason );
		assert.equal( sent.length, count + 1, reason );
		game.dispose();
	}
});
