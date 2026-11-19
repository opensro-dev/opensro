/*
===========================================================================

gm-setberserk.test.mjs - the port's /SETBERSERK console command (GM test tooling)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmReply } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );

test("/SETBERSERK name level sends subcommand 0xF3, the sized name and the level, in any case", () => {
	for ( const line of [ "/SETBERSERK Ayla 3", "/setberserk Ayla 3", "/SetBerserk Ayla 3" ] ) {
		const frame = gmRequest( line );
		assert.equal( frame?.opcode, 0x75b6, line );
		assert.deepEqual( [ ...(frame?.payload ?? []) ], [ 0xf3, 4, 0, 65, 121, 108, 97, 3 ], line );
	}
	assert.deepEqual( [ ...(gmRequest( "/SETBERSERK Ayla 0" )?.payload ?? []) ].at( -1 ), 0 );
	assert.deepEqual( [ ...(gmRequest( "/SETBERSERK Ayla 6" )?.payload ?? []) ].at( -1 ), 6 );
});

test("/SETBERSERK needs a name and a level from 0 to 6", () => {
	for (
		const line of [
			"/SETBERSERK",
			"/SETBERSERK Ayla",
			"/SETBERSERK Ayla 7",
			"/SETBERSERK Ayla -1",
			"/SETBERSERK Ayla 1.5",
			"/SETBERSERK Ayla x",
			"/SETBERSERK Ayla 2 3"
		]
	) {
		assert.equal( gmRequest( line ), null, line );
	}
});

test("the /SETBERSERK reply prints the level in force or the refusal", () => {
	assert.deepEqual( gmReply( Uint8Array.of( 1, 0xf3, 4 ) ), { console: true, text: "-> berserk level set to 4" } );
	assert.deepEqual( gmReply( Uint8Array.of( 2, 0xf3 ) ), {
		console: true,
		text: "Failed. -> berserk level change refused"
	} );
	assert.throws( () => gmReply( Uint8Array.of( 1, 0xf3 ) ), /Invalid GM berserk reply/ );
});

test("/HWANARENA ch|eu sends the coordinate warp to the trial arena's entry, in any case", () => {
	const decode = frame => {
		const v = new DataView( Uint8Array.from( frame.payload ).buffer );
		return [
			frame.payload[0],
			v.getUint16( 1, true ),
			v.getFloat32( 3, true ),
			v.getFloat32( 7, true ),
			v.getFloat32( 11, true )
		];
	};
	assert.deepEqual( decode( gmRequest( "/HWANARENA ch" ) ), [ 16, 22966, 1043, 52, 1087 ] );
	assert.deepEqual( decode( gmRequest( "/hwanarena EU" ) ), [ 16, 22454, 1195, 91, 653 ] );
	// The same frame /warp sends for the same point.
	assert.deepEqual( gmRequest( "/HWANARENA ch" ), gmRequest( "/warp 22966 1043 52 1087" ) );
	for ( const line of [ "/HWANARENA", "/HWANARENA xx", "/HWANARENA ch eu" ] ) {
		assert.equal( gmRequest( line ), null, line );
	}
});
