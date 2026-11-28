/*
===========================================================================

gm-weather.test.mjs - the port's /WEATHER console command (GM tooling)

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
test("/WEATHER kind [amount] [minutes] sends subcommand 0xFC, u8 kind, u8 amount, u16 minutes, in any case", () => {
	for ( const line of [ "/WEATHER snow 200 30", "/weather SNOW 200 30", "/Weather Snow 200 30" ] ) {
		const frame = gmRequest( line );
		assert.equal( frame?.opcode, 0x75b6, line );
		assert.deepEqual( [ ...(frame?.payload ?? []) ], [ 0xfc, 3, 200, 30, 0 ], line );
	}
	// Missing numbers are zero: the server's defaults.
	assert.deepEqual( [ ...(gmRequest( "/WEATHER rain" )?.payload ?? []) ], [ 0xfc, 2, 0, 0, 0 ] );
	assert.deepEqual( [ ...(gmRequest( "/WEATHER clear 150" )?.payload ?? []) ], [ 0xfc, 1, 150, 0, 0 ] );
	assert.deepEqual( [ ...(gmRequest( "/WEATHER snow 255 120" )?.payload ?? []) ], [ 0xfc, 3, 255, 120, 0 ] );
});

/*
================
bounds
================
*/
test("/WEATHER refuses an unknown kind, a bad number or an extra word", () => {
	for (
		const line of [
			"/WEATHER",
			"/WEATHER fog",
			"/WEATHER snow 256",
			"/WEATHER snow -1",
			"/WEATHER snow lots",
			"/WEATHER snow 100 121",
			"/WEATHER snow 100 10 now"
		]
	) {
		assert.equal( gmRequest( line ), null, line );
	}
});

/*
================
reply
================
*/
test("the /WEATHER reply prints the server's text on both results, and only an accepted one is a weather set", () => {
	const text = "snow 200 for 30 min on climate 1001";
	const bytes = new TextEncoder().encode( text );
	const ok = Uint8Array.of( 1, 0xfc, bytes.length, 0, ...bytes );
	// An accepted /WEATHER releases the console's weather hold (gameplay.ts);
	// a refusal (the flag off, a region without weather) leaves it standing.
	assert.deepEqual( gmReply( ok ), { console: true, text: "-> " + text, weatherSet: true } );
	const why = new TextEncoder().encode( "region 0x0000 has no weather" );
	const failed = Uint8Array.of( 2, 0xfc, why.length, 0, ...why );
	assert.deepEqual( gmReply( failed ), { console: true, text: "Failed. -> region 0x0000 has no weather" } );
});
