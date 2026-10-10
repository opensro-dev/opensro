/*
===========================================================================

chat-ack-timeout.test.mjs - an unanswered chat line never locks chat

A line the server never acknowledges used to hold the pending slot for the
whole session: every later Enter was ignored until a reload. The deadline
now releases the slot and tells the player the line may not have arrived.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createChat } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" ).href
);

/*
================
chatAck

The 0xB367 success acknowledgement for one requested channel.
================
*/
function chatAck( channel ) {
	return { opcode: 0xb367, payload: Uint8Array.of( 1, channel, 255 ) };
}

test("an unacknowledged line releases chat at its deadline", () => {
	const sent = [];
	const chat = createChat( frame => sent.push( frame ) );
	chat.bootstrap( { character: { name: "Me" } } );

	chat.request( 1, "first", "", 0 );
	assert.equal( chat.state().pending, true );
	assert.throws( () => chat.request( 1, "too soon", "", 5000 ), /pending/ );

	assert.equal( chat.step( 9999 ), false, "the line keeps its full wait" );
	assert.equal( chat.state().pending, true );

	assert.equal( chat.step( 10000 ), true );
	assert.equal( chat.state().pending, false, "the deadline releases the slot" );
	assert.match( chat.state().error, /not confirmed/ );

	chat.request( 1, "second", "", 10001 );
	assert.equal( sent.length, 2, "a later line still reaches the server" );
	assert.equal( chat.state().error, null, "a new line clears the old warning" );

	chat.receive( chatAck( 1 ), 10002 );
	assert.equal( chat.state().pending, false );
});

test("an acknowledged line never reports a timeout", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Me" } } );
	chat.request( 1, "hello", "", 0 );
	chat.receive( chatAck( 1 ), 50 );
	assert.equal( chat.step( 20000 ), false );
	assert.equal( chat.state().error, null );
});
