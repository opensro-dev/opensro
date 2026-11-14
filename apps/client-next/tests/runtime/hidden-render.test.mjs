/*
===========================================================================

hidden-render.test.mjs - a backgrounded load still reaches the renderer

A hidden tab's maintenance frames skip the renderer, but scene admission
happens only inside it. The live incidents (frontend_loading_stalled at
loading-dock: dock ready, stage not ready, "Preparing scene graphics")
were dock loads left in a background tab. These cases pin which frames
still render.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
const { hiddenFrameRenders } = await import( "../../src/engine/foundation/rendering/hidden-render.ts" );

test("a frontend loading phase renders while hidden", () => {
	const loading = /** @type {const} */ ([ "loading-dock", "loading-create", "loading-race" ]);
	for ( const phase of loading ) {
		assert.equal( hiddenFrameRenders( phase, "character-select", false ), true, phase );
	}
});

test("a pending world entry renders while hidden until game-ready is sent", () => {
	assert.equal( hiddenFrameRenders( "loading-world", "world", false ), true );
	assert.equal( hiddenFrameRenders( "world", "world", true ), false );
});

test("a settled screen does not render while hidden", () => {
	const settled = /** @type {const} */ ([ "loading-title", "dock", "create", "login", "customize", "world" ]);
	for ( const phase of settled ) {
		assert.equal( hiddenFrameRenders( phase, "character-select", true ), false, phase );
	}
	assert.equal( hiddenFrameRenders( undefined, undefined, false ), false );
});
