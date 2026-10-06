/*
===========================================================================

chat-presentation.test.mjs - tests for chat-presentation.ts, text-lines.ts,
chat.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { composeChat, selectChatTab, chatLineColor, chatLineText, chatFeedbackText } = await import(
	"../../src/engine/foundation/ui/chat-presentation.ts"
);
const { textLines } = await import( "../../src/engine/foundation/ui/text-lines.ts" );
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const catalog = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) ).entries;
test("tabs seed markers without rewriting drafts; the actual prefix owns routing", () => {
	for ( const [tab, marker, channel] of [ [ 0, "", 1 ], [ 1, "#", 4 ], [ 2, "@", 5 ], [ 3, "%", 11 ] ] ) {
		assert.equal( selectChatTab( "", tab ), marker );
		assert.equal( selectChatTab( "$", tab ), marker );
		assert.equal( selectChatTab( "draft", tab ), "draft" );
		assert.equal( selectChatTab( "#draft", tab ), "#draft" );
		assert.deepEqual( composeChat( marker + "hello" ), { channel, target: "", text: "hello" } );
	}
	assert.equal( composeChat( "#" ), null );
	assert.equal( composeChat( "$Peer" ), null );
	assert.deepEqual( composeChat( "$Peer hello" ), { channel: 2, target: "Peer", text: "hello" } );
});
test("all native rejection arms drain their keyed pending request and publish localized feedback once", () => {
	const keys = {
		3: "UIIT_CHATERR_CANT_FIND_TARGET",
		6: "UIIT_CHATERR_YOU_ARE_SQUELCHED",
		8: "UIIT_CHATERR_INVALID_COMMAND",
		10: "UIIT_CHATERR_NOT_A_PARTY_MEMBER",
		11: "UIIT_CHATERR_ALLIANCE_PERMISSION_DENIED",
		12: "UIIT_CHATERR_ALLIANCE_PERMISSION_DENIED",
		13: "UIIT_STT_CANT_CHATTING",
		14: "UIIT_MSG_GUILD_UNION_CHAT_LIMIT"
	};
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Me" } } );
	for ( let code = 3; code <= 15; code++ ) {
		const before = chat.state().feedback.length;
		chat.request( 2, "message", "Peer", 0 );
		chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 2, code, 2, 1 ) }, 1 );
		assert.equal( chat.state().pending, true );
		const frame = { opcode: 0xb367, payload: Uint8Array.of( 2, code, 2, 255 ) };
		chat.receive( frame, 1 );
		chat.receive( frame, 1 );
		assert.equal( chat.state().pending, false );
		assert.equal( chat.state().lines.length, 0 );
		assert.equal( chat.state().feedback.length, before + (keys[code] ? 1 : 0) );
		if ( keys[code] ) {
			assert.deepEqual( { ...chat.state().feedback.at( -1 ), sequence: 0 }, {
				sequence: 0,
				key: keys[code],
				argument: "Peer"
			} );
		}
		if ( keys[code] ) {
			assert.doesNotMatch( chatFeedbackText( chat.state().feedback.at( -1 ), key => catalog[key] ), /%[sd]/ );
		}
	}
});
test("authored welcome indentation survives hard line breaks but soft wrapping creates no indentation", () => {
	const welcome = catalog.UIIT_STT_STARTING_MSG;
	assert.deepEqual( textLines( welcome, 1000, s => s.length, true ), welcome.split( "\n" ).map( s => s.trimEnd() ) );
	assert.deepEqual( textLines( "word next", 4, s => s.length, true ), [ "word", "next" ] );
	assert.deepEqual( textLines( "  long", 4, s => s.length, true ), [ "  lo", "ng" ] );
	assert.deepEqual( textLines( " first\n second", 100, s => s.length, true ), [ " first", " second" ] );
	assert.deepEqual( textLines( " first\n second", 100, s => s.length ), [ "first", "second" ] );
});
test("native channel captions and colors are shared for every rendered row", () => {
	const copy = k => catalog[k];
	assert.equal( chatLineText( { channel: 2, name: "Peer", text: "hello", outgoing: true }, copy ), "Peer(TO):hello" );
	assert.equal(
		chatLineText( { channel: 4, name: "Peer", text: "hello", outgoing: false }, copy ),
		"Peer(Party):hello"
	);
	assert.equal(
		chatLineText( { channel: 11, name: "Peer", text: "hello", outgoing: false }, copy ),
		"Peer(Union):hello"
	);
	for (
		const [channel, color] of [
			[ 1, 0xffffff ],
			[ 2, 0x9ffffe ],
			[ 3, 0xffaec3 ],
			[ 4, 0x9affd0 ],
			[ 5, 0xffb541 ],
			[ 11, 0xc2f573 ]
		]
	) {
		const rgb = chatLineColor( channel );
		assert.equal(
			(Math.round( rgb[0] * 255 ) << 16) | (Math.round( rgb[1] * 255 ) << 8) | Math.round( rgb[2] * 255 ),
			color
		);
	}
});
