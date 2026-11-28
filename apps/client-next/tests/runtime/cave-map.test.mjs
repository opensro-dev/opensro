/*
===========================================================================

cave-map.test.mjs - a dungeon page lays out around the player

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { caveDungeonFloors, caveMapCatalog, caveMapPage, caveMapView, cavePageLocal, cavePlayerSigns } = await import(
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
		local: true,
		...overrides
	} );
}

test("the player's sign sits at the centre of the view", () => {
	const sign = view().quads.at( -1 );
	assert.equal( sign?.texture.endsWith( "mm_sign_character.png" ), true );
	assert.deepEqual( sign?.rect.slice( 0, 2 ), [ CLIP[0] + 150 - 8, CLIP[1] + 150 - 8 ] );
});

test("roster signs stand on the page at their points, under the player's sign", () => {
	const shown = view( {
		roster: [ { x: 0, z: 0, path: "/assets/images/x/wmap_sign_party.png" } ]
	} );
	const sign = shown.quads.find( quad => quad.texture.endsWith( "wmap_sign_party.png" ) );
	assert.ok( sign, "the party sign is drawn" );
	// World zero is the view centre: a 16x16 sign centred on it.
	assert.deepEqual( sign?.rect, [ CLIP[0] + 150 - 8, CLIP[1] + 150 - 8, 16, 16 ] );
	// The player's sign still paints last, as 57CE80's pass order ends.
	assert.equal( shown.quads.at( -1 )?.texture.endsWith( "mm_sign_character.png" ), true );
	assert.equal( view().quads.some( quad => quad.texture.endsWith( "wmap_sign_party.png" ) ), false );
});

test("the beta roster's other players stand on the page of their own region", () => {
	const players = [
		{ gid: 1, regionId: 0x8006, x: 0, z: 0, name: "me" },
		{ gid: 2, regionId: 0x8006, x: 0, z: 0, name: "peer" },
		{ gid: 3, regionId: 0x8005, x: 0, z: 0, name: "below" },
		{ gid: 4, regionId: 0x63a6, x: 0, z: 0, name: "outside" }
	];
	const signs = cavePlayerSigns( PAGE, players, 1, new Set() );
	// Only the peer on this floor's region; the local player has their own sign.
	assert.equal( signs.length, 1 );
	const shown = view( { roster: signs } );
	const sign = shown.quads.find( quad => quad.texture.endsWith( "mm_sign_otherplayer.png" ) );
	assert.deepEqual( sign?.rect, [ CLIP[0] + 150 - 8, CLIP[1] + 150 - 8, 16, 16 ] );
	assert.equal( shown.quads.at( -1 )?.texture.endsWith( "mm_sign_character.png" ), true );
	assert.deepEqual( cavePlayerSigns( PAGE, [], 1, new Set() ), [] );
});

test("a player the party pass draws wears one sign, and an AI wears the world page's", () => {
	const players = [
		{ gid: 2, regionId: 0x8006, x: 0, z: 0, name: "partner" },
		{ gid: 3, regionId: 0x8006, x: 0, z: 0, name: "stranger" },
		{ gid: 4, regionId: 0x8006, x: 0, z: 0, name: "[AI] Trader 1" },
		{ gid: 5, regionId: 0x8006, x: 0, z: 0, name: "[AI] Thief 2" }
	];
	// The partner is in the floor's party rows, so only the party sign
	// stands for them (#4846); off their floor the beta sign is their only one.
	const signs = cavePlayerSigns( PAGE, players, 1, new Set( [ "partner" ] ) );
	assert.deepEqual( signs.map( sign => sign.path.split( "/" ).at( -1 ) ), [
		"mm_sign_otherplayer.png",
		"mm_sign_npc.png",
		"mm_sign_monster.png"
	] );
	assert.equal( cavePlayerSigns( PAGE, players, 1, new Set() ).length, 4 );
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
	// A tree built by the older importer is told how to rebuild.
	assert.throws(
		() => caveMapCatalog( { ...CATALOG, version: 1 } ),
		/version 1, this client reads 2.*SRO_VSRO_ROOT/
	);
	// A page without its title, or a catalogue without the foot note, is
	// an import from another shape: refused whole.
	assert.throws( () => caveMapCatalog( { ...CATALOG, maps: [ { ...PAGE, caption: "" } ] } ) );
	assert.throws( () => caveMapCatalog( { ...CATALOG, monsterLevelNote: undefined } ) );
});

// The floors of both dungeons, as the published catalogue names them:
// Donwhang (0x8001) by floor index in one region, the Tomb (0x8007
// down to 0x8002) by layer, one region a floor.
const FLOORS = caveMapCatalog( {
	format: "sro-later-dungeon-map",
	version: 2,
	imageRoot: "/assets/images/x/",
	signIcon: "map_world_iconwf",
	monsterLevelNote: "Display 6 lv.higher monsters from the current character's level",
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

// The picked guide monster's ground arrives in region-local units
// (worldmap-guide.ts guideDungeonGround; the guide test pins these
// numbers from the published guide catalogue). The pages below are
// the published cave catalogue's own rows.
/** @type {readonly [number, number, number, number]} */
const TINT = [ 0xe9 / 255, 0xc6 / 255, 0x24 / 255, 0x80 / 255 ];
/** @type {import("../../src/engine/foundation/ui/cave-map.ts").CaveMapPage} */
const DONWHANG_PAGE = {
	regionId: 0x8001,
	floorIndex: 0,
	layer: 1,
	floorLabel: "DH_A01_FLOOR01",
	columns: 3,
	rows: 3,
	width: 768,
	height: 768,
	left: 127,
	top: 128,
	image: "map_world_donf01",
	caption: "Donwhang Cave B1",
	labels: [],
	signs: []
};

test("a Donwhang ground tints on the cave page at the guide page's pixels (issue #3395)", () => {
	const pose = { regionId: 0x8001, x: 0, y: 0, z: 0, angle: 0 };
	const shown = caveMapView( {
		pose,
		center: pose,
		page: DONWHANG_PAGE,
		catalog: CATALOG,
		clip: [ 0, 0, 768, 768 ],
		pan: [ 0, 0 ],
		local: false,
		// The Bug Ghost's two tiles and its 432x507 page point, in the
		// floor's coordinates (a pose's units, ten to the world unit).
		ground: {
			tiles: [
				{ x: 0, z: 0, w: 1920, h: 1920, texture: "mask/a.png", color: TINT },
				{ x: 0, z: -1920, w: 1920, h: 1920, texture: "mask/b.png", color: TINT }
			],
			icon: { x: 1320, z: -1882.5, size: 32, texture: "icon.png" }
		}
	} );
	// 256 pixels a sector: the tiles stack at the page's west edge and
	// the icon stands on its point, at the v1.696 page's own pixels.
	assert.deepEqual( shown.ground.map( quad => quad.rect ), [
		[ 256, 256, 256, 256 ],
		[ 256, 512, 256, 256 ],
		[ 432, 507, 32, 32 ]
	] );
	assert.deepEqual( shown.ground[0]?.color, TINT );
	assert.deepEqual( shown.ground[2]?.color, [ 1, 1, 1, 1 ], "the icon is not tinted" );
});

test("a tomb ground tints on the 64-pixel floor (issue #3395)", () => {
	const shown = view( {
		clip: [ 0, 0, 1408, 640 ],
		// The Tomb Archer's north-west tiles and its 694x122 page point
		// on jinsi2, in the floor's coordinates.
		ground: {
			tiles: [
				{ x: -9600, z: 9600, w: 1920, h: 1920, texture: "mask/a.png", color: TINT },
				{ x: -7680, z: 9600, w: 1920, h: 1920, texture: "mask/b.png", color: TINT }
			],
			icon: { x: -300, z: 5940, size: 32, texture: "icon.png" }
		}
	} );
	assert.deepEqual( shown.ground.map( quad => quad.rect ), [
		[ 384, 0, 64, 64 ],
		[ 448, 0, 64, 64 ],
		[ 694, 122, 32, 32 ]
	] );
});

test("no picked guide monster, no ground (issue #3395)", () => {
	assert.deepEqual( view().ground, [] );
});
