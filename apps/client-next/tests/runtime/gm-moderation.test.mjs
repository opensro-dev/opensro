/*
===========================================================================

gm-moderation.test.mjs - the port's GM moderation commands

/MUTE, /UNMUTE, /SHADOWMUTE, /UNSHADOWMUTE, /BLOCKLOGOUT and /ALLOWLOGOUT
compose subcommands 0xF5..0xFA; the server's replies carry a text on
both results. The byte layouts are the server's decoder
(apps/server/internal/game/gmcommand/wire.go).

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

test("/MUTE sends name, minutes, clause and the rejoined reason", () => {
	const frame = gmRequest( "/mute Juke 30 C1 spamming   trade chat" );
	assert.equal( frame?.opcode, 0x75b6 );
	assert.deepEqual( [ ...frame.payload ], [
		0xf5,
		...sized( "Juke" ),
		30,
		0,
		...sized( "C1" ),
		...sized( "spamming trade chat" )
	] );
	// No reason, a zero or non-numeric duration, or one past u16 sends nothing.
	for (
		const line of [
			"/MUTE Juke 30 C1",
			"/MUTE Juke 0 C1 spam",
			"/MUTE Juke ten C1 spam",
			"/MUTE Juke 65536 C1 spam"
		]
	) {
		assert.equal( gmRequest( line ), null, line );
	}
});

test("/SHADOWMUTE carries an optional note; the name-only commands take exactly one name", () => {
	assert.deepEqual( [ ...(gmRequest( "/SHADOWMUTE Juke rmt spam" )?.payload ?? []) ], [
		0xf7,
		...sized( "Juke" ),
		...sized( "rmt spam" )
	] );
	assert.deepEqual( [ ...(gmRequest( "/shadowmute Juke" )?.payload ?? []) ], [ 0xf7, ...sized( "Juke" ), 0, 0 ] );
	const named = { "/UNMUTE": 0xf6, "/unshadowmute": 0xf8, "/BlockLogout": 0xf9, "/ALLOWLOGOUT": 0xfa };
	for ( const [word, code] of Object.entries( named ) ) {
		assert.deepEqual( [ ...(gmRequest( `${word} Juke` )?.payload ?? []) ], [ code, ...sized( "Juke" ) ], word );
		assert.equal( gmRequest( `${word} Juke extra` ), null, word );
		assert.equal( gmRequest( word ), null, word );
	}
});

test("moderation replies print the server's text on both results", () => {
	const reply = ( result, code, text ) => Uint8Array.of( result, code, ...sized( text ) );
	assert.deepEqual( gmReply( reply( 1, 0xf5, "Juke muted until 2026-11-23 13:53 UTC" ) ), {
		console: true,
		text: "-> Juke muted until 2026-11-23 13:53 UTC"
	} );
	assert.deepEqual( gmReply( reply( 2, 0xf9, "OtherGm is a GM" ) ), {
		console: true,
		text: "Failed. -> OtherGm is a GM"
	} );
	assert.throws( () => gmReply( Uint8Array.of( 2, 0xf7 ) ) );
});
