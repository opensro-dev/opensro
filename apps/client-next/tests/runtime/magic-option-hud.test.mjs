/*
===========================================================================

magic-option-hud.test.mjs - the grant window list's scroll position

The list shows five rows (6EB3E0 sets its CIFScrollManager to 5); the
arrows, the wheel and the thumb all move the first visible row, clamped to
the option count, and a new item starts the list over at the top.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createMagicOptionHud } = await import( "../../src/engine/runtime/ui/hud/magic-option-hud.ts" );

const OPTIONS = 14, LAST_TOP = 9;

test("scroll moves the first row and clamps to the option count", () => {
	const hud = createMagicOptionHud();
	hud.sync( true, 7 );
	hud.scroll( 1, OPTIONS );
	assert.equal( hud.state().top, 1 );
	hud.scroll( 100, OPTIONS );
	assert.equal( hud.state().top, LAST_TOP );
	hud.scroll( -100, OPTIONS );
	assert.equal( hud.state().top, 0 );
	hud.scroll( 1, 5 );
	assert.equal( hud.state().top, 0, "five options fit without scrolling" );
});

test("the thumb moves whole rows and keeps the remainder", () => {
	const hud = createMagicOptionHud();
	hud.sync( true, 7 );
	// A 90 pixel travel over 9 hidden rows: 10 pixels a row.
	hud.drag( 6, 90, OPTIONS );
	assert.equal( hud.state().top, 0 );
	hud.drag( 6, 90, OPTIONS );
	assert.equal( hud.state().top, 1, "the remainder of the first move counts" );
	hud.endDrag();
	hud.drag( 9, 90, OPTIONS );
	assert.equal( hud.state().top, 1, "a finished drag drops its remainder" );
	hud.drag( 1000, 90, OPTIONS );
	assert.equal( hud.state().top, LAST_TOP );
	hud.drag( -1000, 90, OPTIONS );
	assert.equal( hud.state().top, 0 );
});

test("a new item starts the list at the top", () => {
	const hud = createMagicOptionHud();
	hud.sync( true, 7 );
	hud.scroll( 3, OPTIONS );
	hud.sync( true, 8 );
	assert.equal( hud.state().top, 0 );
});
