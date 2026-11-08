/*
===========================================================================

gm-forge.test.mjs - the port's /FORGE console command (GM tooling)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmReply } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );

/*
================
frameText

Reads the u16-sized ASCII tail of a composed 0xF2 frame.
================
*/
function frameText( frame ) {
	const payload = frame?.payload ?? new Uint8Array();
	const size = payload[1] | payload[2] << 8;
	return new TextDecoder().decode( payload.subarray( 3, 3 + size ) );
}

/*
================
request
================
*/
test("/FORGE sends subcommand 0xF2 and the rejoined typed tail, in any case", () => {
	for ( const line of [ "/FORGE ITEM_X plus=12", "/forge ITEM_X plus=12", "/Forge ITEM_X plus=12" ] ) {
		const frame = gmRequest( line );
		assert.equal( frame?.opcode, 0x75b6, line );
		assert.equal( frame?.payload[0], 0xf2, line );
		assert.equal( frameText( frame ), "ITEM_X plus=12", line );
	}
	assert.equal( frameText( gmRequest( "/forge help ITEM_X" ) ), "help ITEM_X" );
	// Irregular spacing rejoins to single spaces, as typed tokens.
	assert.equal( frameText( gmRequest( "/FORGE  ITEM_X   blue=full" ) ), "ITEM_X blue=full" );
});

/*
================
bounds
================
*/
test("/FORGE refuses an empty, oversized or non-printable tail before the wire", () => {
	assert.equal( gmRequest( "/FORGE" ), null );
	assert.equal( gmRequest( "/FORGE " + "A".repeat( 1025 ) ), null );
	assert.notEqual( gmRequest( "/FORGE " + "A".repeat( 1024 ) ), null );
	assert.equal( gmRequest( "/FORGE ITEM_Ü plus=1" ), null );
	assert.equal( gmRequest( "/FORGE ITEM_X\0plus=1" ), null );
});

/*
================
reply
================
*/
test("the /FORGE reply prints each guide line or the refusal", () => {
	const line = "forge ITEM_X: keys";
	const payload = new Uint8Array( 4 + line.length );
	payload.set( [ 1, 0xf2, line.length, 0 ] );
	payload.set( new TextEncoder().encode( line ), 4 );
	assert.deepEqual( gmReply( payload ), { console: true, text: "-> " + line } );
	assert.deepEqual( gmReply( Uint8Array.of( 2, 0xf2 ) ), { console: true, text: "Failed. -> forge refused" } );
	assert.throws( () => gmReply( Uint8Array.of( 2, 0xf2, 0 ) ) );
	assert.throws( () => gmReply( Uint8Array.of( 1, 0xf2, 3, 0, 65 ) ) );
});
