/*
===========================================================================

crest-register.test.mjs - the crest register items' client behaviour

#2282 (register half): confirming ITEM_ETC_GUILD_CREST_USER or
ITEM_ETC_UNION_CREST_USER sends the picked image on the crest upload
lane (control 19); the lane's refusal prints its native line (the
gate's notice byte, else the reason's), and the use's 0xB5BD success
answer raises the native completion box 0x1A/0x1B (755E40's codename
chain: the REGISTER_COMPLETE body and the four information lines).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
const {
	crestRegisterKind,
	crestRegisterKeys,
	crestRegisterResult,
	crestRegisterUploadPayload,
	CREST_IMAGE_MAX_BYTES
} = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/crest-register.ts" ).href
);
const { CREST_UPLOAD_CONTROL } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/commerce-controls.ts" ).href
);

// TID 3/3/12/2 packed as the client's type word (see cos-status-lines).
const CREST_FLAGS = 3 << 2 | 3 << 5 | 12 << 7 | 2 << 11;
const u16 = n => [ n & 255, n >>> 8 & 255 ];
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const json = value => new TextEncoder().encode( JSON.stringify( value ) );
const PNG_HEAD = Uint8Array.from( [ 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4 ] );

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

test("the register items are known by their shipped reference ids", () => {
	assert.equal( crestRegisterKind( 9268 ), "guild" );
	assert.equal( crestRegisterKind( 9270 ), "union" );
	// The delete half is not the register flow's business.
	assert.equal( crestRegisterKind( 9269 ), undefined );
	assert.equal( crestRegisterKind( 9271 ), undefined );
	assert.equal( crestRegisterKind( 3630 ), undefined );
	assert.deepEqual( crestRegisterKeys( "guild" ), {
		title: "UIIT_STT_CONFIRM_BOX",
		lines: [
			"UIIT_MSG_GUILD_USER_CREST",
			"UIIT_MSG_GUILD_USER_CREST_FILE",
			"UIIT_MSG_GUILD_USER_CREST_FOLDER",
			"UIIT_MSG_GUILD_CREST_REGISTER"
		],
		refused: "UIIT_MSG_CREST_REGISTER_OVERLAP",
		fail: "UIIT_MSG_CREST_REGISTER_FAIL",
		error: "UIIT_MSG_CREST_REGISTER_ERROR",
		complete: "UIIT_MSG_GUILD_CREST_REGISTER_COMPLETE",
		information: [
			"UIIT_MSG_CREST_REGISTER_INFORMATION_01",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_02",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_03",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_04"
		]
	} );
	assert.deepEqual( crestRegisterKeys( "union" ), {
		title: "UIIT_STT_CONFIRM_BOX",
		lines: [
			"UIIT_MSG_UNION_USER_CREST",
			"UIIT_MSG_UNION_USER_CREST_FOLDER",
			"UIIT_CTL_UNION_CREST_REGISTER",
			"UIIT_MSG_UNION_CREST_REGISTER"
		],
		refused: "UIIT_MSG_CREST_REGISTER_OVERLAP",
		fail: "UIIT_MSG_CREST_REGISTER_FAIL",
		error: "UIIT_MSG_CREST_REGISTER_ERROR",
		complete: "UIIT_MSG_UNION_CREST_REGISTER_COMPLETE",
		information: [
			"UIIT_MSG_CREST_REGISTER_INFORMATION_01",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_02",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_03",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_04"
		]
	} );
	assert.equal( CREST_IMAGE_MAX_BYTES, 256 << 10 );
	assert.equal( CREST_UPLOAD_CONTROL, 19 );
});

test("an upload payload carries the kind, the slot and the image bytes", () => {
	const payload = crestRegisterUploadPayload( "union", 23, PNG_HEAD );
	const request = JSON.parse( new TextDecoder().decode( payload ) );
	assert.equal( request.kind, "union" );
	assert.equal( request.slot, 23 );
	assert.deepEqual( Buffer.from( request.image, "base64" ), Buffer.from( PNG_HEAD ) );
});

test("the upload answer decodes strictly", () => {
	assert.deepEqual( crestRegisterResult( json( { ok: true, crest: 7 } ) ), { ok: true, crest: 7 } );
	assert.deepEqual(
		crestRegisterResult( json( { ok: false, reason: "overlap" } ) ),
		{ ok: false, reason: "overlap" }
	);
	assert.deepEqual(
		crestRegisterResult( json( { ok: false, reason: "refused", code: 188 } ) ),
		{ ok: false, reason: "refused", code: 188 }
	);
	assert.throws( () => crestRegisterResult( json( { ok: true, crest: 7, extra: 1 } ) ) );
	assert.throws( () => crestRegisterResult( json( { ok: true } ) ) );
	assert.throws( () => crestRegisterResult( json( { ok: false } ) ) );
	assert.throws( () => crestRegisterResult( json( { ok: false, reason: "refused", code: 999 } ) ) );
	assert.throws( () => crestRegisterResult( json( { version: 1, ok: true, crest: 7 } ) ) );
	assert.throws( () => crestRegisterResult( new TextEncoder().encode( "not json" ) ) );
});

test("a confirmed register sends one upload and holds a second", () => {
	const { game, sent } = world( [ { refObjId: 9268 } ] );
	const frame = game.command(
		{ kind: "crest-register", slot: 20, crestKind: "guild", image: PNG_HEAD },
		1
	);
	assert.equal( frame.opcode, CREST_UPLOAD_CONTROL );
	const request = JSON.parse( new TextDecoder().decode( frame.payload ) );
	assert.equal( request.kind, "guild" );
	assert.equal( request.slot, 20 );
	assert.deepEqual( Buffer.from( request.image, "base64" ), Buffer.from( PNG_HEAD ) );
	assert.equal( sent.length, 1 );
	// One upload outstanding at a time: the second confirmation waits.
	assert.equal(
		game.command( { kind: "crest-register", slot: 20, crestKind: "guild", image: PNG_HEAD }, 2 ),
		null
	);
	assert.equal( sent.length, 1 );
});

test("a register command names the row it stands on", () => {
	const { game, sent } = world( [ { refObjId: 9269 } ] );
	// The slot holds the delete twin: no upload goes out.
	assert.equal(
		game.command( { kind: "crest-register", slot: 20, crestKind: "guild", image: PNG_HEAD }, 1 ),
		null
	);
	assert.equal( sent.length, 0 );
});

test("the lane's refusals print their native lines and free the flight", () => {
	const { game, sent } = world( [ { refObjId: 9268 } ] );
	const upload = () => game.command( { kind: "crest-register", slot: 20, crestKind: "guild", image: PNG_HEAD }, 1 );

	// take() snapshots the standing notices; each round names the newest.
	upload();
	game.receive( { opcode: CREST_UPLOAD_CONTROL, payload: json( { ok: false, reason: "overlap" } ) }, 2 );
	let notices = game.take()?.notices ?? [];
	assert.equal( notices.at( -1 )?.key, "UIIT_MSG_CREST_REGISTER_OVERLAP" );
	assert.deepEqual( notices.filter( notice => notice.dialog ), [] );

	upload();
	game.receive( { opcode: CREST_UPLOAD_CONTROL, payload: json( { ok: false, reason: "invalid" } ) }, 3 );
	notices = game.take()?.notices ?? [];
	assert.equal( notices.at( -1 )?.key, "UIIT_MSG_CREST_REGISTER_ERROR" );

	upload();
	game.receive( { opcode: CREST_UPLOAD_CONTROL, payload: json( { ok: false, reason: "refused" } ) }, 4 );
	notices = game.take()?.notices ?? [];
	assert.equal( notices.at( -1 )?.key, "UIIT_MSG_CREST_REGISTER_FAIL" );

	// The gate's notice byte names the refusal more precisely.
	upload();
	game.receive(
		{ opcode: CREST_UPLOAD_CONTROL, payload: json( { ok: false, reason: "refused", code: 188 } ) },
		5
	);
	notices = game.take()?.notices ?? [];
	assert.equal( notices.at( -1 )?.key, "UIIT_MSG_GUILD_ITEM_RIGHT_LIMIT" );
	assert.equal( sent.length, 4 );
});

test("a guild register's success answer raises the completion box", () => {
	const { game } = world( [ { refObjId: 9268 } ] );
	game.command( { kind: "crest-register", slot: 20, crestKind: "guild", image: PNG_HEAD }, 1 );
	game.receive( { opcode: CREST_UPLOAD_CONTROL, payload: json( { ok: true, crest: 1 } ) }, 2 );
	// The lane's ok alone raises nothing; the 0xB5BD behind it opens the box.
	assert.deepEqual( (game.take()?.notices ?? []).filter( notice => notice.dialog ), [] );
	game.receive( { opcode: 0xb5bd, payload: Uint8Array.from( [ 1, 20, ...u16( 4 ), ...u16( CREST_FLAGS ) ] ) }, 3 );
	const dialogs = (game.take()?.notices ?? []).filter( notice => notice.dialog );
	assert.equal( dialogs.length, 1 );
	assert.deepEqual( dialogs[0].dialog, {
		title: "UIIT_STT_CONFIRM_BOX",
		lines: [
			"UIIT_MSG_GUILD_CREST_REGISTER_COMPLETE",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_01",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_02",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_03",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_04"
		]
	} );
});

test("a union register's success answer names the union line", () => {
	const { game } = world( [ { refObjId: 9270 } ] );
	game.command( { kind: "crest-register", slot: 20, crestKind: "union", image: PNG_HEAD }, 1 );
	game.receive( { opcode: CREST_UPLOAD_CONTROL, payload: json( { ok: true, crest: 1 } ) }, 2 );
	game.receive( { opcode: 0xb5bd, payload: Uint8Array.from( [ 1, 20, ...u16( 4 ), ...u16( CREST_FLAGS ) ] ) }, 3 );
	const dialogs = (game.take()?.notices ?? []).filter( notice => notice.dialog );
	assert.equal( dialogs.length, 1 );
	assert.deepEqual( dialogs[0].dialog, {
		title: "UIIT_STT_CONFIRM_BOX",
		lines: [
			"UIIT_MSG_UNION_CREST_REGISTER_COMPLETE",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_01",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_02",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_03",
			"UIIT_MSG_CREST_REGISTER_INFORMATION_04"
		]
	} );
});
