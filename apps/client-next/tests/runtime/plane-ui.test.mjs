/*
===========================================================================

plane-ui.test.mjs - the map and minimap doors ask the plane owner (#1793 D)

Each door slice D moved onto featuresAt gets one cave and one outdoor case
here, so a door that stops asking (or asks the wrong row) fails: the hunting
guide's anchors and its window, the world map's town page and markers
(worldMap), and the minimap's floor queries (rooms). The minimap's floor art
(floorMinimap) is pinned by minimap.test.mjs.

A cave id below keeps an outdoor id's low bits and adds the dungeon bit, so
only the plane separates the two cases.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { decodeHuntingGuide } = await import( "../../src/engine/foundation/ui/hunting-guide.ts" );
const { createHuntingGuideHud } = await import( "../../src/engine/runtime/ui/hud/hunting-guide.ts" );
const { worldMapPageAt, worldMapPointMarkers } = await import( "../../src/engine/foundation/ui/world-map.ts" );
const { minimapFloorQueries } = await import( "../../src/engine/foundation/ui/minimap-floor.ts" );

const DUNGEON_BIT = 0x8000;
// A native town page's sector (hud-readouts.test.mjs: page 5).
const TOWN = { regionId: 105 * 256 + 79, x: 1000, y: 0, z: 1000, angle: 0 };
const CAVE_TOWN = { ...TOWN, regionId: TOWN.regionId | DUNGEON_BIT };
const FIELD = { regionId: 0x61a0, x: 960, y: 0, z: 960, angle: 0 };
const CAVE_FIELD = { ...FIELD, regionId: FIELD.regionId | DUNGEON_BIT };
// Larger than the native overview in both directions.
/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
const WHOLE_MAP = [ 0, 0, 1 << 20, 1 << 20 ];

/*
================
guideWith
================
*/
function guideWith( points ) {
	return {
		format: "sro-hunting-guide",
		version: 1,
		rows: [ { refObjId: 1933, name: "Mangyang", nameKey: "SN_MOB_CH_MANGNYANG", level: 1, points } ]
	};
}

/*
================
gameAt

A gameplay state that carries only the player's pose, the one field the
minimap's floor queries read.
================
*/
function gameAt( pose ) {
	return /** @type {import("../../src/engine/contracts/gameplay.ts").GameplayState} */ (
		/** @type {unknown} */ ({ pose })
	);
}

test("a hunting guide anchor in a cave is refused; the same offsets outdoors are kept", () => {
	// A far negative z (-6000 units, about 31 rows) pulls a cave id's row
	// (128 and up) back inside the outdoor grid's 128 rows, so the grid
	// bound cannot refuse it and only the plane test does.
	const offsets = { x: 0, z: -60000 };
	assert.equal( decodeHuntingGuide( guideWith( [ { regionId: 0x6001, ...offsets } ] ) ).regions.size, 1 );
	assert.throws(
		() => decodeHuntingGuide( guideWith( [ { regionId: 0x8001, ...offsets } ] ) ),
		/Invalid hunting guide location/
	);
});

test("the hunting guide window draws outdoors and is empty in a cave", () => {
	const bytes = new TextEncoder().encode(
		JSON.stringify( guideWith( [ { regionId: FIELD.regionId, x: 960, z: 960 } ] ) )
	);
	const urls = [];
	const hud = createHuntingGuideHud( {
		available: () => 2,
		request: url => {
			urls.push( url );
			return urls.length;
		},
		take: id => urls[id - 1]?.endsWith( "guide.json" ) ? { kind: "bytes", id, buffer: bytes.buffer } : null,
		cancel() {}
	}, "https://fixture.invalid" );
	const source = { url: "https://fixture.invalid/guide.json", bytes: bytes.length };
	hud.step( source, true );
	hud.step( source, true );
	try {
		// A clip larger than the whole overview pins the frame wherever the
		// center is, so only the plane test can empty the cave case.
		assert.ok( hud.present( 0, WHOLE_MAP, [ 0, 0 ], FIELD ).areas.length );
		assert.equal( hud.present( 0, WHOLE_MAP, [ 0, 0 ], CAVE_FIELD ).areas.length, 0 );
	} finally {
		hud.dispose();
	}
});

test("the world map opens a town page outdoors and the overview in a cave", () => {
	assert.equal( worldMapPageAt( TOWN ), 5 );
	assert.equal( worldMapPageAt( CAVE_TOWN ), 0 );
});

test("the world map marks an outdoor point and no cave point", () => {
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 640, 384 ];
	/** @type {readonly [number, number]} */
	const pan = [ 0, 0 ];
	const view = { page: 0, clip, pan, center: FIELD };
	const outdoor = { regionId: FIELD.regionId, x: 960, z: 960 };
	const cave = { ...outdoor, regionId: CAVE_FIELD.regionId };
	assert.equal( worldMapPointMarkers( { ...view, points: [ outdoor ] } ).length, 1 );
	assert.equal( worldMapPointMarkers( { ...view, points: [ cave ] } ).length, 0 );
});

test("the minimap asks for floors only while the player stands in a cave", () => {
	// Each target shares the player's region, so only the plane separates them.
	const target = pose => ({ regionId: pose.regionId, x: 12, y: 10, z: 50, angle: 0 });
	assert.equal( minimapFloorQueries( gameAt( FIELD ), [ target( FIELD ) ] ).length, 0 );
	assert.equal( minimapFloorQueries( gameAt( CAVE_FIELD ), [ target( CAVE_FIELD ) ] ).length, 1 );
});
