/*
===========================================================================

world-map-compact.test.mjs - the M map on a phone keeps native scale

Port-only, below the native 800x600 minimum: the map window shrinks to the
compact layout's window area instead of being scaled down whole (labels at
a fraction of their size), its buttons stay inside it, and the fit measures
the window rect rather than the page tiles that reach past it through the
map clip. The desktop window keeps its authored 652 x 424, and the
renewal row's follow pin keeps its distance from the clip's right edge
on either (there is no captioned AUTO_MOVE button to anchor).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

// The renewal pin's distance from the map clip's right edge (594 + 36
// in a 645-wide render panel), and the window chrome beside the clip.
const PIN_RIGHT = 51;
const CHROME_RIGHT = 6;

/*
================
fixture

One map presentation at the given screen and compact window area; returns
the window box, the pan clip, the follow pin, the map's control ids
and the fit source.
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
	/** @type {{ box: number[], source: number[] | undefined }} */
	const seen = { box: [], source: undefined };
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
		hudData: { map: {} },
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
		closeButton: () => {},
		image: () => {}
	};
	try {
		hud.present( input );
		const find = ( /** @type {string} */ id ) => input.controls.find( ( /** @type {any} */ c ) => c.id === id );
		const ids = input.controls.map( ( /** @type {any} */ c ) => c.id );
		return { ...seen, pan: find( "map-pan" ).rect, pin: find( "map-pin" ).rect, ids };
	} finally {
		hud.dispose();
	}
}

test("a phone's map window fits the compact area at native scale, its buttons inside", () => {
	const bounds = [ 0, 0, 375, 510 ];
	const { box, pan, pin, ids, source } = present( 375, 667, bounds );
	// 375 - 2 x 4 wide, the authored height (fits under 510 - 8).
	assert.deepEqual( [ box[2], box[3] ], [ 367, 424 ] );
	assert.ok( box[0] >= 0 && box[0] + box[2] <= 375 && box[1] >= 0 && box[1] + box[3] <= 510 );
	// The pan surface is the window less its chrome, as on the desktop.
	assert.deepEqual( pan, [ box[0] + 6, box[1] + 34, box[2] - 12, box[3] - 40 ] );
	// The pin keeps its distance from the right edge, inside the window.
	assert.ok( pin[0] + pin[2] <= box[0] + box[2] );
	assert.equal( pan[0] + pan[2] - pin[0], PIN_RIGHT );
	assert.equal( ids.includes( "map-follow" ), false, "no captioned AUTO_MOVE button" );
	// The fit measures the window, not the page tiles past the clip.
	assert.deepEqual( source, box );
});

test("a short compact area shortens the window instead of scaling it", () => {
	const { box } = present( 375, 667, [ 0, 0, 375, 300 ] );
	assert.deepEqual( [ box[2], box[3] ], [ 367, 292 ] );
});

test("the desktop keeps the authored 652 x 424 window", () => {
	const { box, pin, ids } = present( 1200, 900 );
	assert.deepEqual( [ box[2], box[3] ], [ 652, 424 ] );
	assert.equal( pin[0] - box[0], 652 - CHROME_RIGHT - PIN_RIGHT );
	assert.equal( ids.includes( "map-follow" ), false, "no captioned AUTO_MOVE button" );
});

test("a compact area away from the origin clamps the window to the area, not the screen", () => {
	// Above the quickslots the compact window area starts below the
	// phone's status rows: its origin is not the screen's origin.
	const { box, pan, pin, source } = present( 375, 667, [ 16, 200, 340, 300 ] );
	// 340 - 2 x 4 wide, 300 - 8 high; the resting 100,100 is clamped
	// into the area on both axes (the far side measures from the
	// area's origin too).
	assert.deepEqual( box, [ 24, 200, 332, 292 ] );
	assert.deepEqual( pan, [ box[0] + 6, box[1] + 34, box[2] - 12, box[3] - 40 ] );
	assert.ok( pin[0] + pin[2] <= box[0] + box[2] );
	assert.equal( pan[0] + pan[2] - pin[0], PIN_RIGHT );
	assert.deepEqual( source, box );
});
