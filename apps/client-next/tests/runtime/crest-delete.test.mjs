/*
===========================================================================

crest-delete.test.mjs - the crest delete items' client behaviour

#2282 (delete half): using ITEM_ETC_GUILD_CREST_DELETE or
ITEM_ETC_UNION_CREST_DELETE sends the ordinary item use; its 0xB5BD
success answer raises the native completion box (755E40's codename
chain: a confirm-variant box titled UIIT_STT_CONFIRM_BOX with the
DELETE_COMPLETE body). The register items' answers are the register
flow's business (crest-register.test.mjs).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
const { crestDeleteKind, crestDeleteKeys } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/crest-delete.ts" ).href
);

// TID 3/3/12/2 packed as the client's type word (see cos-status-lines).
const CREST_FLAGS = 3 << 2 | 3 << 5 | 12 << 7 | 2 << 11;
const u16 = n => [ n & 255, n >>> 8 & 255 ];
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];

/*
================
world

A local player (gid 1) whose bag holds the given rows at slot 20+.
================
*/
function world( bagItems ) {
	const sent = [],
		game = createGameplay( f => sent.push( f ), () => {}, () => {}, undefined );
	game.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [],
		refItemSnapshot: bagItems.map( item => ({
			refObjId: item.refObjId,
			typeFlags: CREST_FLAGS,
			nativeFields: { maxStack: 50 }
		}) ),
		equipItems: bagItems.map( ( item, index ) => ({
			slot: 20 + index,
			refObjId: item.refObjId,
			body: [ ...u32( item.refObjId ), ...u16( 5 ) ]
		}) )
	} );
	game.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	return { game, sent };
}

test("the delete items are known by their shipped reference ids", () => {
	assert.equal( crestDeleteKind( 9269 ), "guild" );
	assert.equal( crestDeleteKind( 9271 ), "union" );
	// The register half (#2272) is not the delete flow's business.
	assert.equal( crestDeleteKind( 9268 ), undefined );
	assert.equal( crestDeleteKind( 9270 ), undefined );
	assert.equal( crestDeleteKind( 3630 ), undefined );
	assert.deepEqual( crestDeleteKeys( "guild" ), {
		confirm: "UIIT_MSG_GUILD_CREST_DELETE",
		refused: "UIIT_MSG_GUILD_DELETE_ERROR",
		complete: "UIIT_MSG_GUILD_CREST_DELETE_COMPLETE"
	} );
	assert.deepEqual( crestDeleteKeys( "union" ), {
		confirm: "UIIT_MSG_UNION_CREST_DELETE",
		refused: "UIIT_MSG_UNION_DELETE_ERROR",
		complete: "UIIT_MSG_UNION_CREST_DELETE_COMPLETE"
	} );
});

test("a guild crest delete's success answer raises the completion box", () => {
	const { game, sent } = world( [ { refObjId: 9269 } ] );
	const frame = game.command( { kind: "item-use", slot: 20, crestDelete: true }, 1 );
	assert.equal( frame.opcode, 0x75bd );
	assert.deepEqual( [ ...frame.payload ], [ 20, ...u16( CREST_FLAGS ) ] );
	assert.equal( sent.length, 1 );
	game.receive( { opcode: 0xb5bd, payload: Uint8Array.from( [ 1, 20, ...u16( 4 ), ...u16( CREST_FLAGS ) ] ) }, 2 );
	const dialogs = (game.take()?.notices ?? []).filter( notice => notice.dialog );
	assert.equal( dialogs.length, 1 );
	assert.deepEqual( dialogs[0].dialog, {
		title: "UIIT_STT_CONFIRM_BOX",
		lines: [ "UIIT_MSG_GUILD_CREST_DELETE_COMPLETE" ]
	} );
});

test("a union crest delete's success answer names the union line", () => {
	const { game } = world( [ { refObjId: 9271 } ] );
	game.command( { kind: "item-use", slot: 20, crestDelete: true }, 1 );
	game.receive( { opcode: 0xb5bd, payload: Uint8Array.from( [ 1, 20, ...u16( 4 ), ...u16( CREST_FLAGS ) ] ) }, 2 );
	const dialogs = (game.take()?.notices ?? []).filter( notice => notice.dialog );
	assert.equal( dialogs.length, 1 );
	assert.deepEqual( dialogs[0].dialog, {
		title: "UIIT_STT_CONFIRM_BOX",
		lines: [ "UIIT_MSG_UNION_CREST_DELETE_COMPLETE" ]
	} );
});

test("a refused delete answer raises no box", () => {
	const refused = world( [ { refObjId: 9269 } ] );
	refused.game.command( { kind: "item-use", slot: 20, crestDelete: true }, 1 );
	refused.game.receive( { opcode: 0xb5bd, payload: Uint8Array.from( [ 2, 0 ] ) }, 2 );
	assert.deepEqual( (refused.game.take()?.notices ?? []).filter( notice => notice.dialog ), [] );
});
