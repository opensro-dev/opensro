/*
===========================================================================

beta-global-chat.test.mjs - public echo, replay and character-session history

Exercise native receipt ordering through the chat owner and scene reentry
through the gameplay owner. Neither entity visibility nor travel owns history.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { chatLineColor } = await import( "../../src/engine/foundation/ui/chat-presentation.ts" );

/*
================
globalFrame
================
*/
function globalFrame( name, text ) {
	const sender = Buffer.from( name, "utf8" );
	const message = Buffer.from( text, "utf16le" );
	const payload = Buffer.alloc( 5 + sender.length + message.length );
	payload[0] = 6;
	payload.writeUInt16LE( sender.length, 1 );
	sender.copy( payload, 3 );
	payload.writeUInt16LE( text.length, 3 + sender.length );
	message.copy( payload, 5 + sender.length );
	return { opcode: 0x3667, payload };
}

test("global chat renders the authoritative name without an entity lookup", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	assert.equal( chat.receive( globalFrame( "FarAway", "hello from Jangan" ), 1 ), true );
	assert.deepEqual( chat.state().lines, [ {
		channel: 6,
		name: "FarAway",
		gid: undefined,
		text: "hello from Jangan",
		outgoing: false,
		sequence: 1
	} ] );
	chat.request( 1, "hello back", "", 0 );
	chat.receive( globalFrame( "Local", "hello back" ), 1 );
	assert.equal( chat.state().pending, true, "public echo must not release the native receipt gate" );
	const ownLine = chat.state().lines.at( -1 );
	assert.ok( ownLine );
	assert.equal( ownLine.channel, 6 );
	assert.deepEqual( chatLineColor( ownLine.channel ), chatLineColor( 6 ) );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().pending, false );
	assert.equal( chat.state().lines.length, 2 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 2, "an own-name broadcast must not duplicate the receipt" );
	assert.throws(
		() => chat.request( 6, "forged global", "", 1 ),
		/Invalid chat message/,
		"global channel remains server-owned"
	);
});

test("public replay includes the user's earlier messages and retains repeated text", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	for ( let i = 0; i < 2; i++ ) chat.receive( globalFrame( "Local", "same words" ), 1 );
	assert.equal( chat.state().lines.length, 2 );
	assert.ok( chat.state().lines.every( line => line.channel === 6 && !line.outgoing ) );
});

test("world reentry preserves chat while character changes and session reset clear it", () => {
	const game = createGameplay( () => {} );
	const entry = { character: { name: "Local" } };
	game.bootstrap( entry );
	game.receive( globalFrame( "FarAway", "before teleport" ), 0 );
	const before = game.take()?.chat?.lines;
	assert.equal( before?.length, 1 );
	game.resetWorld();
	game.bootstrap( entry );
	assert.deepEqual( game.take()?.chat?.lines, before );
	game.bootstrap( { character: { name: "Other" } } );
	assert.equal( game.take()?.chat?.lines.length, 0 );
	game.receive( globalFrame( "FarAway", "other session" ), 0 );
	game.reset();
	game.bootstrap( { character: { name: "Other" } } );
	assert.equal( game.take()?.chat?.lines.length, 0 );
	game.dispose();
});

test("travel retains a pending receipt without resending or duplicating the message", () => {
	const sent = [];
	const chat = createChat( frame => sent.push( frame ) );
	const entry = { character: { name: "Local" } };
	chat.bootstrap( entry );
	chat.request( 1, "travel now", "", 0 );
	chat.receive( globalFrame( "Local", "travel now" ), 1 );
	chat.bootstrap( entry );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 1 );
	assert.equal( chat.state().pending, false );
	assert.equal( sent.length, 1 );
});
