/*
===========================================================================

world-map-compact.test.mjs - the M map on a phone keeps native scale

Port-only, below the native 800x600 minimum: the map window shrinks to the
compact layout's window area instead of being scaled down whole (labels at
a fraction of their size), its buttons stay inside it, and the fit measures
the window rect rather than the page tiles that reach past it through the
map clip. The desktop window keeps its authored 652 x 424.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

// The authored automatic-move button sits near the 652-wide window's right edge.
const AUTO_MOVE_RECT = [ 560, 8, 80, 22 ];

/*
================
fixture

One map presentation at the given screen and compact window area; returns
the window box, the pan clip, the auto-move button and the fit source.
================
*/
/**
 * @param {number} w
 * @param {number} h
 * @param {number[]} [bounds]
 * @returns {{ box: number[], auto: number[], source: number[] | undefined, pan: number[] }}
 */
function present( w, h, bounds ) {
	const pose = { regionId: 97 * 256 + 160, x: 960, y: 0, z: 960, angle: 0 };
	const hud = createWorldMapHud( {
		assets: { available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		base: "http://fixture.invalid",
		experimental: { state: () => ({ saved: experimentalOptions() }), restore: () => {} }
	} );
	/** @type {{ box: number[], auto: number[], source: number[] | undefined }} */
	const seen = { box: [], auto: [], source: undefined };
	/** @type {any} */
	const input = {
		next: { entities: [] },
		game: { pose, social: { self: 1, localName: "Me", members: [] } },
		pose,
		panel: "Map",
		w,
		h,
		full: [ 0, 0, w, h ],
		...(bounds ? { bounds } : {}),
		hover: null,
		hudData: { map: { GDR_WM_BTN_AUTO_MOVE: { rect: AUTO_MOVE_RECT } } },
		trackedQuest: 0,
		minimapResources: { positions: () => undefined },
		mapTeleport: { view: () => {} },
		reverseScrollHud: { mapping: () => false },
		text: {
			path: () => null,
			run: ( /** @type {string} */ value ) => ({ width: value.length * 6 }),
			extentHeight: () => 12,
			quads: () => []
		},
		resources: { has: () => true, size: () => undefined },
		paths: [],
		quads: [],
		controls: [],
		hudCopy: ( /** @type {string} */ k ) => k,
		beginWindow: () => [ 0, 0, 0, 0 ],
		endWindow: ( /** @type {any} */ _mark, /** @type {any} */ _owner, /** @type {any} */ source ) => {
			seen.source = source;
		},
		windowBox: (
			/** @type {string} */ _caption,
			/** @type {number} */ x,
			/** @type {number} */ y,
			/** @type {number} */ mw,
			/** @type {number} */ mh
		) => {
			seen.box = [ x, y, mw, mh ];
		},
		authoredButton: () => {},
		authoredLabeledButton: ( /** @type {any} */ node, /** @type {number} */ mx, /** @type {number} */ my ) => {
			seen.auto = [ mx + node.rect[0], my + node.rect[1], node.rect[2], node.rect[3] ];
		},
		closeButton: () => {},
		image: () => {}
	};
	try {
		hud.present( input );
		const pan = input.controls.find( ( /** @type {any} */ c ) => c.id === "map-pan" ).rect;
		return { ...seen, pan };
	} finally {
		hud.dispose();
	}
}

test("a phone's map window fits the compact area at native scale, its buttons inside", () => {
	const bounds = [ 0, 0, 375, 510 ];
	const { box, pan, auto, source } = present( 375, 667, bounds );
	// 375 - 2 x 4 wide, the authored height (fits under 510 - 8).
	assert.deepEqual( [ box[2], box[3] ], [ 367, 424 ] );
	assert.ok( box[0] >= 0 && box[0] + box[2] <= 375 && box[1] >= 0 && box[1] + box[3] <= 510 );
	// The pan surface is the window less its chrome, as on the desktop.
	assert.deepEqual( pan, [ box[0] + 6, box[1] + 34, box[2] - 12, box[3] - 40 ] );
	// The automatic-move button keeps its distance from the right edge.
	assert.ok( auto[0] + auto[2] <= box[0] + box[2] );
	assert.equal( box[0] + box[2] - auto[0], 652 - AUTO_MOVE_RECT[0] );
	// The fit measures the window, not the page tiles past the clip.
	assert.deepEqual( source, box );
});

test("a short compact area shortens the window instead of scaling it", () => {
	const { box } = present( 375, 667, [ 0, 0, 375, 300 ] );
	assert.deepEqual( [ box[2], box[3] ], [ 367, 292 ] );
});

test("the desktop keeps the authored 652 x 424 window", () => {
	const { box, auto } = present( 1200, 900 );
	assert.deepEqual( [ box[2], box[3] ], [ 652, 424 ] );
	assert.equal( auto[0] - box[0], AUTO_MOVE_RECT[0] );
});

test("a compact area away from the origin clamps the window to the area, not the screen", () => {
	// Above the quickslots the compact window area starts below the
	// phone's status rows: its origin is not the screen's origin.
	const { box, pan, auto, source } = present( 375, 667, [ 16, 200, 340, 300 ] );
	// 340 - 2 x 4 wide, 300 - 8 high; the resting 100,100 is clamped
	// into the area on both axes (the far side measures from the
	// area's origin too).
	assert.deepEqual( box, [ 24, 200, 332, 292 ] );
	assert.deepEqual( pan, [ box[0] + 6, box[1] + 34, box[2] - 12, box[3] - 40 ] );
	assert.ok( auto[0] + auto[2] <= box[0] + box[2] );
	assert.equal( box[0] + box[2] - auto[0], 652 - AUTO_MOVE_RECT[0] );
	assert.deepEqual( source, box );
});
