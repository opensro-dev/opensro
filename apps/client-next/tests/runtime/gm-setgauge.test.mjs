/*
===========================================================================

gm-setgauge.test.mjs - the port's /SETGAUGE console command (GM test tooling)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmReply } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );

test("/SETGAUGE name balls sends subcommand 0xF4, the sized name and the count, in any case", () => {
	for ( const line of [ "/SETGAUGE Ayla 5", "/setgauge Ayla 5", "/SetGauge Ayla 5" ] ) {
		const frame = gmRequest( line );
		assert.equal( frame?.opcode, 0x75b6, line );
		assert.deepEqual( [ ...(frame?.payload ?? []) ], [ 0xf4, 4, 0, 65, 121, 108, 97, 5 ], line );
	}
	assert.equal( [ ...(gmRequest( "/SETGAUGE Ayla 0" )?.payload ?? []) ].at( -1 ), 0 );
});

test("/SETGAUGE needs a name and a count from 0 to 5", () => {
	for (
		const line of [
			"/SETGAUGE",
			"/SETGAUGE Ayla",
			"/SETGAUGE Ayla 6",
			"/SETGAUGE Ayla -1",
			"/SETGAUGE Ayla x",
			"/SETGAUGE Ayla 2 3"
		]
	) {
		assert.equal( gmRequest( line ), null, line );
	}
});

test("the /SETGAUGE reply prints the balls in the gauge or the refusal", () => {
	assert.deepEqual( gmReply( Uint8Array.of( 1, 0xf4, 3 ) ), { console: true, text: "-> berserk gauge set to 3" } );
	assert.deepEqual( gmReply( Uint8Array.of( 2, 0xf4 ) ), { console: true, text: "Failed. -> gauge change refused" } );
	assert.throws( () => gmReply( Uint8Array.of( 1, 0xf4 ) ), /Invalid GM gauge reply/ );
});
