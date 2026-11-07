/*
===========================================================================

chat-link-hud.test.mjs - the pending item links of the two chat editors

Port-only, not native (SRO_CHAT_ITEM_LINKS). A link goes in at the caret
as one marker, the limit replaces the oldest link, an edit that removes a
marker drops its link, a pasted marker is not a link, and the editors draw
the label where the marker sits.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createChatLinkHud, chatLinkFromActivate } = await import( "../../src/engine/runtime/ui/hud/chat-link-hud.ts" );
const { CHAT_LINK_MARKER } = await import( "../../src/engine/foundation/gameplay/chat-item-links.ts" );

const M = CHAT_LINK_MARKER;
const A = { ref: { source: 0, slot: 14 }, label: "<A>" };
const B = { ref: { source: 1, slot: 2 }, label: "<B>" };

test("a link goes in at the caret", () => {
	const hud = createChatLinkHud();
	assert.deepEqual( hud.insert( "chat-text", "ab", 1, A, 1 ), { text: "a" + M + "b", caret: 2 } );
	assert.deepEqual( hud.links( "chat-text" ), [ A.ref ] );
	assert.deepEqual( hud.links( "wholechat-text" ), [], "each editor keeps its own links" );
});

test("the second link replaces the first at a limit of one", () => {
	const hud = createChatLinkHud();
	const first = hud.insert( "chat-text", "ab", 1, A, 1 );
	const second = hud.insert( "chat-text", first.text, first.text.length, B, 1 );
	assert.deepEqual( second, { text: "ab" + M, caret: 3 } );
	assert.deepEqual( hud.links( "chat-text" ), [ B.ref ] );
	assert.deepEqual( hud.labels( "chat-text" ), [ "<B>" ] );
});

test("a backspace over the token drops the link", () => {
	const hud = createChatLinkHud();
	hud.insert( "chat-text", "ab", 1, A, 1 );
	assert.equal( hud.edited( "chat-text", "a" + M + "b", "ab" ), "ab" );
	assert.deepEqual( hud.links( "chat-text" ), [] );
});

test("a pasted marker is no link", () => {
	const hud = createChatLinkHud();
	assert.equal( hud.edited( "chat-text", "ab", "a" + M + "b" ), "ab" );
	assert.deepEqual( hud.links( "chat-text" ), [] );
});

test("a full line takes no link", () => {
	const hud = createChatLinkHud();
	const full = "x".repeat( 100 );
	assert.deepEqual( hud.insert( "chat-text", full, 100, A, 1 ), { text: full, caret: 100 } );
	assert.deepEqual( hud.links( "chat-text" ), [] );
});

test("the editor draws the label where the marker sits", () => {
	const hud = createChatLinkHud();
	hud.insert( "chat-text", "ab", 1, A, 1 );
	assert.equal( hud.display( "chat-text", "a" + M + "b" ), "a<A>b" );
	assert.equal( hud.display( "wholechat-text", "plain" ), "plain" );
	hud.clear( "chat-text" );
	assert.deepEqual( hud.links( "chat-text" ), [] );
});

test("slot ids resolve to the item and the link the server reads", () => {
	const blade = { slot: 14, name: "Blade", plus: 7, typeFlags: 0 };
	const cape = { slot: 1, name: "Cape", plus: 0, typeFlags: 2 << 11 };
	const sources = {
		inventory: [ blade ],
		avatarInventory: [ cape ],
		storage: [ { slot: 2, name: "S" } ],
		pet: [ { slot: 0, name: "P" } ]
	};
	assert.deepEqual( chatLinkFromActivate( "slot:14", sources ), {
		ref: { source: 0, slot: 14 },
		label: "<Blade +7>"
	} );
	assert.deepEqual( chatLinkFromActivate( "avatar:2", sources ), { ref: { source: 3, slot: 1 }, label: "<Cape>" } );
	assert.deepEqual( chatLinkFromActivate( "storage-slot:2", sources ).ref, { source: 1, slot: 2 } );
	assert.deepEqual( chatLinkFromActivate( "cos-slot:0", sources ).ref, { source: 2, slot: 0 } );
	assert.equal( chatLinkFromActivate( "slot:15", sources ), null );
	assert.equal( chatLinkFromActivate( "skill:3", sources ), null );
});
