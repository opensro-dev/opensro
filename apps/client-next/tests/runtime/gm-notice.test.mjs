/*
===========================================================================

gm-notice.test.mjs - the port's GM /NOTICE command

/NOTICE <text...> composes subcommand 0xFB with a u16-sized UTF-8 text;
the server's reply carries a text on both results. The byte layout is
the server's decoder (apps/server/internal/game/gmcommand/wire.go,
SubNotice).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmReply } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );

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

test("/NOTICE sends the rejoined text in any case", () => {
	const frame = gmRequest( "/notice Server   restart in 5 minutes" );
	assert.equal( frame?.opcode, 0x75b6 );
	assert.deepEqual( [ ...frame.payload ], [ 0xfb, ...sized( "Server restart in 5 minutes" ) ] );
	assert.deepEqual( [ ...(gmRequest( "/NOTICE Événement à Jangan" )?.payload ?? []) ], [
		0xfb,
		...sized( "Événement à Jangan" )
	] );
});

test("/NOTICE without text, or past the server's byte bound, sends nothing", () => {
	assert.equal( gmRequest( "/NOTICE" ), null );
	assert.equal( gmRequest( "/NOTICE    " ), null );
	assert.equal( gmRequest( "/NOTICE " + "a".repeat( 401 ) ), null );
	// The bound is bytes: 400 one-byte characters still leave the client,
	// and the server answers the banner's 100-character limit itself.
	assert.equal( gmRequest( "/NOTICE " + "a".repeat( 400 ) )?.payload.length, 403 );
});

test("the /NOTICE replies print the server's text on both results", () => {
	const reply = ( result, text ) => Uint8Array.of( result, 0xfb, ...sized( text ) );
	assert.deepEqual( gmReply( reply( 1, "notice sent" ) ), { console: true, text: "-> notice sent" } );
	assert.deepEqual( gmReply( reply( 2, "this command is off on this server (SRO_GM_NOTICE)" ) ), {
		console: true,
		text: "Failed. -> this command is off on this server (SRO_GM_NOTICE)"
	} );
});
