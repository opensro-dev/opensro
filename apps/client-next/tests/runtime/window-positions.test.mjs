/*
===========================================================================

window-positions.test.mjs - remembered window positions (wndpos.dat)

6A06B0 reopens the ten remembered windows where the last session left
them, only at the screen size they were saved at; 6A01B0 writes them back
at logout, keeping the entry position of a window never opened.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const positions = await load( "src/engine/foundation/ui/window-positions.ts" );
const { createWindowPlacement } = await load( "src/engine/runtime/ui/hud/window-placement.ts" );

test("the remembered set is 6A01B0's windows in its order, less the extended quickslot", () => {
	assert.deepEqual(
		positions.rememberedWindows().map( w => w.nativeId ),
		[ 0x19, 0x0f, 0x13, 0x1a, 0x1c, 0x78, 0x20, 0x2c, 0x87 ]
	);
});

test("stored records are validated", () => {
	const good = { width: 1280, height: 720, windows: { store: [ 10, 20 ], worldMap: [ 100, 100 ] } };
	assert.deepEqual( positions.windowPositions( good ), good );
	assert.throws( () => positions.windowPositions( { ...good, width: 0 } ) );
	assert.throws( () => positions.windowPositions( { ...good, windows: { chat: [ 1, 2 ] } } ) );
	assert.throws( () => positions.windowPositions( { ...good, windows: { store: [ 1 ] } } ) );
	assert.throws( () => positions.windowPositions( { ...good, windows: { store: [ 1, Infinity ] } } ) );
});

test("positions apply only at the screen size they were saved at", () => {
	const saved = { width: 1280, height: 720, windows: { store: [ 10, 20 ] } };
	assert.deepEqual( positions.positionsForViewport( saved, 1280, 720 ), saved.windows );
	assert.deepEqual( positions.positionsForViewport( saved, 1920, 1080 ), {} );
	assert.deepEqual( positions.positionsForViewport( null, 1280, 720 ), {} );
});

test("a session reopens its windows where the last one left them", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { store: [ 300, 40 ], mainPopup: [ 500, 60 ] } } );
	const entered = placement.enter( 1280, 720 );
	assert.deepEqual( entered.mainPopup, [ 500, 60 ] );
	assert.deepEqual( placement.frame( "window-drag:Shop", [ 0, 0, 254, 370 ], 1280, 720 ), [ 300, 40, 254, 370 ] );
	assert.equal( placement.enter( 1280, 720 ), null, "a teleport must not reseed the open session" );
	placement.drag( "window-drag:Shop", 5, 6 );
	placement.frame( "window-drag:Storage", [ 0, 0, 254, 317 ], 1280, 720 );
	const saved = placement.leave( 1280, 720, { mainPopup: [ 510, 70 ], worldMap: [ 100, 100 ] } );
	assert.deepEqual( saved, {
		width: 1280,
		height: 720,
		windows: { store: [ 305, 46 ], storageRoom: [ 0, 0 ], mainPopup: [ 510, 70 ], worldMap: [ 100, 100 ] }
	} );
	assert.equal( placement.leave( 1280, 720, {} ), null, "nothing to write without a session" );
});

test("a window not opened keeps its entry position; another screen size starts over", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { exchange: [ 200, 200 ] } } );
	placement.enter( 1280, 720 );
	assert.deepEqual( placement.leave( 1280, 720, {} ).windows, { exchange: [ 200, 200 ] } );
	placement.enter( 1920, 1080 );
	assert.deepEqual( placement.leave( 1920, 1080, {} ), { width: 1920, height: 1080, windows: {} } );
});
