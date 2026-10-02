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

/*
================
invitationFrame

The 0x3393 proposal of a shared invitation type; party types carry options.
================
*/
function invitationFrame( type, gid = 7 ) {
	return {
		opcode: 0x3393,
		payload: Uint8Array.from( [ type, gid, 0, 0, 0, ...([ 2, 3 ].includes( type ) ? [ 3 ] : []) ] )
	};
}

/*
================
resurrectionFrame

{u8 4, u32 casterGid}: the whole body the server writes.
================
*/
function resurrectionFrame( gid = 0x12a ) {
	return { opcode: 0x3393, payload: Uint8Array.of( 4, gid & 0xff, gid >> 8, 0, 0 ) };
}

/*
================
answerInvitation

Receive one invitation of the given type, with a resurrection proposal
arriving before it, after it or not at all, then answer the invitation.
Returns what the invitation path observed and the state left behind.
================
*/
function answerInvitation( type, accept, resurrection ) {
	const { game, frames } = fixture();
	if ( resurrection === "before" ) game.receive( resurrectionFrame(), 0 );
	game.receive( invitationFrame( type ), 1 );
	if ( resurrection === "after" ) game.receive( resurrectionFrame(), 2 );
	const pending = defined( defined( game.take() ).social );
	game.command( { kind: "social-consent", accept }, 3, undefined );
	game.command( { kind: "social-consent", accept }, 4, undefined );
	const answered = defined( defined( game.take() ).social );
	const observed = {
		invitation: pending.invitation,
		replies: frames.map( f => [ f.opcode, ...f.payload ] ),
		after: answered.invitation
	};
	return { game, frames, observed, pending, answered };
}

test("a resurrection proposal is held in its own slot until the player answers it", () => {
	for ( const [accept, answer] of [ [ true, [ 1, 1 ] ], [ false, [ 1, 2 ] ] ] ) {
		const { game, frames } = fixture();
		assert.equal( game.receive( resurrectionFrame(), 0 ), true );
		const social = defined( defined( game.take() ).social );
		assert.deepEqual( social.resurrection, { gid: 0x12a } );
		assert.equal( social.invitation, null, "the question is not an invitation" );
		assert.doesNotThrow( () => game.command( { kind: "social-consent", accept: true }, 1, undefined ) );
		assert.equal( frames.length, 0, "the invitation consent does not answer the question" );
		game.command( { kind: "resurrection-consent", accept: Boolean( accept ) }, 2, undefined );
		assert.equal( frames.length, 1 );
		assert.equal( frames[0].opcode, 0x3393 );
		assert.deepEqual( [ ...frames[0].payload ], answer );
		assert.equal( defined( defined( game.take() ).social ).resurrection, undefined );
		game.command( { kind: "resurrection-consent", accept: true }, 3, undefined );
		assert.equal( frames.length, 1, "the answered proposal accepts no second reply" );
		game.dispose();
	}
});

test("resurrection proposals reject a missing caster and a truncated or padded body", () => {
	for ( const payload of [ [ 4, 0, 0, 0, 0 ], [ 4, 7, 0, 0 ], [ 4, 7, 0, 0, 0, 0 ] ] ) {
		const { game, frames } = fixture();
		assert.throws( () => game.receive( { opcode: 0x3393, payload: Uint8Array.from( payload ) }, 0 ) );
		assert.equal( frames.length, 0 );
		game.dispose();
	}
});

// The consent each shared invitation type answers with, yes then no: the
// bytes the client sent before resurrection proposals were decoded.
const INVITATION_REPLIES = {
	1: [ [ 1, 1 ], [ 1, 2 ] ],
	2: [ [ 1, 1 ], [ 2, 0x0c ] ],
	3: [ [ 1, 1 ], [ 2, 0x17 ] ],
	5: [ [ 1, 1 ], [ 2, 0x16 ] ]
};

test("a resurrection proposal arriving over a pending invitation leaves it answerable unchanged", () => {
	for ( const type of [ 1, 2, 3, 5 ] ) {
		for ( const accept of [ true, false ] ) {
			const alone = answerInvitation( type, accept, "none" );
			const raced = answerInvitation( type, accept, "after" );
			assert.deepEqual( alone.observed.replies, [ [ 0x3393, ...INVITATION_REPLIES[type][accept ? 0 : 1] ] ] );
			assert.equal( alone.observed.after, null );
			assert.deepEqual( raced.observed, alone.observed, `type ${type} accept ${accept}` );
			assert.deepEqual( raced.pending.resurrection, { gid: 0x12a } );
			assert.deepEqual(
				raced.answered.resurrection,
				{ gid: 0x12a },
				"answering the invitation keeps the question"
			);
			raced.game.command( { kind: "resurrection-consent", accept: true }, 5, undefined );
			assert.deepEqual( [ ...defined( raced.frames.at( -1 ) ).payload ], [ 1, 1 ] );
			assert.equal( raced.frames.length, alone.frames.length + 1 );
			alone.game.dispose();
			raced.game.dispose();
		}
	}
});

test("an invitation arriving over an open resurrection question is answered as without it", () => {
	for ( const type of [ 1, 2, 3, 5 ] ) {
		for ( const accept of [ true, false ] ) {
			const alone = answerInvitation( type, accept, "none" );
			const raced = answerInvitation( type, accept, "before" );
			assert.deepEqual( raced.observed, alone.observed, `type ${type} accept ${accept}` );
			assert.deepEqual(
				raced.answered.resurrection,
				{ gid: 0x12a },
				"the invitation does not displace the question"
			);
			raced.game.command( { kind: "resurrection-consent", accept: false }, 5, undefined );
			assert.deepEqual( [ ...defined( raced.frames.at( -1 ) ).payload ], [ 1, 2 ] );
			assert.equal( defined( defined( raced.game.take() ).social ).resurrection, undefined );
			alone.game.dispose();
			raced.game.dispose();
		}
	}
});
