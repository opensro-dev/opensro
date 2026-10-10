/*
===========================================================================

gm-silk.test.mjs - the port's /SILK console command (operator tooling)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmReply } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );

/*
================
request
================
*/
test("/SILK name amount sends subcommand 0xF0, the name and the u32 amount", () => {
	const frame = gmRequest( "/SILK Tester 100000" );
	assert.equal( frame?.opcode, 0x75b6 );
	const p = frame?.payload ?? new Uint8Array( 0 ), v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	assert.equal( p[0], 0xf0 );
	assert.equal( v.getUint16( 1, true ), 6 );
	assert.equal( new TextDecoder().decode( p.subarray( 3, 9 ) ), "Tester" );
	assert.equal( v.getUint32( 9, true ), 100000 );
	assert.equal( p.length, 13 );
});

/*
================
bounds
================
*/
test("/SILK refuses a missing, zero, negative, fractional or oversized amount", () => {
	for (
		const line of [
			"/SILK Tester",
			"/SILK Tester 0",
			"/SILK Tester -5",
			"/SILK Tester 1.5",
			"/SILK Tester 1000001"
		]
	) {
		assert.equal( gmRequest( line ), null, line );
	}
	assert.equal( gmRequest( "/SILK Tester 1000000" )?.payload.length, 13 );
});

/*
================
reply
================
*/
test("the /SILK reply prints the new balance or the refusal", () => {
	assert.deepEqual( gmReply( Uint8Array.of( 1, 0xf0, 0xa0, 0x86, 0x01, 0x00 ) ), {
		console: true,
		text: "-> silk granted, balance 100000"
	} );
	assert.deepEqual( gmReply( Uint8Array.of( 2, 0xf0 ) ), { console: true, text: "Failed. -> silk grant refused" } );
});
