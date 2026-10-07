/*
===========================================================================

overhead-chat.test.mjs - tests for speech.ts, chat.ts, text-lines.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createSpeech } = await import( "../../src/engine/runtime/ui/hud/speech.ts" );
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
test("accepted self chat owns one ten-second board; retained frames and duplicate acknowledgements cannot rearm it", () => {
	const chat = createChat( () => {} ), speech = createSpeech(), players = [ { gid: 1, name: "Me" } ];
	chat.bootstrap( { character: { name: "Me" } } );
	chat.request( 1, "hello", "", 0 );
	assert.equal( speech.step( chat.state().lines, players, 0 ).size, 0 );
	const ack = { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) };
	chat.receive( ack, 1 );
	chat.receive( ack, 1 );
	assert.equal( chat.state().lines.length, 1 );
	assert.equal( defined( speech.step( chat.state().lines, players, 100 ).get( 1 ) ).text, "hello" );
	assert.equal( speech.deadline(), 10100 );
	assert.equal( speech.step( chat.state().lines, players, 10099 ).size, 1 );
	assert.equal( speech.step( chat.state().lines, players, 10100 ).size, 0 );
	chat.request( 1, "hello", "", 10101 );
	chat.receive( ack, 1 );
	assert.equal( speech.step( chat.state().lines, players, 10102 ).size, 1 );
	assert.equal( speech.step( chat.state().lines, [], 10103 ).size, 0 );
	assert.equal( speech.step( chat.state().lines, players, 10104 ).size, 0 );
	speech.reset();
	assert.equal( speech.deadline(), Infinity );
});
test("private/group channels never become overhead speech; a new public message replaces only its speaker", () => {
	const owner = createSpeech(), players = [ { gid: 1, name: "Me" }, { gid: 2, name: "Peer" } ];
	const line = ( sequence, channel, gid, text = "hello" ) => ({
		sequence,
		channel,
		gid,
		text,
		name: "Peer",
		outgoing: false
	});
	assert.equal( owner.step( [ line( 1, 1, 1 ), line( 2, 3, 2 ) ], players, 0 ).size, 2 );
	owner.step( [ line( 3, 2, 1 ), line( 4, 4, 1 ), line( 5, 5, 1 ), line( 6, 11, 1 ) ], players, 1000 );
	assert.equal( owner.deadline(), 10000 );
	assert.equal( defined( owner.step( [ line( 7, 6, undefined, "new" ) ], players, 2000 ).get( 2 ) ).text, "new" );
	const remaining = owner.step( [], players, 10000 );
	assert.equal( remaining.has( 1 ), false );
	assert.equal( remaining.has( 2 ), true );
});

const { textBoardLines } = await import( "../../src/engine/foundation/ui/text-lines.ts" );
test("native boards wrap glyphs and preserve boundary spaces, newlines and long tokens", () => {
	const measure = s => s.length;
	assert.deepEqual( textBoardLines( "one two", 5, measure ), [ "one t", "wo" ] );
	assert.deepEqual( textBoardLines( "  a  b ", 4, measure ), [ "  a ", " b " ] );
	assert.deepEqual( textBoardLines( "a\n\nb\n", 5, measure ), [ "a", "", "b", "" ] );
	assert.deepEqual( textBoardLines( "abcdefgh", 3, measure ), [ "abc", "def", "gh" ] );
});

const { decodeChatHistory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts"
);
/*
================
historyFrame

The server's OpChatHistory payload for global (6) lines {sender, text, sentAt}.
================
*/
const HISTORY_SENT_AT = 1_790_000_000_000;
function historyFrame( lines ) {
	const parts = lines.map( ( [sender, text, sentAt = HISTORY_SENT_AT] ) => {
		const name = new TextEncoder().encode( sender ),
			line = new Uint8Array( 1 + 2 + name.length + 2 + text.length * 2 );
		const v = new DataView( line.buffer );
		line[0] = 6;
		v.setUint16( 1, name.length, true );
		line.set( name, 3 );
		v.setUint16( 3 + name.length, text.length, true );
		for ( let i = 0; i < text.length; i++ ) v.setUint16( 5 + name.length + i * 2, text.charCodeAt( i ), true );
		return { line, sentAt };
	} );
	const out = new Uint8Array( 2 + parts.reduce( ( n, p ) => n + 10 + p.line.length, 0 ) ),
		v = new DataView( out.buffer );
	out[0] = 2;
	out[1] = parts.length;
	let o = 2;
	for ( const p of parts ) {
		v.setBigUint64( o, BigInt( p.sentAt ), true );
		v.setUint16( o + 8, p.line.length, true );
		out.set( p.line, o + 10 );
		o += 10 + p.line.length;
	}
	return out;
}
test("the replayed public transcript is history, never speech over a head on entry", () => {
	const chat = createChat( () => {} ),
		speech = createSpeech(),
		players = [ { gid: 1, name: "Me" }, { gid: 2, name: "Peer" } ];
	chat.bootstrap( { character: { name: "Me" } } );
	assert.equal( speech.step( chat.state().lines, players, 0 ).size, 0 );
	assert.equal(
		chat.receive( {
			opcode: 16,
			payload: historyFrame( [ [ "Me", "my old line" ], [ "Peer", "older", HISTORY_SENT_AT + 60_000 ] ] )
		}, 1 ),
		true
	);
	const lines = chat.state().lines;
	assert.deepEqual( lines.map( l => [ l.name, l.text, l.history ] ), [ [ "Me", "my old line", true ], [
		"Peer",
		"older",
		true
	] ] );
	// BUG-067: replayed lines keep the server's send time, not this login's.
	assert.deepEqual( lines.map( l => l.sentAt ), [ HISTORY_SENT_AT, HISTORY_SENT_AT + 60_000 ] );
	assert.equal( speech.step( lines, players, 100 ).size, 0 );
	assert.throws( () => decodeChatHistory( Uint8Array.of( 1, 0 ) ), /Invalid chat history/ );
	assert.throws(
		() => decodeChatHistory( Uint8Array.of( 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 9 ) ),
		/Truncated chat history/
	);
	assert.throws(
		() => decodeChatHistory( Uint8Array.of( 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 9, 0 ) ),
		/Truncated chat history/
	);
	assert.throws( () => decodeChatHistory( Uint8Array.of( ...historyFrame( [ [ "Me", "x" ] ] ), 0 ) ), /trailing/ );
});
