/*
===========================================================================

cave-map.test.mjs - a dungeon page lays out around the player

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { caveMapCatalog, caveMapPage, caveMapView, cavePageLocal } = await import(
	"../../src/engine/foundation/ui/cave-map.ts"
);

const CLIP = [ 100, 50, 300, 300 ];
// B2's row: 22 x 10 regions drawn 1408 x 640, so 64 pixels a region.
const PAGE = {
	regionId: 0x8006,
	floorIndex: 0,
	layer: 2,
	floorLabel: "QT_A01_FLOOR02",
	columns: 22,
	rows: 10,
	width: 1408,
	height: 640,
	left: 117,
	top: 132,
	image: "map_world_jinf02",
	labels: [ { text: "Room of Ordeal", x: 700, y: 300, font: 2, color: [ 255, 0, 0 ] } ],
	signs: [ { name: "Military Camp 1 -> Audience Chamber", x: 710, y: 310, width: 24, height: 24 } ]
};
const CATALOG = {
	format: "sro-later-dungeon-map",
	version: 1,
	imageRoot: "/assets/images/x/",
	signIcon: "map_world_iconwf",
	maps: [ PAGE ]
};
const POSE = { regionId: 0x8006, x: 0, y: 0, z: 0, angle: 0 };

/** @param {object} [overrides] */
function view( overrides ) {
	return caveMapView( {
		pose: POSE,
		center: POSE,
		page: PAGE,
		catalog: CATALOG,
		clip: CLIP,
		pan: [ 0, 0 ],
		local: true,
		...overrides
	} );
}

test("the player's sign sits at the centre of the view", () => {
	const sign = view().quads.at( -1 );
	assert.equal( sign?.texture.endsWith( "mm_sign_character.png" ), true );
	assert.deepEqual( sign?.rect.slice( 0, 2 ), [ CLIP[0] + 150 - 8, CLIP[1] + 150 - 8 ] );
});

test("one image holds the floor, with world zero on tile 128's corner", () => {
	const pages = view().quads.filter( quad => quad.texture.endsWith( "map_world_jinf02.png" ) );
	assert.equal( pages.length, 1 );
	// World zero is 11 columns from the left (region 128 of 117) and 5 rows
	// from the top (the bottom edge of region 128 of 132), 64 pixels each.
	assert.deepEqual( pages[0]?.rect, [ CLIP[0] + 150 - 11 * 64, CLIP[1] + 150 - 5 * 64, 1408, 640 ] );
});

test("room names and gate signs sit at their authored page positions", () => {
	const shown = view();
	assert.equal( shown.signs[0]?.rect[0] - shown.labels[0]?.x, 10 );
	assert.equal( shown.signs[0]?.rect[1] - shown.labels[0]?.y, 10 );
	assert.deepEqual( shown.labels[0]?.color, [ 1, 0, 0, 1 ] );
	assert.ok( shown.quads.some( quad => quad.texture.endsWith( "map_world_iconwf.png" ) ) );
});

test("the view stops at the page's edge and reports the clamped pan", () => {
	const { pan } = view( { pan: [ 5000, 0 ] } );
	assert.ok( pan[0] < 5000 );
	assert.deepEqual( view( { pan } ).quads.at( -1 )?.rect, view( { pan: [ 5000, 0 ] } ).quads.at( -1 )?.rect );
});

test("a page is found by region and floor, and a bad catalogue is refused", () => {
	assert.equal( caveMapPage( caveMapCatalog( CATALOG ), 0x8006, 0 ), PAGE );
	assert.equal( caveMapPage( caveMapCatalog( CATALOG ), 0x8006, 1 ), undefined );
	assert.throws( () => caveMapCatalog( { ...CATALOG, version: 2 } ) );
});

// The floors of both dungeons, as the published catalogue names them:
// Donwhang (0x8001) by floor index in one region, the Tomb (0x8007
// down to 0x8002) by layer, one region a floor.
const FLOORS = caveMapCatalog( {
	format: "sro-later-dungeon-map",
	version: 1,
	imageRoot: "/assets/images/x/",
	signIcon: "map_world_iconwf",
	maps: [
		{ ...PAGE, regionId: 0x8001, floorIndex: 0, layer: 1, floorLabel: "DH_A01_FLOOR01" },
		{ ...PAGE, regionId: 0x8001, floorIndex: 1, layer: 2, floorLabel: "DH_A01_FLOOR02" },
		{ ...PAGE, regionId: 0x8007, floorIndex: 0, layer: 1, floorLabel: "QT_A01_FLOOR01" },
		{ ...PAGE, regionId: 0x8006, floorIndex: 0, layer: 2, floorLabel: "QT_A01_FLOOR02" }
	]
} );
const DONWHANG_ONE = defined( FLOORS.maps[0] ), DONWHANG_TWO = defined( FLOORS.maps[1] );
const TOMB_ONE = defined( FLOORS.maps[2] ), TOMB_TWO = defined( FLOORS.maps[3] );
// Above ground in Jangan, beside the Donwhang cave mouth.
const JANGAN = { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 };

test("a dungeon viewed from above ground is not the player's floor (issue #3239)", () => {
	assert.equal( cavePageLocal( FLOORS, DONWHANG_ONE, JANGAN, undefined ), false );
	assert.equal( cavePageLocal( FLOORS, TOMB_ONE, JANGAN, undefined ), false );
	// And its page draws no "you are here" sign.
	const shown = view( { page: DONWHANG_ONE, pose: JANGAN, center: JANGAN, local: false } );
	assert.equal( shown.quads.some( quad => quad.texture.endsWith( "mm_sign_character.png" ) ), false );
});

test("the player's own floor is local, the dungeon's other floors are not", () => {
	const inCave = { regionId: 0x8001, x: 100, y: 0, z: 100, angle: 0 };
	// Donwhang's floors share one region: the resolved floor decides.
	assert.equal( cavePageLocal( FLOORS, DONWHANG_ONE, inCave, 0 ), true );
	assert.equal( cavePageLocal( FLOORS, DONWHANG_TWO, inCave, 0 ), false );
	assert.equal( cavePageLocal( FLOORS, DONWHANG_ONE, inCave, undefined ), false );
	// The Tomb's floors are regions of their own.
	const inTomb = { regionId: 0x8006, x: 100, y: 0, z: 100, angle: 0 };
	assert.equal( cavePageLocal( FLOORS, TOMB_TWO, inTomb, 0 ), true );
	assert.equal( cavePageLocal( FLOORS, TOMB_ONE, inTomb, 0 ), false );
	const shown = view( { local: true } );
	assert.equal( shown.quads.some( quad => quad.texture.endsWith( "mm_sign_character.png" ) ), true );
});

test("a picked species' sign draws at its anchor on the page", () => {
	const shown = view( { mark: { x: 0, z: 0 } } );
	const sign = shown.quads.find( quad => quad.texture.endsWith( "mm_sign_monster.png" ) );
	assert.ok( sign );
	// World zero projects where the player's sign centres in the
	// first test: 150 - 8 into the clip.
	assert.deepEqual( sign?.rect.slice( 0, 2 ), [ CLIP[0] + 150 - 8, CLIP[1] + 150 - 8 ] );
	assert.equal( view().quads.some( quad => quad.texture.endsWith( "mm_sign_monster.png" ) ), false );
});
