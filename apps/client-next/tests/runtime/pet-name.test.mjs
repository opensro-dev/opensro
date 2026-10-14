/*
===========================================================================

pet-name.test.mjs - naming a pet (foundation/gameplay/pet-name.ts)

The wire against the server half's pinned vectors (#1034,
apps/server/internal/game/action/petname_test.go), the client's own name
check, and the worker: the one request for an unnamed pet, the refusal's
notice and the new name on the record.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { petNameError, petNameRefusal, petNameRequest, petNameUpdate } = await import(
	"../../src/engine/foundation/gameplay/pet-name.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const bytes = hex => Uint8Array.from( hex.split( " " ).map( b => parseInt( b, 16 ) ) );
// A name table that allows letters, digits and the underscore, as the
// server's [A-Za-z0-9_].
const RULES = {
	allowed: new Set( Array.from( "abcdefghijklmnopqrstuvwxyz0123456789_", c => c.charCodeAt( 0 ) ) ),
	forbidden: [ "gm" ],
	wholeWords: []
};

test("the request and the new name match the server's vectors", () => {
	assert.deepEqual( petNameRequest( 0x01020304, "Fluffy" ).payload, bytes( "04 03 02 01 06 00 46 6C 75 66 66 79" ) );
	assert.equal( petNameRequest( 0x01020304, "Fluffy" ).opcode, 0x7571 );
	assert.deepEqual(
		petNameUpdate( { opcode: 0x3508, payload: bytes( "04 03 02 01 05 06 00 46 6C 75 66 66 79" ) } ),
		{ gid: 0x01020304, name: "Fluffy" }
	);
	assert.equal( petNameUpdate( { opcode: 0x3508, payload: bytes( "04 03 02 01 04 10 27" ) } ), null, "another case" );
	assert.throws( () => petNameUpdate( { opcode: 0x3508, payload: bytes( "04 03 02 01 05 06 00 46" ) } ) );
	// A stored name is player text: an invalid byte decodes, never throws.
	assert.equal( petNameUpdate( { opcode: 0x3508, payload: bytes( "04 03 02 01 05 02 00 C3 28" ) } )?.name, "�(" );
});

test("each refusal shows its native notice", () => {
	const refusal = hex => petNameRefusal( { opcode: 0xb571, payload: bytes( hex ) } );
	assert.equal( refusal( "02 0E 44" ), "UIIT_MSG_COSPETERR_PETNAME_NOTPUT" );
	assert.equal( refusal( "02 0F 44" ), "UIIT_MSG_COSPETERR_PETNAME_SUMENESS" );
	assert.equal( refusal( "02 03 00" ), "UIIT_STT_ERR_COMMON_INVALID_TARGET" );
	assert.equal( refusal( "02 05 00" ), "UIIT_STT_ERR_COMMON_INVALID_OPERATION" );
	assert.equal( petNameRefusal( { opcode: 0x3508, payload: bytes( "02 05 00" ) } ), null );
	assert.throws( () => refusal( "01" ) );
});

test("the name box sends nothing outside 2..12 or the name rules", () => {
	assert.equal( petNameError( "Fluffy", RULES ), null );
	assert.equal( petNameError( "F", RULES ), "UIIT_MSG_COSPETERR_PETNAME_NOTPUT" );
	assert.equal( petNameError( "ThirteenChars", RULES ), "UIIT_MSG_COSPETERR_PETNAME_NOTPUT" );
	assert.equal( petNameError( "Fluf fy", RULES ), "UIIT_MSG_COSPETERR_PETNAME_NOTPUT", "a space is not allowed" );
	assert.equal( petNameError( "TheGm", RULES ), "UIIT_MSG_COSPETERR_PETNAME_NOTPUT", "a forbidden word" );
});

const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];

test("the worker names an unnamed pet once, shows a refusal and takes the new name", () => {
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	// An attack pet's reference: COS (0x1C6) of band 3.
	game.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [ { kind: "cos", refObjId: 103, tidWord: 3 << 11 | 0x1c6 } ]
	} );
	game.seed( { gid: 1, refObjId: 1907, kind: "player", name: "Player", regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	// 0x3158, band 3: hp, mp, exp, level, satiety, command mode, an empty
	// name, no bag, alive, bag slot 14.
	game.receive( {
		opcode: 0x3158,
		payload: Uint8Array.from( [
			...u32( 7 ),
			...u32( 103 ),
			...u32( 100 ),
			...u32( 0 ),
			...u32( 0 ),
			...u32( 0 ),
			1,
			16,
			39,
			...u32( 0 ),
			0,
			0,
			0,
			...u32( 0 ),
			14
		] )
	}, 0 );
	assert.equal( game.take()?.cosRecords?.find( r => r.gid === 7 )?.name, "" );
	game.command( { kind: "cos-name", gid: 7, name: "Fluffy" }, 0, undefined );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ ...petNameRequest( 7, "Fluffy" ).payload ] );
	game.receive( { opcode: 0xb571, payload: bytes( "02 0F 44" ) }, 1 );
	assert.equal( game.take()?.notices?.at( -1 )?.key, "UIIT_MSG_COSPETERR_PETNAME_SUMENESS" );
	game.receive( {
		opcode: 0x3508,
		payload: Uint8Array.from( [ ...u32( 7 ), 5, 6, 0, ...new TextEncoder().encode( "Fluffy" ) ] )
	}, 2 );
	assert.equal( game.take()?.cosRecords?.find( r => r.gid === 7 )?.name, "Fluffy" );
	// Named once: the command sends nothing more.
	const before = sent.length;
	game.command( { kind: "cos-name", gid: 7, name: "Other" }, 3, undefined );
	assert.equal( sent.length, before );
	game.dispose();
});

const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

/*
================
petWindow

The COS window on an attack pet's info tab, with name, and the published
control ids.
================
*/
function petWindow( name ) {
	const commands = [], f = uiFixture( command => commands.push( command ) );
	let now = 0, seen = null;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) seen = f.ui.step( f.state, now += 100 ) ?? seen;
		return (seen?.controls ?? []).map( control => control.id );
	};
	f.state.gameplay.cosRecords = [ { gid: 7, refObjId: 100, band: 3, hp: 100, mp: 0, status: 0, dead: false, name } ];
	// The HUD must have seen the world once before a window opens.
	step();
	f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
	return { f, commands, step };
}

test("the Name button shows only for an unnamed pet; a valid name is confirmed, then sent", () => {
	const named = petWindow( "Rex" );
	assert.ok( !named.step().includes( "cos-name" ), "a named pet has no Name button" );
	named.f.dispose();
	const { f, commands, step } = petWindow( "" );
	assert.ok( step().includes( "cos-name" ) );
	f.ui.event( { kind: "activate", id: "cos-name" } );
	assert.ok( step().includes( "pet-name" ) );
	// 6A59E0: one character is refused before anything is sent.
	f.ui.event( { kind: "edit", id: "pet-name", value: "F", start: 1, end: 1, composing: false } );
	f.ui.event( { kind: "activate", id: "pet-name-ok" } );
	assert.ok( step().includes( "pet-name" ), "the name box stays open" );
	f.ui.event( { kind: "edit", id: "pet-name", value: "Fluffy", start: 6, end: 6, composing: false } );
	f.ui.event( { kind: "activate", id: "pet-name-ok" } );
	assert.ok( !step().includes( "pet-name" ), "the confirm replaced the name box" );
	assert.equal( commands.length, 0, "nothing is sent before the confirm" );
	f.ui.event( { kind: "activate", id: "pet-name-ok" } );
	step();
	assert.deepEqual( commands.at( -1 ), { kind: "gameplay", command: { kind: "cos-name", gid: 7, name: "Fluffy" } } );
	f.dispose();
});
