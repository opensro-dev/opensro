/*
===========================================================================

chat-history.test.mjs - the chat log the browser keeps across a reload

Port-only, not native (default by owner decision 2026-11-26): the chat
window keeps the last 100 lines per character across a page reload. The
worker never touches storage: the page offers a character's kept lines
ahead of its entry (chatHistory), the chat owner spends them onto an
empty log at bootstrap, and the page writes the presented log down
(storedChatLines) into one per-scope book (chatHistorySavedBook).
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test, mock } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const {
	CHAT_HISTORY_KEPT,
	chatHistoryBook,
	chatHistorySavedBook,
	chatHistoryScope,
	storedChatLines
} = await import( "../../src/engine/foundation/gameplay/chat-history.ts" );
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
fixture
================
*/
function fixture() {
	const game = createGameplay( () => {} );
	const enter = name => game.bootstrap( { simulationProtocolVersion: 1, character: { name } } );
	const say = ( channel, name, text ) =>
		game.receive( { opcode: 0x3667, payload: lineBody( channel, name, text ) }, 1 );
	const lines = () => game.take()?.chat?.lines ?? [];
	const texts = () => lines().map( line => line.text );
	return { game, enter, say, lines, texts };
}

test("the 101st line drops the oldest", () => {
	mock.method( Date, "now", () => NOON_UTC );
	const f = fixture();
	try {
		f.enter( "Local" );
		for ( let i = 1; i <= CHAT_HISTORY_KEPT + 1; i++ ) f.say( GLOBAL, "Peer", "line " + i );
		const texts = f.texts();
		assert.equal( texts.length, CHAT_HISTORY_KEPT );
		assert.equal( texts[0], "line 2", "the first line said is the one dropped" );
		assert.equal( texts[CHAT_HISTORY_KEPT - 1], "line " + (CHAT_HISTORY_KEPT + 1) );
	} finally {
		f.game.dispose();
		mock.restoreAll();
	}
});

test("a reload restores the lines the browser kept", () => {
	mock.method( Date, "now", () => NOON_UTC );
	const before = fixture(), after = fixture();
	try {
		before.enter( "Local" );
		before.say( GLOBAL, "Peer", "selling blades" );
		before.say( WHISPER, "Friend", "are you there?" );
		// The page writes the presented log down and, after the reload,
		// offers it ahead of the same character's entry.
		const stored = storedChatLines( before.lines() );
		after.game.chatHistory( "Local", stored );
		after.enter( "Local" );
		const restored = after.lines();
		assert.deepEqual( restored.map( line => line.text ), [ "selling blades", "are you there?" ] );
		assert.ok(
			restored.every( line => line.history === true ),
			"restored lines are history: said before this session"
		);
	} finally {
		before.game.dispose();
		after.game.dispose();
		mock.restoreAll();
	}
});

test("another character's history stays apart", () => {
	mock.method( Date, "now", () => NOON_UTC );
	const f = fixture();
	try {
		f.game.chatHistory(
			"Local",
			storedChatLines( [
				{ channel: GLOBAL, name: "Peer", text: "selling blades", outgoing: false, sentAt: NOON_UTC }
			] )
		);
		f.enter( "Other" );
		assert.deepEqual( f.texts(), [], "the offer names Local; Other reads nothing" );
	} finally {
		f.game.dispose();
		mock.restoreAll();
	}

	const scopeA = chatHistoryScope( "shard-a", "Local" ), scopeB = chatHistoryScope( "shard-b", "Local" );
	const local = [ { channel: GLOBAL, name: "Peer", text: "hi", outgoing: false, sentAt: NOON_UTC } ];
	const book = chatHistorySavedBook( chatHistorySavedBook( {}, scopeA, local ), scopeB, [] );
	assert.deepEqual( Object.keys( book ).sort(), [ scopeA, scopeB ].sort(), "scopes keep their own records" );
	assert.equal( chatHistoryBook( book )[scopeA][0].text, "hi" );
	assert.deepEqual( chatHistoryBook( "{not a book" ), {}, "a corrupt record reads as no book" );
});

test("only plain line facts are written down, capped at the kept count", () => {
	const many = Array.from( { length: CHAT_HISTORY_KEPT + 5 }, ( _, i ) => ({
		channel: GLOBAL,
		name: "Peer",
		text: "line " + i,
		outgoing: false,
		sentAt: NOON_UTC + i,
		links: [ { refObjId: 1 } ],
		notice: { name: "Peer" }
	}) );
	const stored = storedChatLines( many );
	assert.equal( stored.length, CHAT_HISTORY_KEPT );
	assert.equal( stored[0].text, "line 5", "the oldest five are dropped" );
	assert.ok( stored.every( line => !("links" in line) && !("notice" in line) ), "no live decorations persist" );
	assert.equal( stored[0].sentAt, NOON_UTC + 5 );
});
