/*
===========================================================================

chat-item-links.test.mjs - the chat item-link codec and labels

Port-only, not native (SRO_CHAT_ITEM_LINKS). The request block and the
broadcast tail against the server's pinned vectors
(apps/server/internal/game/social/chat/itemlinks_test.go), the lenient
decode that never fails the session, and the labels a link shows.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const {
	CHAT_LINK_MARKER,
	encodeChatLinkRefs,
	decodeChatLinkTail,
	chatLinkLabel,
	chatLinkSpans,
	markerCount,
	stripChatMarkers,
	replaceChatMarkers
} = await import( "../../src/engine/foundation/gameplay/chat-item-links.ts" );

const bytes = hex => Uint8Array.from( hex.split( " " ).map( b => parseInt( b, 16 ) ) );

test("the link refs encode as the server decodes them", () => {
	assert.deepEqual( encodeChatLinkRefs( [ { source: 1, slot: 0x0102 } ] ), bytes( "01 01 02 01" ) );
	assert.equal( encodeChatLinkRefs( [] ).length, 0 );
});

test("a pet link names its pet after the slot", () => {
	// Pinned against the server's vector (chat/itemlinks_test.go): a pet
	// ref is [u8 source][u16 slot][u32 gid]; the others stay 3 bytes.
	assert.deepEqual(
		encodeChatLinkRefs( [ { source: 1, slot: 0x0102 }, { source: 2, slot: 0x0041, pet: 0x12345678 } ] ),
		bytes( "02 01 02 01 02 41 00 78 56 34 12" )
	);
	assert.deepEqual(
		encodeChatLinkRefs( [ { source: 2, slot: 0x0041, pet: 0x12345678 } ] ),
		bytes( "01 02 41 00 78 56 34 12" )
	);
});

test("with links off a received line draws unchanged", () => {
	// A pasted marker is plain text then: no label, no "<?>" (the owner
	// report behind #3022). Links on, the same line labels and spans.
	const text = "look " + CHAT_LINK_MARKER + " ok";
	assert.deepEqual( chatLinkSpans( text, undefined, false ), { text, spans: [] } );
	assert.deepEqual( chatLinkSpans( text, [], false ), { text, spans: [] } );
	const on = chatLinkSpans( text, [ { name: "Blade", plus: 7 } ], true );
	assert.equal( on.text, "look <Blade +7> ok" );
	assert.deepEqual( on.spans, [ { kind: "chat-item:0", start: 5, end: 15 } ] );
});

test("the broadcast tail decodes the server's vector", () => {
	const bodies = defined( decodeChatLinkTail( bytes( "AA 01 03 00 01 02 03" ), 1 ) );
	assert.equal( bodies.length, 1 );
	assert.deepEqual( bodies[0], bytes( "01 02 03" ) );
	assert.deepEqual( decodeChatLinkTail( bytes( "AA" ), 1 ), [] );
});

test("a malformed tail is no links and never throws", () => {
	const nine = [ "09", ...Array.from( { length: 9 }, () => "01 00 01" ) ].join( " " );
	for ( const hex of [ nine, "00", "01 03", "01 03 00 01 02", "01 01 00 01 FF", "01 00 00" ] ) {
		assert.equal( decodeChatLinkTail( bytes( hex ), 0 ), null, hex );
	}
});

test("labels name the item and its plus", () => {
	assert.equal( chatLinkLabel( { name: "Blade", plus: 7 } ), "<Blade +7>" );
	assert.equal( chatLinkLabel( { name: "Blade", plus: 0 } ), "<Blade>" );
	assert.equal( chatLinkLabel( undefined ), "<?>" );
});

test("markers are counted, stripped and replaced", () => {
	const text = "a" + CHAT_LINK_MARKER + "b" + CHAT_LINK_MARKER;
	assert.equal( markerCount( text ), 2 );
	assert.equal( stripChatMarkers( text ), "ab" );
	assert.equal( replaceChatMarkers( text, [ "<X>" ] ), "a<X>b<?>" );
});

test("an unreadable earlier link drops its own marker", async () => {
	const { resolveChatLinks } = await import( "../../src/engine/foundation/gameplay/chat-item-links.ts" );
	const M = CHAT_LINK_MARKER, a = { source: 2, slot: 4 }, b = { source: 0, slot: 13 };
	const out = resolveChatLinks( "x " + M + " and " + M, [ a, b ], 2, ref => ref === b ? "B" : undefined );
	assert.equal( out.text, "x  and " + M );
	assert.deepEqual( out.refs, [ b ] );
	assert.deepEqual( out.items, [ "B" ] );
});
