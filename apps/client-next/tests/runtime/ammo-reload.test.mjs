/*
===========================================================================

ammo-reload.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const { experimentalOptions, experimentalGameplay } = await load( "foundation/ui/experimental-options.ts" );
const { AMMO_RELOAD_CONTROL } = await load( "foundation/gameplay/commerce-controls.ts" );

/*
================
slice

The Experimental window's gameplay slice as the worker receives it.
================
*/
function slice( rowOn ) {
	return experimentalGameplay( experimentalOptions( rowOn ? { smallestAmmoStack: true } : {} ) );
}

test("the smallest-stack row maps to the gameplay slice and defaults off (#4527)", () => {
	assert.equal( slice( false ).smallestAmmoStack, false, "off is the native lowest-slot reload" );
	assert.equal( slice( true ).smallestAmmoStack, true );
});

test("an entry with the row on declares it once the world is ready, and only then", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( {} );
	g.experimental( slice( true ) );
	assert.deepEqual( sent, [], "nothing rides the wire before the world is ready" );
	g.worldReady();
	assert.deepEqual(
		sent.map( f => [ f.opcode, ...f.payload ] ),
		[ [ AMMO_RELOAD_CONTROL, 1 ] ],
		"the entry declares the smallest-stack choice"
	);
	g.dispose();
});

test("a row that stays off never puts a byte on the wire", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( {} );
	g.experimental( slice( false ) );
	g.worldReady();
	assert.deepEqual( sent, [], "the server's default is the native reload; nothing to declare" );
	g.experimental( slice( false ) );
	assert.deepEqual( sent, [], "re-applying the same choice sends nothing either" );
	g.dispose();
});

test("toggling the row under a live world declares each change once", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( {} );
	g.experimental( slice( false ) );
	g.worldReady();
	g.experimental( slice( true ) );
	g.experimental( slice( true ) );
	g.experimental( slice( false ) );
	assert.deepEqual(
		sent.map( f => [ f.opcode, ...f.payload ] ),
		[ [ AMMO_RELOAD_CONTROL, 1 ], [ AMMO_RELOAD_CONTROL, 0 ] ],
		"on then off, each declared once; an unchanged row is silent"
	);
	g.dispose();
});

test("a later entry with the row on declares again (#4527)", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( {} );
	g.experimental( slice( true ) );
	g.worldReady();
	g.reset();
	g.worldReady();
	assert.deepEqual(
		sent.map( f => [ f.opcode, ...f.payload ] ),
		[ [ AMMO_RELOAD_CONTROL, 1 ], [ AMMO_RELOAD_CONTROL, 1 ] ],
		"the server forgets the choice at restart; every entry speaks for itself"
	);
	g.dispose();
});
