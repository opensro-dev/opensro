/*
===========================================================================

beta-global-chat.test.mjs - global names do not depend on visible entities

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );

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
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().pending, false );
	assert.equal( chat.state().lines.length, 2 );
	chat.receive( globalFrame( "Local", "hello back" ), 1 );
	assert.equal( chat.state().lines.length, 2, "an own-name broadcast must not duplicate the receipt" );
	assert.throws(
		() => chat.request( 6, "forged global", "", 1 ),
		/Invalid chat message/,
		"global channel remains server-owned"
	);
});
