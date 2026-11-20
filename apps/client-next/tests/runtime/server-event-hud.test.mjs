/*
===========================================================================

server-event-hud.test.mjs - the Server Mission gauge widget (v1.696 native)

The later client's CNIFServerEvent at its nifserverevent.2dt rects,
anchored to the right edge; its gauge fill and "count/goal" text; shown
whenever the server sends a mission, hidden by its close button until the
mission goes away. The production UI's draw is server-event-hud-ui.test.mjs
(asset-backed).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { serverEventLayout, gaugeRatio, gaugeText, createServerEventHud } = await import(
	"../../src/engine/runtime/ui/hud/server-event-hud.ts"
);

const GAUGE = { id: 30001, count: 59, goal: 118 };

test("the widget sits at the native rects, 244 px from the right edge", () => {
	assert.deepEqual( serverEventLayout( 1024 ), {
		frame: [ 780, 56, 120, 24 ],
		gauge: [ 803, 59, 80, 4 ],
		text: [ 802, 67, 80, 13 ],
		snow: [ 779, 56, 24, 24 ],
		close: [ 885, 61, 16, 16 ]
	} );
	assert.deepEqual( serverEventLayout( 1280 ).frame, [ 1036, 56, 120, 24 ] );
});

test("with the minimap drawn, the widget sits just under it, clear of the screen edge", () => {
	// The owner's placement (2026-11-17): the native spot covers the pet's
	// portrait on this HUD. The widget spans the snow icon to the close
	// button, 122 px; centred under the minimap, then 8 px further from the
	// right edge, 2 px under the minimap.
	assert.deepEqual( serverEventLayout( 1024, [ 895, 0, 129, 129 ] ), {
		frame: [ 891, 131, 120, 24 ],
		gauge: [ 914, 134, 80, 4 ],
		text: [ 913, 142, 80, 13 ],
		snow: [ 890, 131, 24, 24 ],
		close: [ 996, 136, 16, 16 ]
	} );
});

test("the gauge fills count over goal, never past either end", () => {
	assert.equal( gaugeRatio( 59, 118 ), .5 );
	assert.equal( gaugeRatio( 5, 0 ), 0 );
	assert.equal( gaugeRatio( 150, 118 ), 1 );
	assert.equal( gaugeRatio( 0, 118 ), 0 );
	assert.equal( gaugeText( 7, 118 ), "7/118" );
});

test("a mission shows; close hides it until the mission goes away", () => {
	const hud = createServerEventHud();
	hud.reconcile( undefined );
	assert.equal( hud.shown(), null );
	hud.reconcile( GAUGE );
	assert.deepEqual( hud.shown(), GAUGE );
	hud.close();
	assert.equal( hud.shown(), null );
	hud.reconcile( { ...GAUGE, count: 60 } );
	assert.equal( hud.shown(), null, "a new count keeps it closed" );
	hud.reconcile( undefined );
	hud.reconcile( GAUGE );
	assert.deepEqual( hud.shown(), GAUGE, "the next mission shows again" );
});

test("a new login forgets the close; the same session keeps it", () => {
	const hud = createServerEventHud();
	hud.reconcile( GAUGE );
	hud.close();
	hud.reconcile( GAUGE );
	assert.equal( hud.shown(), null );
	hud.reset();
	hud.reconcile( GAUGE );
	assert.deepEqual( hud.shown(), GAUGE );
});
