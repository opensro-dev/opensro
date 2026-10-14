/*
===========================================================================

friends.test.mjs - tests for friends.ts (wire) and the gameplay friend lane

The friend lane reads the native v1.150 packets as the original does
(foundation/gameplay/friends.ts) and the worker owner keeps the roster,
the prompt and the request in flight (gameplay/friends/friends.ts). These
tests drive both through createGameplay with hand-built frames.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const friends = await import( "../../src/engine/foundation/gameplay/friends.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { constantNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );

/*
================
bytes

A little-endian frame body from u8 / u32 / sized-name parts.
================
*/
function bytes( ...parts ) {
	const out = [];
	for ( const part of parts ) {
		if ( typeof part === "string" ) {
			const name = new TextEncoder().encode( part );
			out.push( name.length & 255, name.length >> 8, ...name );
		} else if ( part.u32 !== undefined ) {
			out.push( part.u32 & 255, (part.u32 >> 8) & 255, (part.u32 >> 16) & 255, (part.u32 >>> 24) & 255 );
		} else out.push( part );
	}
	return Uint8Array.from( out );
}
const u32 = value => ({ u32: value });

/*
================
lane

A gameplay owner with its sends captured, entered as "Alfa".
================
*/
function lane() {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( { character: { name: "Alfa" } } );
	g.seed( /** @type {any} */ ({ gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 }) );
	g.take();
	return { sent, g };
}

/*
================
friendsOf

The friend lane of a published state.
================
*/
function friendsOf( state ) {
	return defined( defined( state ).friends );
}

/*
================
lastNotice
================
*/
function lastNotice( state ) {
	const notice = defined( defined( state ).notices ).at( -1 );
	return { ...notice, sequence: undefined };
}

test("the wire decoders read the native layouts and refuse malformed frames", () => {
	const roster = bytes( 2, u32( 7 ), "Berk", u32( 1907 ), 0, u32( 9 ), "Cale", u32( 1908 ), 1 );
	assert.deepEqual( friends.decodeFriendRoster( roster ), [
		{ jid: 7, name: "Berk", modelRefId: 1907, online: true },
		{ jid: 9, name: "Cale", modelRefId: 1908, online: false }
	] );
	assert.throws( () => friends.decodeFriendRoster( roster.slice( 0, -1 ) ), /Truncated/ );
	assert.throws( () => friends.decodeFriendRoster( Uint8Array.from( [ ...roster, 0 ] ) ), /Trailing/ );
	assert.throws( () => friends.decodeFriendRoster( bytes( 21 ) ), /list limit/ );
	assert.throws(
		() => friends.decodeFriendPrompt( Uint8Array.from( [ 1, 0, 0, 0, 3, 0, 0, 0, 1, 0, 0xff ] ) ),
		"a name that is not UTF-8 is refused"
	);
	assert.deepEqual( friends.decodeFriendEvent( bytes( 2, u32( 7 ), "Berk", u32( 1907 ) ) ), {
		kind: "added",
		row: { jid: 7, name: "Berk", modelRefId: 1907, online: true }
	} );
	assert.deepEqual( friends.decodeFriendEvent( bytes( 3, u32( 7 ) ) ), { kind: "deleted", jid: 7 } );
	assert.deepEqual( friends.decodeFriendEvent( bytes( 4, u32( 7 ), 1 ) ), { kind: "state", jid: 7, online: false } );
	assert.equal( friends.decodeFriendEvent( bytes( 8, 0 ) ), null, "letters are another lane's" );
	assert.deepEqual( friends.decodeFriendPrompt( bytes( u32( 1 ), u32( 3 ), "Alfa" ) ), {
		jid: 1,
		serial: 3,
		name: "Alfa"
	} );
	assert.deepEqual( friends.decodeFriendRequestResult( bytes( 1 ) ), { kind: "ok" } );
	assert.deepEqual( friends.decodeFriendRequestResult( bytes( 2, 8 ) ), { kind: "code", code: 8 } );
	assert.deepEqual( friends.decodeFriendRequestResult( bytes( 2, 0x0b, 0, "Berk" ) ), {
		kind: "refused",
		name: "Berk"
	} );
	assert.deepEqual( friends.decodeFriendRequestResult( bytes( 2, 0x0b, 2, "Berk" ) ), {
		kind: "cancelled",
		name: "Berk"
	} );
	assert.throws( () => friends.decodeFriendRequestResult( bytes( 2, 0x0b, 1, "Berk" ) ) );
	assert.throws( () => friends.decodeFriendRequestResult( bytes( 3 ) ) );
	assert.deepEqual( friends.decodeFriendDeleteResult( bytes( 1, u32( 7 ) ) ), { kind: "ok", jid: 7 } );
	assert.deepEqual( friends.decodeFriendDeleteResult( bytes( 2, 3 ) ), { kind: "code", code: 3 } );
});

test("the requests carry what the original sends: a bounded name, a 9-byte consent, a jid", () => {
	assert.deepEqual( friends.friendRequest( " Berk " ), { opcode: 0x7164, payload: bytes( "Berk" ) } );
	assert.equal( friends.friendRequest( "" ), null );
	assert.equal( friends.friendRequest( "x".repeat( 33 ) ), null );
	assert.ok( friends.friendRequest( "x".repeat( 32 ) ) );
	const consent = friends.friendConsent( { jid: 1, serial: 3, name: "Alfa" }, true );
	assert.equal( consent.opcode, 0x3012 );
	assert.deepEqual( consent.payload, bytes( u32( 1 ), u32( 3 ), 1 ) );
	assert.equal( consent.payload.length, 9, "the port's game-ready 0x3012 is empty; a consent is never" );
	assert.deepEqual( friends.friendConsent( { jid: 1, serial: 3, name: "Alfa" }, false ).payload[8], 0 );
	assert.deepEqual( friends.friendDelete( 7 ), { opcode: 0x75db, payload: bytes( u32( 7 ) ) } );
});

test("the roster seeds at world entry and follows add, delete and presence events", () => {
	const { g } = lane();
	assert.equal( g.receive( { opcode: 0x3769, payload: bytes( 1, u32( 7 ), "Berk", u32( 1907 ), 1 ) }, 0 ), true );
	assert.deepEqual( friendsOf( g.take() ).rows, [ { jid: 7, name: "Berk", modelRefId: 1907, online: false } ] );
	g.receive( { opcode: 0x3f9a, payload: bytes( 4, u32( 7 ), 0 ) }, 0 );
	let state = g.take();
	assert.equal( friendsOf( state ).rows[0].online, true );
	assert.deepEqual( lastNotice( state ), {
		key: "UIIT_MSG_FRIEND_CONNECT",
		value: 0,
		arguments: [ "Berk" ],
		formatKinds: [ "s" ],
		nativeType: 0,
		sequence: undefined
	} );
	g.receive( { opcode: 0x3f9a, payload: bytes( 4, u32( 7 ), 1 ) }, 0 );
	assert.equal( lastNotice( g.take() ).key, "UIIT_MSG_FRIEND_DISCONNECT" );
	g.receive( { opcode: 0x3f9a, payload: bytes( 2, u32( 9 ), "Cale", u32( 1908 ) ) }, 0 );
	assert.deepEqual( friendsOf( g.take() ).rows.map( row => row.name ), [ "Berk", "Cale" ] );
	g.receive( { opcode: 0x3f9a, payload: bytes( 3, u32( 7 ) ) }, 0 );
	assert.deepEqual( friendsOf( g.take() ).rows.map( row => row.name ), [ "Cale" ] );
	assert.equal(
		g.receive( { opcode: 0x3f9a, payload: bytes( 8, "Dora", u32( 1909 ), u32( 0 ) ) }, 0 ),
		true,
		"a letter event goes on to the letter lane"
	);
	assert.deepEqual( friendsOf( g.take() ).rows.map( row => row.name ), [ "Cale" ], "and leaves the roster alone" );
});

test("a prompt waits for this player's answer, which is sent as the 9-byte 0x3012", () => {
	const { sent, g } = lane();
	g.receive( { opcode: 0x7164, payload: bytes( u32( 2 ), u32( 5 ), "Berk" ) }, 0 );
	assert.deepEqual( friendsOf( g.take() ).prompt, { jid: 2, serial: 5, name: "Berk" } );
	g.command( { kind: "friend-answer", accept: true }, 0, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x3012, payload: bytes( u32( 2 ), u32( 5 ), 1 ) } );
	assert.equal( friendsOf( g.take() ).prompt, null );
	const count = sent.length;
	g.command( { kind: "friend-answer", accept: false }, 0, undefined );
	assert.equal( sent.length, count, "no prompt, nothing to answer" );
	// A cancel from the server closes an open prompt without a notice,
	// whatever name it carries (760AF0 reads the panel state).
	g.receive( { opcode: 0x7164, payload: bytes( u32( 2 ), u32( 6 ), "Berk" ) }, 0 );
	g.take();
	g.receive( { opcode: 0xb164, payload: bytes( 2, 0x0b, 2, "Somebody" ) }, 0 );
	const state = defined( g.take() );
	assert.equal( friendsOf( state ).prompt, null );
	assert.equal( (state.notices ?? []).some( notice => notice.key === "UIIT_MSG_FRIEND_SYSCANCEL" ), false );
});

test("a request is sent whenever asked, except for this player's own name, and shows the original's notices", () => {
	const { sent, g } = lane();
	g.command( { kind: "friend-request", name: "Berk" }, 0, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x7164, payload: bytes( "Berk" ) } );
	assert.equal( friendsOf( g.take() ).pending, true );
	g.command( { kind: "friend-request", name: "Alfa" }, 0, undefined );
	assert.equal( sent.length, 1, "602180 drops the player's own name (56E110)" );
	g.command( { kind: "friend-request", name: "Cale" }, 0, undefined );
	assert.equal( sent.length, 2, "nothing gates a second request; the server answers 0x0E" );
	g.receive( { opcode: 0xb164, payload: bytes( 2, 0x0b, 0, "Berk" ) }, 0 );
	let state = g.take();
	assert.equal( friendsOf( state ).pending, false );
	assert.deepEqual( lastNotice( state ), {
		key: "UIIT_MSG_FRIENDERR_REFUSAL",
		value: 0,
		notificationBanner: true,
		bannerOnly: true,
		sequence: undefined
	} );
	g.command( { kind: "friend-request", name: "Berk" }, 0, undefined );
	g.receive( { opcode: 0xb164, payload: bytes( 2, 0x0b, 2, "Berk" ) }, 0 );
	assert.equal( lastNotice( g.take() ).key, "UIIT_MSG_FRIEND_SYSCANCEL" );
	/** @type {[number, string][]} */
	const codes = [
		[ 8, "UIIT_MSG_FRIENDERR_FULLLIST" ],
		[ 9, "UIIT_MSG_FRIENDERR_ALREADYFRIEND" ],
		[ 0x0d, "UIIT_MSG_FRIENDERR_UNKNOWNUSER" ],
		[ 0x0e, "UIIT_MSG_FRIEND_SYSPROGRESS" ],
		[ 0x0f, "UIIT_MSG_FRIENDERR_TARGETLISTFULL" ]
	];
	for ( const [code, key] of codes ) {
		g.command( { kind: "friend-request", name: "Berk" }, 0, undefined );
		g.receive( { opcode: 0xb164, payload: bytes( 2, code ) }, 0 );
		state = g.take();
		assert.equal( lastNotice( state ).key, key, `code ${code}` );
		assert.deepEqual( lastNotice( state ), { ...constantNativeNotice( 0x13, code ), sequence: undefined } );
	}
	const before = defined( g.take() ?? state ).notices?.length ?? 0;
	g.command( { kind: "friend-request", name: "Alfa" }, 0, undefined );
	g.receive( { opcode: 0xb164, payload: bytes( 2, 3 ) }, 0 );
	assert.equal( defined( g.take() ).notices?.length ?? 0, before, "the original shows nothing for code 3" );
	g.command( { kind: "friend-request", name: "Berk" }, 0, undefined );
	g.receive( { opcode: 0xb164, payload: bytes( 1 ) }, 0 );
	assert.equal( friendsOf( g.take() ).pending, false );
});

test("a delete is sent only for a listed friend", () => {
	const { sent, g } = lane();
	g.receive( { opcode: 0x3769, payload: bytes( 1, u32( 7 ), "Berk", u32( 1907 ), 0 ) }, 0 );
	g.command( { kind: "friend-delete", jid: 9 }, 0, undefined );
	assert.equal( sent.length, 0 );
	g.command( { kind: "friend-delete", jid: 7 }, 0, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x75db, payload: bytes( u32( 7 ) ) } );
	assert.equal( g.receive( { opcode: 0xb5db, payload: bytes( 1, u32( 7 ) ) }, 0 ), true );
});
