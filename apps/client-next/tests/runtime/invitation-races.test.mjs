/*
===========================================================================

invitation-races.test.mjs - deadline and queued-button invitation regressions.

Exercise production gameplay admission and packet decoding with explicit
clock values. Timer delivery and a UI reply may occur in either order.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
fixture
================
*/
function fixture() {
	const frames = [];
	const game = createGameplay( frame => frames.push( frame ) );
	game.bootstrap( { character: { name: "Owner" } } );
	game.seed( {
		refObjId: 1907,
		kind: "local-player",
		name: "Owner",
		gid: 1,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	} );
	return { game, frames };
}

/*
================
matchingRequest

A minimal masked member record follows the two correlation words.
================
*/
function matchingRequest( id = 123 ) {
	const payload = new Uint8Array( 26 );
	const view = new DataView( payload.buffer );
	view.setUint32( 0, id, true );
	view.setUint32( 4, 42, true );
	payload[21] = 16;
	view.setUint32( 22, 77, true );
	return { opcode: 0x75bf, payload };
}

test("matching answer and timer share the deadline at either dispatch order", () => {
	for ( const now of [ 10499, 10500, 60000 ] ) {
		for ( const timerFirst of [ false, true ] ) {
			for ( const answer of [ 0, 1 ] ) {
				const { game, frames } = fixture();
				game.receive( matchingRequest(), 500 );
				if ( timerFirst ) game.step( now );
				assert.doesNotThrow( () =>
					game.command(
						{ kind: "party-match-answer", a: 123, b: 42, answer: answer === 1 ? 1 : 0 },
						now,
						undefined
					)
				);
				game.step( now );
				assert.equal( frames.length, 1 );
				assert.equal( frames[0].opcode, 0x30fa );
				assert.equal( frames[0].payload[8], now >= 10500 ? 2 : answer );
				assert.equal( defined( defined( game.take() ).partyMatching ).request, null );
				game.dispose();
			}
		}
	}
});

test("a stale matching button cannot answer a replacement request", () => {
	const { game, frames } = fixture();
	game.receive( matchingRequest(), 0 );
	game.receive( matchingRequest( 124 ), 100 );
	assert.doesNotThrow( () =>
		game.command( { kind: "party-match-answer", a: 123, b: 42, answer: 1 }, 101, undefined )
	);
	assert.equal( frames.length, 0 );
	assert.equal( defined( defined( defined( game.take() ).partyMatching ).request ).a, 124 );
	game.command( { kind: "party-match-answer", a: 124, b: 42, answer: 1 }, 102, undefined );
	assert.equal( frames.length, 1 );
	game.dispose();
});

test("queued direct invitation replies after timeout are harmless", () => {
	for ( const type of [ 2, 3 ] ) {
		for ( const accept of [ false, true ] ) {
			const { game, frames } = fixture();
			game.receive( { opcode: 0x3393, payload: Uint8Array.of( type, 7, 0, 0, 0, 0 ) }, 0 );
			game.receive( { opcode: 0xb452, payload: Uint8Array.of( 2, 16 ) }, 31001 );
			assert.doesNotThrow( () => game.command( { kind: "social-consent", accept }, 31001, undefined ) );
			assert.equal( frames.length, 0 );
			assert.equal( defined( defined( game.take() ).social ).invitation, null );
			game.dispose();
		}
	}
});

test("all shared invitation types ignore duplicate clicks after answering", () => {
	for ( const type of [ 1, 2, 3, 5 ] ) {
		const { game, frames } = fixture();
		game.receive( {
			opcode: 0x3393,
			payload: Uint8Array.from( [ type, 7, 0, 0, 0, ...([ 2, 3 ].includes( type ) ? [ 0 ] : []) ] )
		}, 0 );
		game.command( { kind: "social-consent", accept: true }, 1, undefined );
		assert.doesNotThrow( () => game.command( { kind: "social-consent", accept: true }, 2, undefined ) );
		assert.equal( frames.length, 1 );
		assert.deepEqual( [ ...frames[0].payload ], [ 1, 1 ] );
		game.dispose();
	}
});

test("missing inviter and failed commit acknowledgements reach native player notices", () => {
	for ( const opcode of [ 0xb0d5, 0xb51a, 0xb452 ] ) {
		for (
			const [code, key] of [ [ 14, "UIIT_MSG_PARTYERR_CANT_FIND_CREATER" ], [
				2,
				"UIIT_MSG_PARTYERR_UNKNOWN_ERROR"
			] ]
		) {
			const { game } = fixture();
			game.receive( { opcode, payload: Uint8Array.of( 2, Number( code ) ) }, 1 );
			assert.equal( defined( defined( defined( game.take() ).notices ).at( -1 ) ).key, key );
			game.dispose();
		}
	}
});
