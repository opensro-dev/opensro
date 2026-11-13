/*
===========================================================================

world-map-compact.test.mjs - the M map on a phone keeps native scale

Port-only, below the native 800x600 minimum: the map window shrinks to the
compact layout's window area instead of being scaled down whole (labels at
a fraction of their size), its buttons stay inside it, and the fit measures
the window rect and its foot rather than the page tiles that reach past
it through the map clip. The desktop window keeps v1.188's 650 x 422
(worldmapguide.2dt) with the 34-deep foot below it, and the follow pin
keeps its distance from the window's right edge on either (there is no
captioned AUTO_MOVE button to anchor).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

// The pin's distance from the window's right edge (605 in a 650 window),
// the render panel's inset (5,33 and 5 on the other sides) and the foot
// below the window (412 + 44 - 422).
const PIN_RIGHT = 45;
const PANEL = [ 5, 33, 10, 38 ];
const FOOT_DEPTH = 34;

/*
================
panel

The render panel of a window box.
================
*/
function panel( box ) {
	return [ box[0] + PANEL[0], box[1] + PANEL[1], box[2] - PANEL[2], box[3] - PANEL[3] ];
}

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
	// 375 - 2 x 4 wide, the authored height (its foot fits under 510 - 8).
	assert.deepEqual( [ box[2], box[3] ], [ 367, 422 ] );
	assert.ok( box[0] >= 0 && box[0] + box[2] <= 375 && box[1] >= 0 && box[1] + box[3] + FOOT_DEPTH <= 510 );
	// The pan surface is the render panel, as on the desktop.
	assert.deepEqual( pan, panel( box ) );
	// The pin keeps its distance from the right edge, inside the window.
	assert.ok( pin[0] + pin[2] <= box[0] + box[2] );
	assert.equal( box[0] + box[2] - pin[0], PIN_RIGHT );
	assert.equal( ids.includes( "map-follow" ), false, "no captioned AUTO_MOVE button" );
	// The fit measures the window and its foot, not the page tiles past
	// the clip.
	assert.deepEqual( source, [ box[0], box[1], box[2], box[3] + FOOT_DEPTH ] );
});

test("a short compact area shortens the window instead of scaling it", () => {
	const { box } = present( 375, 667, [ 0, 0, 375, 300 ] );
	// 300 - 2 x 4, less the foot below the window.
	assert.deepEqual( [ box[2], box[3] ], [ 367, 300 - 8 - FOOT_DEPTH ] );
});

test("the desktop keeps the authored 650 x 422 window", () => {
	const { box, pin, ids } = present( 1200, 900 );
	assert.deepEqual( [ box[2], box[3] ], [ 650, 422 ] );
	assert.deepEqual( pin, [ box[0] + 605, box[1] + 39, 36, 36 ], "the pin at its layout place" );
	assert.equal( ids.includes( "map-follow" ), false, "no captioned AUTO_MOVE button" );
});

test("a compact area away from the origin clamps the window to the area, not the screen", () => {
	// Above the quickslots the compact window area starts below the
	// phone's status rows: its origin is not the screen's origin.
	const { box, pan, pin, source } = present( 375, 667, [ 16, 200, 340, 300 ] );
	// 340 - 2 x 4 wide, 300 - 8 high with the foot; the resting 100,100
	// is clamped into the area on both axes (the far side measures from
	// the area's origin too).
	assert.deepEqual( box, [ 24, 200, 332, 300 - 8 - FOOT_DEPTH ] );
	assert.deepEqual( pan, panel( box ) );
	assert.ok( pin[0] + pin[2] <= box[0] + box[2] );
	assert.equal( box[0] + box[2] - pin[0], PIN_RIGHT );
	assert.deepEqual( source, [ box[0], box[1], box[2], box[3] + FOOT_DEPTH ] );
});
