/*
===========================================================================

name-change.test.mjs - the name-change scroll's client half (#1689)

Port-only: the scroll is the shipped ref 3789 alone (its type word is
shared with the +2 scroll), the name tail is raw ASCII, the server's
0x90..0x98 refusal bytes read as their own lines, the 0x83 notice shows
as a server notification, and a roster row may carry renameCancelled
from its closed reason set only.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const {
	isNameChangeScrollItem,
	isValidRenameName,
	nameChangeTail,
	nameChangeFailureText,
	renameCancelledText
} = await import( "../../src/engine/foundation/gameplay/name-change.ts" );
const { inventoryNotice } = await import( "../../src/engine/foundation/gameplay/inventory-notices.ts" );
const { isServerNoticeChannel, serverNotification } = await import(
	"../../src/engine/foundation/gameplay/system-notices.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { defined } = await import( "../helpers/defined.mjs" );
const { createSessionDecoder } = await import(
	"../../src/engine/runtime/simulation/worker/session/decode/decode.ts"
);

test("the scroll is ref 3789 alone, never its shared type word", () => {
	assert.equal( isNameChangeScrollItem( 3789 ), true );
	assert.equal( isNameChangeScrollItem( 3790 ), false );
	assert.equal( isNameChangeScrollItem( 9259 ), false );
});

test("the name shape mirrors the native 2..12 ASCII rule", () => {
	for ( const name of [ "Ab", "NewName", "x_9", "ABCDEFGHIJKL" ] ) {
		assert.equal( isValidRenameName( name ), true, name );
	}
	for ( const name of [ "", "A", "ABCDEFGHIJKLM", "has space", "dash-name", "café", "[GM]Root" ] ) {
		assert.equal( isValidRenameName( name ), false, name );
	}
	assert.deepEqual( [ ...nameChangeTail( "New_Name9" ) ], [ ...new TextEncoder().encode( "New_Name9" ) ] );
	assert.throws( () => nameChangeTail( "bad name" ) );
});

test("the scroll's refusal bytes read as their own lines", () => {
	for ( const code of [ 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD ] ) {
		assert.equal( typeof nameChangeFailureText( code ), "string", code.toString( 16 ) );
	}
	assert.equal( nameChangeFailureText( 0xF4 ), undefined );
	// The native mapper stays exact: at these codes it has no text, on
	// either acknowledgement, and a native byte routes untouched.
	assert.equal( inventoryNotice( 0xb5bd, Uint8Array.of( 2, 0xF9 ) ), null );
	assert.equal( inventoryNotice( 0xb06d, Uint8Array.of( 2, 0xF9 ) ), null );
	assert.equal( inventoryNotice( 0xb5bd, Uint8Array.of( 2, 3 ) )?.key, "UIIT_MSG_STRGERR_INVALID_TARGET" );
});

test("only the scroll's own answer reads as its lines, exactly once", () => {
	const body = Buffer.alloc( 6 );
	body.writeUInt32LE( 3789 );
	body.writeUInt16LE( 1, 4 );
	const sent = [];
	const g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( {
		refItemSnapshot: [ { refObjId: 3789, typeFlags: 0x5eec } ],
		equipItems: [ { slot: 13, refObjId: 3789, body: [ ...body ] } ]
	} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	g.take();
	// No use in flight: the byte is the native mapper's, which is silent.
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF9 ) }, 0 );
	assert.deepEqual( defined( defined( g.take() ).notices ), [] );
	// The scroll's use goes out with the name as its tail...
	g.command( { kind: "item-use", slot: 13, renameName: "NewName" }, 0 );
	const use = defined( sent.find( f => f.opcode === 0x75bd ) );
	assert.deepEqual( [ ...use.payload.slice( 3 ) ], [ ...new TextEncoder().encode( "NewName" ) ] );
	// ...so its refusal answer reads as the scroll's line, once.
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF9 ) }, 0 );
	const notice = defined( defined( defined( g.take() ).notices ).at( -1 ) );
	assert.equal( notice.text, nameChangeFailureText( 0xF9 ) );
	assert.equal( notice.nativeType, 5 );
	// The flight is retired: the same byte again is the native mapper's.
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xF9 ) }, 0 );
	assert.deepEqual( defined( defined( g.take() ).notices ).map( n => n.sequence ), [ notice.sequence ] );
	g.dispose();
});

test("the rename notice channel shows as a server notification", () => {
	assert.equal( isServerNoticeChannel( 0x83 ), true );
	const text = "OldName is now known as NewName";
	const wide = new Uint8Array( 3 + text.length * 2 );
	const view = new DataView( wide.buffer );
	wide[0] = 0x83;
	view.setUint16( 1, text.length, true );
	for ( let i = 0; i < text.length; i++ ) view.setUint16( 3 + 2 * i, text.charCodeAt( i ), true );
	const notice = serverNotification( 0x3667, wide );
	assert.equal( notice?.text, text );
	assert.equal( notice?.jobKill, undefined );
	assert.equal( notice?.dropPlus, undefined );
	assert.equal( notice?.itemBreak, undefined );
});

const characterRow = {
	id: 1,
	name: "fixture",
	level: 1,
	raceIndex: 0,
	gender: 0,
	figureIndex: 0,
	heightIndex: 0,
	volumeIndex: 0,
	weaponIndex: 0,
	protectorIndex: 0,
	maxHp: 100,
	maxMp: 100,
	armorSelected: true,
	weaponSelected: true,
	deletePending: false,
	visualLoadout: {
		modelCodename: "CHAR_TEST",
		animationSetName: "test",
		items: [],
		avatars: [],
		heightScale: 1,
		volumeScale: 1
	}
};

test("a roster row carries a reasoned rename cancellation, and only those", () => {
	const decoder = createSessionDecoder();
	const answer = row => ({
		characterRosterContractVersion: 2,
		action: 2,
		nativeResult: 1,
		characters: [ row ]
	});
	const [row] = decoder.roster( answer( { ...characterRow, renameCancelled: "taken" } ) );
	assert.equal( row?.renameCancelled, "taken" );
	const [plain] = decoder.roster( answer( characterRow ) );
	assert.equal( plain?.renameCancelled, undefined );
	assert.throws( () => decoder.roster( answer( { ...characterRow, renameCancelled: "bogus" } ) ) );
	assert.match( renameCancelledText( "taken", "fixture" ), /fixture.*taken/ );
	assert.match( renameCancelledText( "guild-war", "fixture" ), /at war/ );
});
