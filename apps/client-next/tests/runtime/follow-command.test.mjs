/*
===========================================================================

follow-command.test.mjs - Trace admission through worker and quickslots

Exercise the shipped command dispatcher with real selection state. Trace
must keep its native family and reject stale or unsuitable entity targets.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { quickSlotCommand, TRACE_ACTION_ID } = await import( "../../src/engine/foundation/gameplay/quickslots.ts" );

/*
================
followFixture
================
*/
function followFixture() {
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
	const target = { ...local, gid: 7, name: "target", x: 400 };
	const entities = new Map( [ [ local.gid, local ], [ target.gid, target ] ] );
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ), undefined, undefined, gid => entities.get( gid ) );
	game.bootstrap( {} );
	game.seed( local );
	game.command( { kind: "select", gid: target.gid }, 0, target, local );
	sent.length = 0;
	return { game, sent, local, target, entities };
}

test("Trace uses the same native request through direct action and quickslot", () => {
	const { game, sent, local } = followFixture();
	const state = game.take();
	assert.ok( state );
	const command = quickSlotCommand( { slot: 1, kind: 0x4a, payload: TRACE_ACTION_ID }, state );
	assert.deepEqual( command, { kind: "action-command", id: TRACE_ACTION_ID } );
	assert.ok( command );
	for ( const request of [ command, command ] ) {
		const frame = game.command( request, 100, undefined, local );
		assert.ok( frame );
		assert.equal( frame.opcode, 0x72cd );
		assert.deepEqual( [ ...frame.payload ], [ 1, 3, 1, 7, 0, 0, 0 ] );
	}
	assert.equal( sent.length, 2 );
	assert.equal( quickSlotCommand( { slot: 1, kind: 0x4a, payload: TRACE_ACTION_ID }, state, 123 ), null );
	game.dispose();
});

test("Trace rejects missing, dead, non-player and self targets and unavailable actors", () => {
	for ( const reason of [ "missing", "dead target", "monster", "self", "dead actor", "mounted" ] ) {
		const { game, sent, local, target, entities } = followFixture();
		let actor = local;
		if ( reason === "missing" ) entities.delete( target.gid );
		if ( reason === "dead target" ) entities.set( target.gid, { ...target, appearanceState: [ 2 ] } );
		if ( reason === "monster" ) entities.set( target.gid, { ...target, kind: "monster" } );
		if ( reason === "self" ) entities.set( target.gid, local );
		if ( reason === "dead actor" ) actor = { ...local, appearanceState: [ 2 ] };
		if ( reason === "mounted" ) actor = { ...local, mountedOn: 123 };
		assert.equal(
			game.command( { kind: "action-command", id: TRACE_ACTION_ID }, 100, undefined, actor ),
			null,
			reason
		);
		assert.equal( sent.length, 0, reason );
		game.dispose();
	}
});
