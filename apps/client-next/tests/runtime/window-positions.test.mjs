/*
===========================================================================

window-positions.test.mjs - remembered window positions (wndpos.dat)

6A06B0 loads ten remembered origins at the same screen size; initial
layout overrides five eager windows. 6A01B0 writes origins at logout,
keeping the entry position of a window never opened.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const positions = await import( "../../src/engine/foundation/ui/window-positions.ts" );
const { createWindowPlacement } = await import( "../../src/engine/runtime/ui/hud/window-placement.ts" );

test("the remembered set is all ten 6A01B0 windows in its order", () => {
	assert.deepEqual(
		positions.rememberedWindows().map( w => w.nativeId ),
		[ 0x19, 0x0f, 0x13, 0x1a, 0x1c, 0x78, 0x20, 0x2c, 0x87, 0x85 ]
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
	const saved = positions.windowPositions( {
		width: 1280,
		height: 720,
		windows: { store: [ 10, 20 ], extendedQuickslot: [ 300, 50 ] }
	} );
	assert.deepEqual( positions.positionsForViewport( saved, 1280, 720 ), saved.windows );
	assert.deepEqual( positions.positionsForViewport( saved, 1920, 1080 ), {} );
	assert.deepEqual( positions.positionsForViewport( null, 1280, 720 ), {} );
});

test("a session restores lazy windows while eager windows keep layout defaults", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { alchemyBox: [ 300, 40 ], mainPopup: [ 500, 60 ] } } );
	const entered = placement.enter( 1280, 720 );
	assert.ok( entered );
	assert.equal( entered.mainPopup, undefined );
	assert.deepEqual( placement.frame( "window-drag:Alchemy", [ 0, 0, 254, 370 ], 1280, 720 ), [ 300, 40, 254, 370 ] );
	assert.equal( placement.enter( 1280, 720 ), null, "a teleport must not reseed the open session" );
	placement.drag( "window-drag:Alchemy", 5, 6 );
	placement.frame( "window-drag:Storage", [ 0, 0, 254, 317 ], 1280, 720 );
	const saved = placement.leave( 1280, 720, { mainPopup: [ 510, 70 ], worldMap: [ 100, 100 ] } );
	assert.deepEqual( saved, {
		width: 1280,
		height: 720,
		windows: {
			mainPopup: [ 510, 70 ],
			store: [ 510, 70 ],
			storageRoom: [ 0, 0 ],
			exchange: [ 0, 0 ],
			worldMap: [ 100, 100 ],
			cosWindow: [ 100, 100 ],
			gameGuide: [ 100, 100 ],
			alchemyBox: [ 305, 46 ],
			autoPotion: [ 305, 46 ],
			extendedQuickslot: [ 305, 46 ]
		}
	} );
	assert.equal( placement.leave( 1280, 720, {} ), null, "nothing to write without a session" );
});

test("a window not opened keeps its entry position; another screen size starts over", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { exchange: [ 200, 200 ] } } );
	placement.enter( 1280, 720 );
	assert.deepEqual( placement.leave( 1280, 720, {} )?.windows.exchange, [ 200, 200 ] );
	placement.reset();
	placement.enter( 1920, 1080 );
	const saved = placement.leave( 1920, 1080, {} );
	assert.ok( saved );
	assert.equal( saved.width, 1920 );
	assert.equal( saved.height, 1080 );
	assert.equal( Object.keys( saved.windows ).length, 10 );
	for ( const point of Object.values( saved.windows ) ) assert.deepEqual( point, [ 0, 0 ] );
});

test("resizing preserves an unopened window's remembered position at logout", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { exchange: [ 900, 600 ] } } );
	placement.enter( 1280, 720 );
	placement.frame( "window-drag:Shop", [ 20, 30, 254, 370 ], 960, 600 );
	const saved = placement.leave( 960, 600, {} );
	assert.ok( saved );
	assert.equal( saved.width, 960 );
	assert.equal( saved.height, 600 );
	assert.deepEqual( saved.windows.exchange, [ 900, 600 ] );
	assert.deepEqual( saved.windows.store, [ 20, 30 ] );
	placement.reset();
	assert.deepEqual( placement.enter( 960, 600 ), {
		cosWindow: [ 900, 600 ],
		gameGuide: [ 900, 600 ],
		alchemyBox: [ 900, 600 ],
		autoPotion: [ 900, 600 ],
		extendedQuickslot: [ 900, 600 ]
	} );
});

test("native initial layout replaces five eager origins and preserves five lazy origins", () => {
	const record = positions.windowPositions( {
		width: 1280,
		height: 720,
		windows: Object.fromEntries( positions.rememberedWindows().map( ( { key } ) => [ key, [ 50, 60 ] ] ) )
	} );
	assert.deepEqual( positions.positionsAfterLayout( record.windows ), {
		cosWindow: [ 50, 60 ],
		gameGuide: [ 50, 60 ],
		alchemyBox: [ 50, 60 ],
		autoPotion: [ 50, 60 ],
		extendedQuickslot: [ 50, 60 ]
	} );
	const placement = createWindowPlacement();
	placement.load( record );
	placement.enter( 1280, 720 );
	assert.deepEqual( placement.frame( "window-drag:Shop", [ 10, 20, 254, 370 ], 1280, 720 ), [ 10, 20, 254, 370 ] );
	assert.deepEqual( placement.frame( "window-drag:COS inventory", [ 10, 20, 200, 300 ], 1280, 720 ), [
		50,
		60,
		200,
		300
	] );
	assert.deepEqual( placement.frame( "window-drag:Auto Potion", [ 10, 20, 100, 100 ], 1280, 720 ), [
		50,
		60,
		100,
		100
	] );
});

test("lazy windows clamp remembered origins against their first real extent", () => {
	const placement = createWindowPlacement();
	placement.load( {
		width: 1280,
		height: 720,
		windows: { alchemyBox: [ 1270, 710 ], autoPotion: [ -20, -30 ] }
	} );
	placement.enter( 1280, 720 );
	assert.deepEqual(
		placement.frame( "window-drag:Alchemy", [ 0, 0, 300, 400 ], 1280, 720 ),
		[ 980, 320, 300, 400 ]
	);
	assert.deepEqual(
		placement.frame( "window-drag:Auto Potion", [ 0, 0, 200, 100 ], 1280, 720 ),
		[ -20, -30, 200, 100 ]
	);
	assert.deepEqual(
		placement.frame( "window-drag:Alchemy", [ 0, 0, 400, 450 ], 1280, 720 ),
		[ 980, 320, 400, 450 ],
		"later tab extent changes do not replay remembered-position clamping"
	);
});

test("a rejected viewport record is replaced immediately without retiring the session", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { autoPotion: [ 500, 300 ] } } );
	assert.deepEqual( placement.enter( 960, 600 ), {} );
	assert.equal( placement.needsInitialSave(), true );
	const replacement = placement.snapshot( 960, 600, { mainPopup: [ 100, 200 ] } );
	assert.ok( replacement );
	assert.equal( replacement.width, 960 );
	assert.equal( replacement.height, 600 );
	for ( const point of Object.values( replacement.windows ) ) assert.deepEqual( point, [ 100, 200 ] );
	assert.equal( placement.needsInitialSave(), false );
	assert.equal( placement.enter( 960, 600 ), null, "saving replacement must not reopen the active session" );
	placement.frame( "window-drag:Auto Potion", [ 20, 30, 100, 100 ], 960, 600 );
	placement.drag( "window-drag:Auto Potion", 10, 20 );
	const saved = placement.leave( 960, 600, { mainPopup: [ 100, 200 ] } );
	assert.deepEqual( saved?.windows.mainPopup, [ 100, 200 ] );
	assert.deepEqual( saved?.windows.autoPotion, [ 30, 50 ] );
	assert.deepEqual( saved?.windows.extendedQuickslot, [ 30, 50 ] );
	assert.equal( placement.snapshot( 960, 600, {} ), null );
});

test("missing files wait until logout and missing origins inherit the preceding native entry", () => {
	const placement = createWindowPlacement();
	placement.enter( 1280, 720 );
	assert.equal( placement.needsInitialSave(), false );
	assert.deepEqual(
		placement.leave( 1280, 720, {
			store: [ 10, 20 ],
			worldMap: [ 100, 100 ],
			alchemyBox: [ 30, 40 ]
		} )?.windows,
		{
			mainPopup: [ 0, 0 ],
			store: [ 10, 20 ],
			storageRoom: [ 10, 20 ],
			exchange: [ 10, 20 ],
			worldMap: [ 100, 100 ],
			cosWindow: [ 100, 100 ],
			gameGuide: [ 100, 100 ],
			alchemyBox: [ 30, 40 ],
			autoPotion: [ 30, 40 ],
			extendedQuickslot: [ 30, 40 ]
		}
	);
});

test("a rejected existing record requests immediate replacement while a missing file does not", () => {
	const placement = createWindowPlacement();
	placement.load( null );
	assert.deepEqual( placement.enter( 1280, 720 ), {} );
	assert.equal( placement.needsInitialSave(), true );
	assert.ok( placement.snapshot( 1280, 720, {} ) );
	assert.equal( placement.needsInitialSave(), false );
	assert.equal( placement.enter( 1280, 720 ), null );
	placement.leave( 1280, 720, {} );
	placement.reset();
	placement.enter( 1280, 720 );
	assert.equal( placement.needsInitialSave(), false, "replacement cleared the rejected-record state" );
});

test("manual lazy owners consume a remembered origin once with native upper-only clipping", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 300, height: 200, windows: { gameGuide: [ 100, -400 ] } } );
	placement.enter( 300, 200 );
	assert.deepEqual( placement.takeRemembered( "gameGuide", 300, 200, [ 420, 452 ] ), [ -120, -400 ] );
	assert.equal( placement.takeRemembered( "gameGuide", 200, 100, [ 420, 452 ] ), null );
	assert.equal( placement.enter( 300, 200 ), null );
	assert.equal( placement.takeRemembered( "gameGuide", 300, 200, [ 420, 452 ] ), null );
	placement.leave( 300, 200, { gameGuide: [ -120, -400 ] } );
	placement.reset();
	placement.enter( 300, 200 );
	assert.deepEqual( placement.takeRemembered( "gameGuide", 300, 200, [ 420, 452 ] ), [ -120, -400 ] );
});

test("a missing remembered origin leaves tiny-viewport default placement to its owner", () => {
	const placement = createWindowPlacement();
	placement.enter( 300, 200 );
	assert.equal( placement.takeRemembered( "gameGuide", 300, 200, [ 420, 452 ] ), null );
	assert.equal( placement.takeRemembered( "gameGuide", 300, 200, [ 420, 452 ] ), null );
});

test("closing a manual lazy control remembers its latest origin and rearms restoration on recreation", () => {
	const placement = createWindowPlacement();
	placement.load( { width: 1280, height: 720, windows: { gameGuide: [ 100, 110 ] } } );
	placement.enter( 1280, 720 );
	assert.deepEqual( placement.takeRemembered( "gameGuide", 1280, 720, [ 420, 452 ] ), [ 100, 110 ] );
	placement.remember( "gameGuide", [ 800, 250 ] );
	assert.deepEqual( placement.snapshot( 1280, 720, {} )?.windows.gameGuide, [ 800, 250 ] );
	assert.deepEqual( placement.takeRemembered( "gameGuide", 800, 600, [ 420, 452 ] ), [ 380, 148 ] );
	assert.equal( placement.takeRemembered( "gameGuide", 800, 600, [ 420, 452 ] ), null );
	placement.remember( "gameGuide", [ 380, 148 ] );
	assert.deepEqual( placement.leave( 800, 600, {} )?.windows.gameGuide, [ 380, 148 ] );
	placement.remember( "gameGuide", [ 0, 0 ] );
	placement.reset();
	placement.enter( 800, 600 );
	assert.deepEqual( placement.takeRemembered( "gameGuide", 800, 600, [ 420, 452 ] ), [ 380, 148 ] );
});
