/*
===========================================================================

letters.test.mjs - tests for letters.ts (wire) and the gameplay letter lane

The letter lane reads the native v1.150 packets as the original does
(foundation/gameplay/letters.ts) and the worker owner keeps the mailbox,
each fetched body and the open read window (gameplay/letters/letters.ts).
These tests drive both through createGameplay with hand-built frames.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const letters = await import( "../../src/engine/foundation/gameplay/letters.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { constantNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );

/*
================
bytes

A little-endian frame body from u8 / u32 / sized-text parts.
================
*/
function bytes( ...parts ) {
	const out = [];
	for ( const part of parts ) {
		if ( typeof part === "string" ) {
			const text = new TextEncoder().encode( part );
			out.push( text.length & 255, text.length >> 8, ...text );
		} else if ( part.u32 !== undefined ) {
			out.push( part.u32 & 255, (part.u32 >> 8) & 255, (part.u32 >> 16) & 255, (part.u32 >>> 24) & 255 );
		} else out.push( part );
	}
	return Uint8Array.from( out );
}
const u32 = value => ({ u32: value });

/*
================
packed

A receive time packed as 60C9B0 reads it.
================
*/
function packed( year, month, day, hour, minute ) {
	return year | month << 6 | day << 10 | hour << 15 | minute << 20;
}

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
lettersOf
================
*/
function lettersOf( state ) {
	return defined( defined( state ).letters );
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

const MAILBOX = bytes(
	1,
	2,
	"Berk",
	u32( 1907 ),
	u32( packed( 26, 10, 15, 14, 5 ) ),
	1,
	"Cale",
	u32( 1908 ),
	u32( packed( 26, 10, 14, 9, 30 ) ),
	0
);

test("the wire decoders read the native layouts and refuse malformed frames", () => {
	assert.deepEqual( letters.decodeLetterList( MAILBOX ), [
		{ sender: "Berk", modelRefId: 1907, received: packed( 26, 10, 15, 14, 5 ), read: true, body: null },
		{ sender: "Cale", modelRefId: 1908, received: packed( 26, 10, 14, 9, 30 ), read: false, body: null }
	] );
	assert.deepEqual( letters.decodeLetterList( bytes( 2, 7 ) ), { code: 7 } );
	assert.throws( () => letters.decodeLetterList( MAILBOX.slice( 0, -1 ) ), /Truncated/ );
	assert.throws( () => letters.decodeLetterList( Uint8Array.from( [ ...MAILBOX, 0 ] ) ), /Trailing/ );
	assert.throws( () => letters.decodeLetterList( bytes( 1, 21 ) ), /mailbox limit/ );
	assert.throws( () => letters.decodeLetterList( bytes( 1, 1, "Berk", u32( 1 ), u32( 0 ), 2 ) ), /read flag/ );
	assert.throws( () => letters.decodeLetterList( bytes( 3 ) ) );
	// Player text is decoded leniently: a stored bad byte reads as U+FFFD.
	const bad = Uint8Array.from( [ 1, 0, 3, 0, 0x48, 0xff, 0x69 ] );
	assert.deepEqual( letters.decodeLetterReadResult( bad ), { kind: "ok", index: 0, body: "H�i" } );
	assert.throws( () => letters.decodeLetterReadResult( bad.slice( 0, -1 ) ), /Truncated/ );
	assert.deepEqual( letters.decodeLetterEvent( bytes( 8, "Dora", u32( 1909 ), u32( 5 ) ) ), {
		kind: "arrived",
		row: { sender: "Dora", modelRefId: 1909, received: 5, read: false, body: null }
	} );
	assert.deepEqual( letters.decodeLetterEvent( bytes( 9, 0 ) ), { kind: "unknown" } );
	assert.equal( letters.decodeLetterEvent( bytes( 3, u32( 7 ) ) ), null, "friend events are another lane's" );
	assert.deepEqual( letters.decodeLetterSendResult( bytes( 1 ) ), { kind: "ok", index: -1 } );
	assert.deepEqual( letters.decodeLetterSendResult( bytes( 2, 4 ) ), { kind: "code", code: 4 } );
	assert.deepEqual( letters.decodeLetterReadResult( bytes( 1, 1, "Hello" ) ), {
		kind: "ok",
		index: 1,
		body: "Hello"
	} );
	assert.deepEqual( letters.decodeLetterReadResult( bytes( 2, 5 ) ), { kind: "code", code: 5 } );
	assert.equal( letters.decodeLetterDeleteResult( bytes( 1, 1 ) ), 1 );
	assert.equal( letters.decodeLetterDeleteResult( bytes( 2 ) ), null, "75CFD0 reads nothing past another result" );
});

test("the requests carry what the original sends and the time reads as 60C9B0 prints it", () => {
	assert.deepEqual( letters.letterSend( " Berk ", "Hi\nthere" ), {
		opcode: 0x7261,
		payload: bytes( "Berk", "Hi\nthere" )
	} );
	assert.equal( letters.letterSend( "", "Hi" ), null );
	assert.equal( letters.letterSend( "Berk", "" ), null );
	assert.equal( letters.letterSend( "x".repeat( 13 ), "Hi" ), null );
	assert.equal( letters.letterSend( "Berk", "x".repeat( 257 ) ), null );
	assert.ok( letters.letterSend( "x".repeat( 12 ), "x".repeat( 256 ) ) );
	assert.deepEqual( letters.letterRead( 3 ), { opcode: 0x73f2, payload: Uint8Array.of( 3 ) } );
	assert.deepEqual( letters.letterDelete( 3 ), { opcode: 0x70cc, payload: Uint8Array.of( 3 ) } );
	assert.deepEqual( letters.letterTime( packed( 26, 10, 15, 14, 5 ) ), {
		year: 26,
		month: 10,
		day: 15,
		meridiem: "UIIT_STT_LETTER_AFTEMOON",
		hour: 2,
		minute: 5
	} );
	assert.equal(
		letters.letterTime( packed( 26, 10, 15, 12, 0 ) ).meridiem,
		"UIIT_STT_LETTER_FORENOON",
		"noon is forenoon"
	);
	assert.ok(
		letters.letterTimeKey( packed( 26, 10, 15, 9, 30 ) ) > letters.letterTimeKey( packed( 26, 10, 14, 23, 59 ) ),
		"a later day sorts later whatever the hour"
	);
	assert.ok(
		letters.letterTimeKey( packed( 27, 1, 1, 0, 0 ) ) > letters.letterTimeKey( packed( 26, 12, 31, 23, 59 ) )
	);
});

test("the mailbox seeds at world entry, takes arrivals and erases what the server deletes", () => {
	const { g } = lane();
	assert.equal( g.receive( { opcode: 0xb3cd, payload: MAILBOX }, 0 ), true );
	assert.deepEqual( lettersOf( g.take() ).rows.map( row => row.sender ), [ "Berk", "Cale" ] );
	assert.equal( g.receive( { opcode: 0x3f9a, payload: bytes( 8, "Dora", u32( 1909 ), u32( 5 ) ) }, 0 ), true );
	let state = lettersOf( g.take() );
	assert.deepEqual( state.rows.map( row => row.sender ), [ "Berk", "Cale", "Dora" ] );
	assert.equal( state.rows[2].read, false );
	g.receive( { opcode: 0xb0cc, payload: bytes( 1, 0 ) }, 0 );
	assert.deepEqual( lettersOf( g.take() ).rows.map( row => row.sender ), [ "Cale", "Dora" ] );
	g.receive( { opcode: 0xb0cc, payload: bytes( 1, 9 ) }, 0 );
	assert.deepEqual(
		lettersOf( g.take() ).rows.map( row => row.sender ),
		[ "Cale", "Dora" ],
		"an index past the mailbox changes nothing"
	);
	g.receive( { opcode: 0x3f9a, payload: bytes( 9, 0 ) }, 0 );
	assert.equal( lastNotice( g.take() ).key, "UIIT_MSG_LETTERERR_UNKNOWNLETTER" );
	// 0x14/0x0D is UIIT_MSG_LETTERERR_UNKNOWNUSER (689420).
	g.receive( { opcode: 0xb3cd, payload: bytes( 2, 0x0d ) }, 0 );
	state = defined( g.take() );
	assert.equal( lastNotice( state ).key, "UIIT_MSG_LETTERERR_UNKNOWNUSER" );
	assert.deepEqual( lastNotice( state ), { ...constantNativeNotice( 0x14, 0x0d ), sequence: undefined } );
	assert.equal( lettersOf( state ).rows.length, 2, "an error answer keeps the mailbox" );
});

test("a read fetches a body once, opens the read window, and marks the letter read", () => {
	const { sent, g } = lane();
	g.receive( { opcode: 0xb3cd, payload: MAILBOX }, 0 );
	g.take();
	g.command( { kind: "letter-read", index: 1 }, 0, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x73f2, payload: Uint8Array.of( 1 ) } );
	assert.equal( lettersOf( g.take() ).reading, -1, "the window opens on the answer" );
	g.receive( { opcode: 0xb3f2, payload: bytes( 1, 1, "Meet at Jangan" ) }, 0 );
	let state = lettersOf( g.take() );
	assert.equal( state.reading, 1 );
	assert.deepEqual( state.rows[1], { ...state.rows[1], read: true, body: "Meet at Jangan" } );
	g.command( { kind: "letter-close" }, 0, undefined );
	assert.equal( lettersOf( g.take() ).reading, -1 );
	const count = sent.length;
	g.command( { kind: "letter-read", index: 1 }, 0, undefined );
	assert.equal( sent.length, count, "609B50 opens a fetched letter without asking again" );
	assert.equal( lettersOf( g.take() ).reading, 1 );
	g.receive( { opcode: 0xb0cc, payload: bytes( 1, 0 ) }, 0 );
	state = lettersOf( g.take() );
	assert.equal( state.reading, 0, "the open letter moves up with the list" );
	g.receive( { opcode: 0xb0cc, payload: bytes( 1, 0 ) }, 0 );
	assert.equal( lettersOf( g.take() ).reading, -1, "deleting the open letter closes the window" );
	g.command( { kind: "letter-read", index: 5 }, 0, undefined );
	assert.equal( sent.length, count, "no letter, nothing to read" );
});

test("a send refuses empty fields with the write window's notices and confirms with SENDOK", () => {
	const { sent, g } = lane();
	g.command( { kind: "letter-send", receiver: " ", body: "Hi" }, 0, undefined );
	assert.equal( lastNotice( g.take() ).key, "UIIT_MSG_LETTERERR_IDWRITE" );
	g.command( { kind: "letter-send", receiver: "Berk", body: "" }, 0, undefined );
	assert.equal( lastNotice( g.take() ).key, "UIIT_MSG_LETTERERR_MESSAGEWRITE" );
	assert.equal( sent.length, 0 );
	g.command( { kind: "letter-send", receiver: "Berk", body: "Hi" }, 0, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x7261, payload: bytes( "Berk", "Hi" ) } );
	g.receive( { opcode: 0xb261, payload: bytes( 1 ) }, 0 );
	assert.deepEqual( lastNotice( g.take() ), {
		key: "UIIT_MSG_LETTER_SENDOK",
		value: 0,
		notificationBanner: true,
		bannerOnly: true,
		sequence: undefined
	} );
	g.receive( { opcode: 0xb261, payload: bytes( 2, 0x14 ) }, 0 );
	assert.equal( lastNotice( g.take() ).key, "UIIT_MSG_LETTERERR_FULLLETTERBOX" );
});

test("a delete is sent only for a letter in the mailbox", () => {
	const { sent, g } = lane();
	g.receive( { opcode: 0xb3cd, payload: MAILBOX }, 0 );
	g.command( { kind: "letter-delete", index: 2 }, 0, undefined );
	assert.equal( sent.length, 0 );
	g.command( { kind: "letter-delete", index: 1 }, 0, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x70cc, payload: Uint8Array.of( 1 ) } );
});

test("a mailbox seed with invalid UTF-8 in a sender or body is read with replacement characters", () => {
	const { g } = lane();
	// {1, 1 letter: sender "B\xffk", model, time, unread}: a stored bad byte
	// must never fail the session at world entry.
	const seed = Uint8Array.from( [ 1, 1, 3, 0, 0x42, 0xff, 0x6b, 1, 0, 0, 0, 0, 0, 0, 0, 0 ] );
	assert.equal( g.receive( { opcode: 0xb3cd, payload: seed }, 0 ), true );
	assert.equal( lettersOf( g.take() ).rows[0].sender, "B�k" );
	g.receive( { opcode: 0xb3f2, payload: Uint8Array.from( [ 1, 0, 2, 0, 0xc3, 0x28 ] ) }, 0 );
	assert.equal( lettersOf( g.take() ).rows[0].body, "�(" );
});

test("the write window's caps count UTF-8 bytes and never split a character", () => {
	assert.equal( letters.clampLetterText( "abc", 2 ), "ab" );
	// "é" is two bytes: 200 of them fit 128 in 256 bytes, and the result sends.
	const body = letters.clampLetterText( "é".repeat( 200 ), letters.LETTER_BODY_LIMIT );
	assert.equal( body.length, 128 );
	assert.ok( letters.letterSend( "Berk", body ), "a clamped body is always sendable" );
	assert.equal( letters.letterSend( "Berk", "é".repeat( 200 ) ), null, "an unclamped one is not" );
	// A three-byte character that would cross the cap is left out whole.
	assert.equal( letters.clampLetterText( "ab한", 4 ), "ab" );
	assert.equal( new TextEncoder().encode( letters.clampLetterText( "한".repeat( 10 ), 12 ) ).length, 12 );
});
