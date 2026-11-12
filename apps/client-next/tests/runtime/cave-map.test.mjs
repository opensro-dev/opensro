/*
===========================================================================

cave-map.test.mjs - a dungeon page lays out around the player

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { caveDungeonFloors, caveMapCatalog, caveMapPage, caveMapView } = await import(
	"../../src/engine/foundation/ui/cave-map.ts"
);

const CLIP = [ 100, 50, 300, 300 ];
// B2's row: 22 x 10 regions drawn 1408 x 640, so 64 pixels a region.
/** @type {import("../../src/engine/foundation/ui/cave-map.ts").CaveMapPage} */
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
	caption: "Underground Level 2 of Tomb of Qin-Shi",
	labels: [ { text: "Room of Ordeal", x: 700, y: 300, font: 2, color: [ 255, 0, 0 ] } ],
	signs: [ { name: "Military Camp 1 -> Audience Chamber", x: 710, y: 310, width: 24, height: 24 } ]
};
const CATALOG = {
	format: "sro-later-dungeon-map",
	version: 2,
	imageRoot: "/assets/images/x/",
	signIcon: "map_world_iconwf",
	monsterLevelNote: "Display 6 lv.higher monsters from the current character's level",
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

test("the tomb's six regions are one dungeon of floors, by layer", () => {
	// Ch'in Tomb B1 is region 32775 and B6 is 32770: the selector orders
	// them by their authored layer, and Donwhang's one region by floorIndex.
	const tomb = [ 32770, 32772, 32775 ].map( ( regionId, i ) => ({ ...PAGE, regionId, layer: 6 - 2 * i }) );
	const cave = [ 1, 0 ].map( floorIndex => ({ ...PAGE, regionId: 32769, floorIndex }) );
	const catalog = { ...CATALOG, maps: [ ...tomb, ...cave ] };
	assert.deepEqual( caveDungeonFloors( catalog, 32772 ).map( page => page.regionId ), [ 32775, 32772, 32770 ] );
	assert.deepEqual( caveDungeonFloors( catalog, 32769 ).map( page => page.floorIndex ), [ 0, 1 ] );
	assert.deepEqual( caveDungeonFloors( catalog, 250 ), [] );
});

test("a page is found by region and floor, and a bad catalogue is refused", () => {
	assert.equal( caveMapPage( caveMapCatalog( CATALOG ), 0x8006, 0 ), PAGE );
	assert.equal( caveMapPage( caveMapCatalog( CATALOG ), 0x8006, 1 ), undefined );
	assert.throws( () => caveMapCatalog( { ...CATALOG, version: 1 } ) );
	// A page without its title, or a catalogue without the foot note, is
	// an import from another shape: refused whole.
	assert.throws( () => caveMapCatalog( { ...CATALOG, maps: [ { ...PAGE, caption: "" } ] } ) );
	assert.throws( () => caveMapCatalog( { ...CATALOG, monsterLevelNote: undefined } ) );
});
