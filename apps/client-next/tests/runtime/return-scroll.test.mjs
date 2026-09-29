/*
===========================================================================

return-scroll.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load

Use the shipped module graph through the shared TypeScript source loader.
================
*/
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path + ".ts" ).href );
}
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay" );
const { returnScrollBar } = await load( "foundation/ui/return-scroll" );
const pose = { regionId: 0x6b4f, x: 60, y: 10, z: 100, angle: 0 };
/*
================
fixture

Admit a real scroll reference so its final-unit receipt preserves the label.
================
*/
function fixture( quantity = 1 ) {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		refItemSnapshot: [ {
			refObjId: 61,
			typeFlags: 0x9ec,
			name: "Return Scroll",
			nativeFields: { itemParam1_29c: 30000 }
		} ],
		equipItems: [ { refObjId: 61, slot: 13, body: [ 61, 0, 0, 0, quantity, 0 ] } ]
	} );
	game.seed( { ...pose, gid: 7, heading: 0 } );
	game.take();
	return { game, sent };
}
/*
================
status
================
*/
const status = ( gid, mode ) => ({ opcode: 0x3122, payload: Uint8Array.of( gid, 0, 0, 0, 11, mode ) });
/*
================
receipt
================
*/
const receipt = ( quantity ) => ({ opcode: 0xb5bd, payload: Uint8Array.of( 1, 13, quantity, 0, 0xec, 9 ) });
test("production scroll use retains reference before last unit disappears and waits for server cancellation", () => {
	const { game, sent } = fixture();
	game.command( { kind: "item-use", slot: 13 }, 100 );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x75bd, payload: Uint8Array.of( 13, 0xec, 9 ) } );
	game.receive( status( 7, 1 ), 100 );
	assert.equal( game.take().returnScroll, undefined );
	game.receive( receipt( 0 ), 101 );
	const cast = game.take().returnScroll;
	assert.deepEqual( cast, { refObjId: 61, name: "Return Scroll", startedAtMs: 101, durationMs: 30000 } );
	for (
		const command of [
			{ kind: "move", destination: pose },
			{ kind: "attack", gid: 8 },
			{ kind: "pickup", gid: 8 },
			{ kind: "skill", skillId: 1 }
		]
	) game.command( command, 102 );
	assert.equal( sent.length, 1 );
	game.command( { kind: "return-cancel" }, 102 );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x72dd, payload: new Uint8Array( 0 ) } );
	game.receive( status( 8, 0 ), 103 );
	game.receive( { opcode: 0xb2dd, payload: Uint8Array.of( 2, 6 ) }, 103 );
	game.receive( status( 7, 0 ), 104 );
	const after = game.take();
	assert.equal( after.returnScroll, undefined );
	assert.deepEqual( after.inventory, [] );
	assert.equal( after.notices.at( -1 ).key, "UIIT_MSG_TRANSITION_CANCEL_RESULT" );
	assert.equal( cast.name, "Return Scroll" );
	game.dispose();
});
test("replacement, dead actor, reset and malformed response transitions use the gameplay owner", () => {
	const { game } = fixture( 3 );
	for ( let n = 2; n >= 0; n-- ) {
		game.command( { kind: "item-use", slot: 13 }, (3 - n) * 100 );
		game.receive( status( 7, 1 ), (3 - n) * 100 );
		game.receive( receipt( n ), (3 - n) * 100 );
		assert.ok( game.take().returnScroll );
		game.die( 7, 1000 );
		assert.ok( game.take().returnScroll, "death does not invent a cast cancellation" );
		game.receive( status( 7, 0 ), 1001 );
		assert.equal( game.take().returnScroll, undefined );
	}
	for ( const payload of [ [], [ 1, 0 ], [ 2 ], [ 3 ] ] ) {
		assert.throws(
			() => game.receive( { opcode: 0xb2dd, payload: Uint8Array.from( payload ) }, 1 ),
			/cancellation response/
		);
	}
	game.resetWorld();
	assert.equal( game.take().returnScroll, undefined );
	game.dispose();
});
test("native return gauge remains visible at completion with authored geometry across viewport branches", () => {
	const cast = { refObjId: 61, name: "Return Scroll", startedAtMs: 0, durationMs: 30000 };
	assert.equal(
		returnScrollBar( { ...cast, durationMs: 0 }, 1024, 768, { now: 60000 } ).quads.some( q =>
			q.texture.endsWith( "gauge_return.png" )
		),
		false,
		"zero-duration initialization skips the native fraction update"
	);
	for ( const [width, height, x] of [ [ 800, 600, 405 ], [ 1024, 768, 416 ], [ 1200, 900, 504 ] ] ) {
		for ( const now of [ 0, 15000, 30000, 60000 ] ) {
			const bar = returnScrollBar( cast, width, height, { now } ),
				gauge = bar.quads.find( q => q.texture.endsWith( "gauge_return.png" ) );
			assert.deepEqual( bar.frame, [ x, height - 89, 192, 36 ] );
			assert.deepEqual( bar.cancel, [ x + 171, height - 85, 20, 20 ] );
			assert.equal( gauge?.rect[2] ?? 0, 184 * Math.min( 1, now / 30000 ) );
		}
	}
});
