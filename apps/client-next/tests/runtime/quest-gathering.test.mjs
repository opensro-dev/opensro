/*
===========================================================================

quest-gathering.test.mjs - native short collection gauge and cancellation

Drive shipped gameplay with v1.150 packets. The delay must preserve receipt
time, isolate identities, reject malformed packets and reset with the world.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { returnScrollBar } = await import( "../../src/engine/foundation/ui/return-scroll.ts" );

test("gathering uses native seconds and waits for matching cancellation", () => {
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( {
		gid: 7,
		refObjId: 14875,
		kind: "player",
		name: "GatherProbe",
		regionId: 0x655e,
		x: 253,
		y: 85,
		z: 449,
		heading: 0
	} );
	game.receive( { opcode: 0x36bd, payload: Uint8Array.of( 207, 0, 0, 0, 10 ) }, 500 );
	const expected = { refId: 207, startedAtMs: 500, durationMs: 10000 };
	const started = game.take();
	assert.ok( started );
	assert.deepEqual( started.questGathering, expected );
	game.command( { kind: "gathering-cancel" }, 501, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x775d, payload: Uint8Array.of( 207, 0, 0, 0 ) } );
	game.receive( { opcode: 0xb75d, payload: Uint8Array.of( 1, 208, 0, 0, 0 ) }, 502 );
	const unrelated = game.take();
	assert.ok( unrelated );
	assert.deepEqual( unrelated.questGathering, expected );
	assert.equal( unrelated.notices?.some( row => row.key === "UIIT_MSG_QUEST_GET_ITEM_FAILURE" ), false );
	game.receive( { opcode: 0xb75d, payload: Uint8Array.of( 1, 207, 0, 0, 0 ) }, 503 );
	const canceled = game.take();
	assert.ok( canceled );
	assert.equal( canceled.questGathering, undefined );
	assert.equal( canceled.notices?.at( -1 )?.key, "UIIT_MSG_QUEST_GET_ITEM_FAILURE" );
	game.receive( { opcode: 0xb75d, payload: Uint8Array.of( 2, 0 ) }, 503 );
	assert.equal( game.take()?.notices?.at( -1 )?.key, "UIIT_STT_ERR_COMMON_NOT_ACCEPT" );
	for ( const payload of [ [], [ 207, 0, 0, 0 ], [ 0, 0, 0, 0, 10 ], [ 207, 0, 0, 0, 10, 0 ] ] ) {
		assert.throws(
			() => game.receive( { opcode: 0x36bd, payload: Uint8Array.from( payload ) }, 504 ),
			/gathering/
		);
	}
	game.receive( { opcode: 0x36bd, payload: Uint8Array.of( 207, 0, 0, 0, 10 ) }, 600 );
	game.resetWorld();
	const reset = game.take();
	assert.ok( reset );
	assert.equal( reset.questGathering, undefined );
	game.dispose();
});

test("collection and return rows share native geometry without overlapping", () => {
	const clock = { startedAtMs: 500, durationMs: 10000 };
	const bar = returnScrollBar( clock, 1600, 900, { now: 5500, collection: true, row: 1 } );
	assert.deepEqual( bar.frame, [ 704, 773, 192, 36 ] );
	const gauge = bar.quads.find( quad => quad.texture.endsWith( "com_casting_gauge_collection.png" ) );
	assert.deepEqual( gauge?.rect, [ 710, 800, 92, 8 ] );
	assert.deepEqual( gauge?.uv, [ 0, 0, 0.5, 1 ] );
	assert.equal( bar.quads.some( quad => quad.texture.endsWith( "com_casting_gauge_return.png" ) ), false );
});
