/*
===========================================================================

gm-setspeed.test.mjs - the port's /SETSPEED console command (GM tooling)

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
test("/SETSPEED percent sends subcommand 0xF1 and the u16 percent, in any case", () => {
	for ( const line of [ "/SETSPEED 300", "/setspeed 300", "/SetSpeed 300" ] ) {
		const frame = gmRequest( line );
		assert.equal( frame?.opcode, 0x75b6, line );
		assert.deepEqual( [ ...(frame?.payload ?? []) ], [ 0xf1, 0x2c, 0x01 ], line );
	}
	// A bare /SETSPEED asks for normal speed.
	assert.deepEqual( [ ...(gmRequest( "/SETSPEED" )?.payload ?? []) ], [ 0xf1, 100, 0 ] );
});

/*
================
bounds
================
*/
test("/SETSPEED refuses a value outside 10..1000 or a non-number", () => {
	for (
		const line of [
			"/SETSPEED 9",
			"/SETSPEED 1001",
			"/SETSPEED -5",
			"/SETSPEED 1.5",
			"/SETSPEED fast",
			"/SETSPEED 100 200"
		]
	) {
		assert.equal( gmRequest( line ), null, line );
	}
	assert.deepEqual( [ ...(gmRequest( "/SETSPEED 10" )?.payload ?? []) ], [ 0xf1, 10, 0 ] );
	assert.deepEqual( [ ...(gmRequest( "/SETSPEED 1000" )?.payload ?? []) ], [ 0xf1, 0xe8, 0x03 ] );
});

/*
================
reply
================
*/
test("the /SETSPEED reply prints the percent in force or the refusal", () => {
	assert.deepEqual( gmReply( Uint8Array.of( 1, 0xf1, 0x2c, 0x01 ) ), {
		console: true,
		text: "-> speed set to 300%"
	} );
	assert.deepEqual( gmReply( Uint8Array.of( 2, 0xf1 ) ), { console: true, text: "Failed. -> speed change refused" } );
	assert.throws( () => gmReply( Uint8Array.of( 1, 0xf1, 0x2c ) ) );
});
