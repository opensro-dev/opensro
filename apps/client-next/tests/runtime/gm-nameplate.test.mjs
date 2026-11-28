/*
===========================================================================

gm-nameplate.test.mjs - the port's GM /NAMEPLATE ON and OFF

Port-only, not native (owner 2026-11-28). /NAMEPLATE ON|OFF composes
subcommand 0xFD with a u8 switch (1 on, 0 off); anything else sends
nothing. The server's reply carries a text on both results. The byte
layout is the server's decoder (apps/server/internal/game/gmcommand/
wire.go, SubNameplate). The server then tells every player through
0x3FD3, which the worker folds into the roster.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmReply } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );
const { decodeStaffNameplates, withNameplates } = await import(
	"../../src/engine/foundation/gameplay/staff-roster.ts"
);

/*
================
sized

u16 length + UTF-8 bytes.
================
*/
function sized( text ) {
	const bytes = new TextEncoder().encode( text );
	return [ bytes.length & 255, bytes.length >> 8, ...bytes ];
}

test("/NAMEPLATE ON and OFF send the switch in any case", () => {
	assert.equal( gmRequest( "/NAMEPLATE ON" )?.opcode, 0x75b6 );
	assert.deepEqual( [ ...(gmRequest( "/NAMEPLATE ON" )?.payload ?? []) ], [ 0xfd, 1 ] );
	assert.deepEqual( [ ...(gmRequest( "/nameplate off" )?.payload ?? []) ], [ 0xfd, 0 ] );
	assert.deepEqual( [ ...(gmRequest( "/Nameplate   Off  " )?.payload ?? []) ], [ 0xfd, 0 ] );
});

test("/NAMEPLATE without ON or OFF sends nothing", () => {
	for ( const line of [ "/NAMEPLATE", "/NAMEPLATE TOGGLE", "/NAMEPLATE ON NOW", "/NAMEPLATE 1" ] ) {
		assert.equal( gmRequest( line ), null, line );
	}
});

test("the /NAMEPLATE replies print the server's text on both results", () => {
	const reply = ( result, text ) => Uint8Array.of( result, 0xfd, ...sized( text ) );
	assert.deepEqual( gmReply( reply( 1, "nameplate glow off" ) ), { console: true, text: "-> nameplate glow off" } );
	assert.deepEqual( gmReply( reply( 2, "nameplate not switched" ) ), {
		console: true,
		text: "Failed. -> nameplate not switched"
	} );
});

test("0x3FD3 decodes whole or not at all and folds into the roster", () => {
	const frame = Uint8Array.of( 1, 0, ...sized( "Numz" ) );
	assert.deepEqual( decodeStaffNameplates( frame ), [ "Numz" ] );
	assert.deepEqual( decodeStaffNameplates( Uint8Array.of( 0, 0 ) ), [] );
	for ( const bad of [ Uint8Array.of( 1 ), Uint8Array.of( 1, 0, 4, 0, 65 ), Uint8Array.of( 0, 0, 9 ) ] ) {
		assert.throws( () => decodeStaffNameplates( bad ) );
	}
	const roster = [
		{ name: "numz", tag: "[GM]", nameColor: 0xffffd87a, chatColor: 0xffffaec3 },
		{ name: "Harry", tag: "[MOD]", nameColor: 0xff40c0ff, chatColor: 0xff40c0ff }
	];
	const off = withNameplates( roster, [ "Numz" ] );
	assert.equal( off[0].nameplateOff, true );
	// An unchanged entry keeps its object, so nothing downstream redraws it.
	assert.equal( off[1], roster[1] );
	// Back on: the entry carries no switch again.
	assert.equal( "nameplateOff" in withNameplates( off, [] )[0], false );
});
