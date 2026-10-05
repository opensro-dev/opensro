/*
===========================================================================

spawn-fade.test.mjs - CIDecoAppear's spawn alpha ramp

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { spawnFadeAlpha, spawnFadeKind, SPAWN_FADE_SECONDS } = await import(
	sourceFileUrl( "src/engine/foundation/animation/spawn-fade.ts" ).href
);

test("the spawn ramp truncates the alpha byte and finishes at two seconds", () => {
	assert.equal( SPAWN_FADE_SECONDS, 2 );
	assert.equal( spawnFadeAlpha( 0 ), 0 );
	assert.equal( spawnFadeAlpha( -1 ), 0 );
	// 255 / 2 s: one second in is byte 127, not 127.5.
	assert.equal( spawnFadeAlpha( 1 ), 127 / 255 );
	assert.equal( spawnFadeAlpha( 0.004 ), 0 );
	assert.equal( spawnFadeAlpha( 0.008 ), 1 / 255 );
	assert.ok( spawnFadeAlpha( 1.99 ) < 1 );
	assert.equal( spawnFadeAlpha( 2 ), 1 );
	assert.equal( spawnFadeAlpha( 9 ), 1 );
});

test("players, monsters and COS fade in; NPCs and objects appear at once", () => {
	for ( const kind of [ "player", "local-player", "monster", "cos" ] ) {
		assert.equal( spawnFadeKind( kind ), true, kind );
	}
	for ( const kind of [ "npc", "ground-item", "teleport", "script-object" ] ) {
		assert.equal( spawnFadeKind( kind ), false, kind );
	}
});
