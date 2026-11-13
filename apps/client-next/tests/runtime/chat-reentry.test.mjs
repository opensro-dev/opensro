/*
===========================================================================

chat-reentry.test.mjs - a re-entry keeps the log the player was away from

An idle session that is lost and enters the world again as the same
character keeps its chat, whispers included (#3435). The server's public
replay adds only the lines the log does not hold. Another character, or a
reset that does not keep the chat, starts from nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test, mock } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { CHAT_HISTORY_CONTROL } = await import( "../../src/engine/foundation/gameplay/commerce-controls.ts" );
const NOON_UTC = Date.UTC( 2026, 0, 15, 12, 0, 0 );
const WHISPER = 2, GLOBAL = 6;

/*
================
lineBody

A named 0x3667 body: [u8 channel][u16 sender][sender][u16 chars][utf16 text].
================
*/
function lineBody( channel, name, text ) {
	const sender = Buffer.from( name, "utf8" ), message = Buffer.from( text, "utf16le" );
	const body = Buffer.alloc( 5 + sender.length + message.length );
	body[0] = channel;
	body.writeUInt16LE( sender.length, 1 );
	sender.copy( body, 3 );
	body.writeUInt16LE( text.length, 3 + sender.length );
	message.copy( body, 5 + sender.length );
	return body;
}

/*
================
historyFrame

The server's version 2 public replay: [u8 2][u8 count] then per line
[u64 sentAt][u16 size][0x3667 body].
================
*/
function historyFrame( rows ) {
	const parts = [ Buffer.from( [ 2, rows.length ] ) ];
	for ( const row of rows ) {
		const body = lineBody( row.channel, row.name, row.text ), head = Buffer.alloc( 10 );
		head.writeBigUInt64LE( BigInt( row.sentAt ), 0 );
		head.writeUInt16LE( body.length, 8 );
		parts.push( head, body );
	}
	return { opcode: CHAT_HISTORY_CONTROL, payload: Buffer.concat( parts ) };
}

/*
================
fixture
================
*/
function fixture() {
	const game = createGameplay( () => {} );
	const enter = name => game.bootstrap( { simulationProtocolVersion: 1, character: { name } } );
	const say = ( channel, name, text ) =>
		game.receive( { opcode: 0x3667, payload: lineBody( channel, name, text ) }, 1 );
	const texts = () => game.take()?.chat?.lines.map( line => line.text ) ?? [];
	return { game, enter, say, texts };
}

test("a re-entry of the same character keeps its whispers and public lines", () => {
	mock.method( Date, "now", () => NOON_UTC );
	const f = fixture();
	try {
		f.enter( "Local" );
		f.say( GLOBAL, "Peer", "selling blades" );
		f.say( WHISPER, "Friend", "are you there?" );
		f.game.reset( true );
		f.enter( "Local" );
		assert.deepEqual( f.texts(), [ "selling blades", "are you there?" ] );
	} finally {
		f.game.dispose();
		mock.restoreAll();
	}
});

test("another character, or a reset that does not keep the chat, starts from nothing", () => {
	mock.method( Date, "now", () => NOON_UTC );
	const f = fixture();
	try {
		f.enter( "Local" );
		f.say( WHISPER, "Friend", "are you there?" );
		f.game.reset( true );
		f.enter( "Other" );
		assert.deepEqual( f.texts(), [], "another character inherits nothing" );
		f.say( WHISPER, "Friend", "hello" );
		f.game.reset();
		f.enter( "Other" );
		assert.deepEqual( f.texts(), [], "a departure's reset clears the log" );
	} finally {
		f.game.dispose();
		mock.restoreAll();
	}
});

test("the public replay after a re-entry adds only the lines the log does not hold", () => {
	mock.method( Date, "now", () => NOON_UTC );
	const f = fixture();
	try {
		f.enter( "Local" );
		f.say( GLOBAL, "Peer", "selling blades" );
		f.game.reset( true );
		f.enter( "Local" );
		// The server's clock runs 20 s ahead of this client's.
		f.game.receive(
			historyFrame( [
				{ channel: GLOBAL, name: "Peer", text: "selling blades", sentAt: NOON_UTC + 20_000 },
				{ channel: GLOBAL, name: "Peer", text: "said while away", sentAt: NOON_UTC + 60_000 },
				{ channel: GLOBAL, name: "Peer", text: "said while away", sentAt: NOON_UTC + 70_000 }
			] ),
			2
		);
		assert.deepEqual(
			f.texts(),
			[ "selling blades", "said while away", "said while away" ],
			"the retained line is not repeated; the replay's own repeat stays"
		);
	} finally {
		f.game.dispose();
		mock.restoreAll();
	}
});
