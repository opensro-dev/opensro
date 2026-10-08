/*
===========================================================================

window-position-lifecycle.test.mjs - placement through the production UI owner

Deferred assets isolate session lifetime from rendering readiness. Shutdown,
retained travel and fresh viewport admission use the actual UI entry points.
Native OnCreate loads saved origins, then its layout overwrites eagerly
created main/map windows; lazy guide and extended quickslot keep theirs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );
const { defaultExtendedQuickslot } = await import( "../../src/engine/foundation/ui/extended-quickslot.ts" );

/*
================
fixture
================
*/
function fixture() {
	const saved = [];
	const ui = createUi(
		{ available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		() => {},
		() => {},
		() => {},
		"https://fixture.invalid/",
		"https://fixture.invalid/",
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{ saveWindowPositions: value => saved.push( value ) }
	);
	/** @type {import("../../src/engine/contracts/ui.ts").UiView} */
	const view = {
		session: { phase: "world", revision: 1 },
		gameplay: null,
		entities: [],
		width: 1280,
		height: 720,
		worldReady: false
	};
	return { ui, view, saved };
}

/** @type {import("../../src/engine/foundation/ui/window-positions.ts").WindowPositions} */
const RECORD = {
	width: 1280,
	height: 720,
	windows: {
		mainPopup: [ 220, 90 ],
		worldMap: [ 240, 80 ],
		gameGuide: [ 100, 110 ],
		extendedQuickslot: [ 30, 60 ]
	}
};
const LAID_OUT = {
	...RECORD,
	windows: {
		mainPopup: [ 892, 242 ],
		store: [ 892, 242 ],
		storageRoom: [ 892, 242 ],
		exchange: [ 892, 242 ],
		worldMap: [ 314, 148 ],
		cosWindow: [ 314, 148 ],
		gameGuide: [ 100, 110 ],
		alchemyBox: [ 100, 110 ],
		autoPotion: [ 100, 110 ],
		extendedQuickslot: [ 30, 60 ]
	}
};

test("UI disposal saves an entered session once, without needing a logout frame", () => {
	const { ui, view, saved } = fixture();
	ui.event( { kind: "window-positions", value: RECORD } );
	ui.step( view, 0 );
	assert.equal( saved.length, 0 );
	ui.dispose();
	assert.deepEqual( saved, [ LAID_OUT ] );
	ui.dispose();
	assert.equal( saved.length, 1 );
});

test("logout writes before reset and later disposal does not overwrite it", () => {
	const { ui, view, saved } = fixture();
	ui.event( { kind: "window-positions", value: RECORD } );
	ui.step( view, 0 );
	ui.step( { ...view, session: { phase: "character-select", revision: 2 } }, 1 );
	assert.deepEqual( saved, [ LAID_OUT ] );
	ui.dispose();
	assert.equal( saved.length, 1 );
});

test("retained disconnect and reconnect keep the placement session until shutdown", () => {
	const { ui, view, saved } = fixture();
	ui.event( { kind: "window-positions", value: RECORD } );
	ui.step( view, 0 );
	const gameplay = /** @type {import("../../src/engine/contracts/ui.ts").UiView["gameplay"]} */ ({ localGid: 7 });
	ui.step( { ...view, gameplay, session: { phase: "disconnected", revision: 2 } }, 1 );
	ui.step( { ...view, gameplay, session: { phase: "reconnecting", revision: 3 } }, 2 );
	ui.step( { ...view, gameplay, session: { phase: "world", revision: 4 } }, 3 );
	assert.equal( saved.length, 0 );
	ui.dispose();
	assert.deepEqual( saved, [ LAID_OUT ] );
});

test("a fresh viewport mismatch clears UI-owned origins and ignores legacy quickslot coordinates", () => {
	const { ui, view, saved } = fixture();
	ui.event( { kind: "window-positions", value: RECORD } );
	ui.event( { kind: "quickslot-preferences", value: { ...defaultExtendedQuickslot(), position: [ 900, 500 ] } } );
	ui.step( view, 0 );
	ui.step( { ...view, session: { phase: "character-select", revision: 2 } }, 1 );
	assert.deepEqual( saved[0], LAID_OUT );
	ui.step( { ...view, width: 1024, height: 768, session: { phase: "world", revision: 3 } }, 2 );
	ui.dispose();
	assert.equal( saved.length, 2 );
	assert.equal( saved[1].width, 1024 );
	assert.equal( saved[1].height, 768 );
	assert.deepEqual( saved[1].windows.mainPopup, [ 636, 290 ] );
	assert.deepEqual( saved[1].windows.worldMap, [ 186, 172 ] );
	assert.deepEqual( saved[1].windows.extendedQuickslot, [ 186, 172 ], "old quickslot origin was not restored" );
});

test("UI disposal before world entry leaves the stored record untouched", () => {
	const { ui, view, saved } = fixture();
	ui.event( { kind: "window-positions", value: RECORD } );
	ui.step( { ...view, session: { phase: "signed-out", revision: 1 } }, 0 );
	ui.dispose();
	assert.deepEqual( saved, [] );
});

test("mismatched placement saves authored eager origins before layout and leaves the live session open", () => {
	const saved = [];
	const f = uiFixture( undefined, undefined, undefined, undefined, undefined, undefined, undefined, {
		saveWindowPositions: value => saved.push( value )
	} );
	try {
		f.ui.event( { kind: "window-positions", value: RECORD } );
		f.ui.step( f.state, 0 );
		assert.equal( saved.length, 1, "existing mismatched file is replaced as HUD creation completes" );
		assert.deepEqual( saved[0].windows.mainPopup, [ 595, 262 ] );
		assert.deepEqual( saved[0].windows.worldMap, [ 100, 100 ] );
		assert.deepEqual(
			saved[0].windows.extendedQuickslot,
			[ 100, 100 ],
			"quickslot does not exist in prelayout save"
		);
		f.ui.step( { ...f.state }, 100 );
		assert.equal( saved.length, 1, "initial snapshot is written once" );
		f.dispose();
		assert.equal( saved.length, 2, "the initial snapshot did not retire this session" );
		assert.deepEqual( saved[1].windows.mainPopup, [ 1212, 422 ] );
		assert.deepEqual( saved[1].windows.worldMap, [ 474, 238 ] );
	} finally {
		f.dispose();
	}
});

test("resizing the map saves its displayed clamped origin rather than its old drag coordinates", () => {
	const saved = [];
	const f = uiFixture( undefined, undefined, undefined, undefined, undefined, undefined, undefined, {
		saveWindowPositions: value => saved.push( value )
	} );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyM" } );
		f.ui.step( f.state, 1 );
		f.ui.event( { kind: "drag", id: "map-drag", dx: 700, dy: 500 } );
		f.ui.step( f.state, 2 );
		const resized = { ...f.state, width: 800, height: 600 };
		const semantics = f.ui.step( resized, 3 );
		const drag = semantics?.controls.find( control => control.id === "map-drag" );
		assert.ok( drag, "the resized map is visibly admitted" );
		const origin = [ drag.rect[0] - 20, drag.rect[1] - 4 ];
		assert.deepEqual( origin, [ 148, 176 ] );
		f.dispose();
		assert.deepEqual( saved.at( -1 ).windows.worldMap, origin );
	} finally {
		f.dispose();
	}
});

for ( const restored of [ false, true ] ) {
	test(`tiny viewport ${restored ? "remembered" : "default"} guide and quickslot origins stay stable after first display`, () => {
		const saved = [];
		const f = uiFixture( undefined, undefined, undefined, undefined, undefined, undefined, undefined, {
			saveWindowPositions: value => saved.push( value )
		} );
		try {
			const view = { ...f.state, width: 300, height: 200 };
			if ( restored ) {
				f.ui.event( {
					kind: "window-positions",
					value: {
						width: 300,
						height: 200,
						windows: { gameGuide: [ 0, 0 ], extendedQuickslot: [ 0, 0 ] }
					}
				} );
			}
			f.ui.step( view, 0 );
			f.ui.event( { kind: "activate", id: "open-window:Game Guide" } );
			const semantics = f.ui.step( view, 1 );
			const drag = semantics?.controls.find( control => control.id === "guide-drag" );
			assert.ok( drag, "guide has been instantiated" );
			const origin = [ drag.rect[0] - 10, drag.rect[1] - 5 ];
			assert.deepEqual( origin, restored ? [ -120, -252 ] : [ -60, -126 ] );
			f.ui.step( { ...view }, 200 );
			f.dispose();
			assert.deepEqual( saved.at( -1 ).windows.gameGuide, origin );
			assert.deepEqual( saved.at( -1 ).windows.extendedQuickslot, restored ? [ 0, -12 ] : [ 194, 181 ] );
		} finally {
			f.dispose();
		}
	});
}
