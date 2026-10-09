/*
===========================================================================

global-chat.test.mjs - the Global Chatting item's line on the item-use wire

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { isGlobalChatItem, globalChatTail } = await import( "../../src/engine/foundation/gameplay/global-chat.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

// ITEM_MALL_GLOBAL_CHATTING: an etc item (3/3) of type 3, type 4 = 5.
const GLOBAL_CHATTING = 3 << 2 | 3 << 5 | 3 << 7 | 5 << 11;

test("only the 3/3/3/5 item is the Global Chatting item", () => {
	assert.equal( isGlobalChatItem( GLOBAL_CHATTING ), true );
	// The return scroll (type 4 = 1) and the other 3/3/3 scrolls are not.
	assert.equal( isGlobalChatItem( 3 << 2 | 3 << 5 | 3 << 7 | 1 << 11 ), false );
	assert.equal( isGlobalChatItem( GLOBAL_CHATTING | 2 ), false );
});

test("the line rides the item's use as a sized UTF-16 string (693C90)", () => {
	assert.deepEqual( globalChatTail( "Hi" ), Uint8Array.of( 2, 0, 0x48, 0, 0x69, 0 ) );
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( {
		refItemSnapshot: [ { refObjId: 3851, typeFlags: GLOBAL_CHATTING } ],
		equipItems: [ { refObjId: 3851, slot: 21, body: [ 0x0b, 0x0f, 0, 0, 3, 0 ] } ]
	} );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Author"
	} );
	gameplay.command( { kind: "item-use", slot: 21, message: "Hi" }, 1, undefined );
	assert.deepEqual( sent, [ {
		opcode: 0x75bd,
		payload: Uint8Array.of( 21, GLOBAL_CHATTING & 255, GLOBAL_CHATTING >>> 8, 2, 0, 0x48, 0, 0x69, 0 )
	} ] );
});
