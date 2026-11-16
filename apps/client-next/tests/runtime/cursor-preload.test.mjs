/*
===========================================================================

cursor-preload.test.mjs - world cursors load before they are shown

Hovering a teleport gate showed the system arrow first and the gate
cursor a moment later: the CSS rule named an image the browser had not
fetched yet. These pin the two halves of the fix: every world cursor is
fetched at creation, and a glyph whose image is not ready never replaces
the one already shown.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { presentableCursor, preloadCursors } = await import( "../../src/engine/foundation/ui/cursor-preload.ts" );
const { cursorAssetUrl, worldCursors } = await import( "../../src/engine/foundation/assets/native-assets.ts" );

test("every world cursor is fetched at creation, the teleport gate's included", async () => {
	const fetched = [], loaded = [];
	await preloadCursors( async url => {
		fetched.push( url );
	}, value => loaded.push( value ) );
	assert.deepEqual( fetched, worldCursors().map( cursorAssetUrl ) );
	assert.ok( loaded.includes( 0xa1 ), "the teleport gate cursor (0xA1) is preloaded" );
	assert.deepEqual( [ ...loaded ].sort(), [ ...worldCursors() ].sort() );
});

test("a cursor whose image failed still becomes presentable", async () => {
	const loaded = [];
	await preloadCursors( async url => {
		if ( url === cursorAssetUrl( 0xa1 ) ) throw new Error( "offline" );
	}, value => loaded.push( value ) );
	assert.ok( loaded.includes( 0xa1 ) );
});

test("a glyph not yet decoded keeps the shown one instead of the browser arrow", () => {
	const ready = new Set( [ 0x95 ] );
	assert.equal( presentableCursor( 0xa1, 0x95, ready ), 0x95 );
	ready.add( 0xa1 );
	assert.equal( presentableCursor( 0xa1, 0x95, ready ), 0xa1 );
});
