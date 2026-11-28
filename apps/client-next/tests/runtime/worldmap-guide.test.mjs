/*
===========================================================================

worldmap-guide.test.mjs - the M map guide's catalogue (#3388-#3392)

The later client's trees on a fixture catalogue: the race's order with
sub rows under the last main row, sub_e6cb90's level colours, the port's
cap gate and instance badge, the targets a pick moves the view to, the
picked hunting ground's tinted tiles and icon (sub_e75d50), and the map
tools end to end: the catalogue loads, the tabs fill, a pick marks the
overlay, and the guide's own thumb drags the tree.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const {
	guideAreaRows,
	guideAreaTarget,
	guideDungeonGround,
	guideGroups,
	guideLevelColor,
	guideMonsterRows,
	guideMonsterTarget,
	guideOverlay,
	guideRace,
	levelFilterEntries,
	levelFilterOverlay,
	levelFilterTarget,
	worldMapGuideCatalog
} = await import( "../../src/engine/foundation/ui/worldmap-guide.ts" );
const { createMapToolsHud } = await import( "../../src/engine/runtime/ui/hud/map-tools.ts" );
const { DONWHANG_CAVE_DUNGEON, TOMB_FIRST_DUNGEON } = await import( "../../src/engine/foundation/ui/world-map.ts" );

/*
================
monster
================
*/
function monster( name, order, over = {} ) {
	return {
		codename: "MOB_" + name.toUpperCase(),
		name,
		level: 10,
		page: "field",
		main: false,
		order,
		world: [ 0, 0 ],
		sector: [ 166, 97 ],
		point: [ 12, 47 ],
		colours: [ 0x80000005 ],
		tier: 1,
		continent: 0,
		tiles: /** @type {unknown[]} */ ([]),
		...over
	};
}

/*
================
area
================
*/
function area( name, order, over = {} ) {
	return {
		codename: "AREA_" + name.toUpperCase(),
		textKey: "KEY",
		name,
		page: "field",
		main: false,
		order,
		linkKind: 1,
		link: 91001,
		instances: [],
		tier: 1,
		continent: 3,
		...over
	};
}

/*
================
fixture

China's group first for a Chinese player (column 0) and Europe's for a
European (column 1); Egypt shut at cap 90 (tier 101); a tomb place on a
dungeon page, a town on its page and an instance place.
================
*/
function fixture() {
	return {
		format: "opensro-worldmap-guide-v1",
		imageRoot: "/assets/images/Media_extracted/interface/",
		pages: { field: { origin: [ 46, 113 ], extent: [ 17, 26 ], tile: [ 128, 128 ], perTile: [ 4, 4 ], id: 0 } },
		areas: [
			area( "Town", [ 1000, 1000, 1000 ], { main: true } ),
			area( "Jangan", [ 1001, 1001, 1001 ], { link: 92012 } ),
			area( "Dungeon", [ 1200, 1200, 1200 ], { main: true } ),
			area( "Tomb B3", [ 1201, 1201, 1201 ], { page: "jinsi3", linkKind: 0, link: 2007, tier: 91 } ),
			area( "Temple", [ 1202, 1202, 1202 ], { page: "temple", linkKind: 0, link: 2017, tier: 101 } ),
			area( "Hall", [ 1203, 1203, 1203 ], { instances: [ "INS_A" ], tier: 101 } ),
			area( "Field China", [ 1101, 1108, 1101 ] )
		],
		monsters: [
			monster( "China", [ 10100, 10800, 10100 ], { main: true, level: null } ),
			monster( "Europe", [ 10800, 10100, 10800 ], { main: true, level: null } ),
			monster( "Mangyang", [ 10101, 10801, 10101 ], {
				level: 1,
				tiles: [ [ 166, 97, "worldmap/guide-mask/m2_166x97" ], [ 170, 97, "worldmap/guide-mask/m2_170x97" ] ]
			} ),
			monster( "Weasel", [ 10102, 10802, 10102 ], { level: 30 } ),
			monster( "Wolf", [ 10801, 10101, 10801 ], { level: 5 } ),
			monster( "Egypt", [ 10900, 10900, 10900 ], { main: true, level: null, tier: 101 } ),
			monster( "Scarab", [ 10901, 10901, 10901 ], { level: 100, tier: 101 } ),
			monster( "Cave Bat", [ 10802, 10102, 10802 ], { page: "dunhuang2", level: 6 } )
		],
		localinfo: {
			"91001": { page: 0, sector: [ 167, 98 ], point: [ 16, 2 ] },
			"92012": { page: 1, sector: [ -1, -1 ], point: [ 480, 492 ] }
		},
		art: []
	};
}

const VIEW = { race: 0, playerLevel: 10, open: null };

test("the catalogue is refused whole when its format or a row is wrong", () => {
	assert.throws( () => worldMapGuideCatalog( { ...fixture(), format: "other" } ), /format/ );
	const broken = fixture();
	broken.monsters[2].tiles = [ [ 1, 2 ] ];
	assert.throws( () => worldMapGuideCatalog( broken ), /tile/ );
	const catalog = worldMapGuideCatalog( fixture() );
	assert.equal( catalog.monsters.length, 8 );
	assert.deepEqual( catalog.localinfo["92012"], { page: 1, sector: [ -1, -1 ], point: [ 480, 492 ] } );
});

test("the race's order column groups each sub row under the main row before it", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	const names = groups =>
		groups.map( group => [ catalog.monsters[group.main].name, group.subs.map( i => catalog.monsters[i].name ) ] );
	assert.deepEqual( names( guideGroups( catalog.monsters, guideRace( 0 ) ) ), [
		[ "China", [ "Mangyang", "Weasel" ] ],
		[ "Europe", [ "Wolf", "Cave Bat" ] ],
		[ "Egypt", [ "Scarab" ] ]
	] );
	// A European player reads the second column: Europe comes first.
	assert.deepEqual( names( guideGroups( catalog.monsters, guideRace( 1 ) ) ).map( row => row[0] ), [
		"Europe",
		"China",
		"Egypt"
	] );
	assert.equal( guideRace( undefined ), 0 );
	assert.equal( guideRace( 7 ), 0 );
});

test("sub rows wear sub_e6cb90's colour for the level gap", () => {
	// P = 20: above 26 red, 24..26 light red, 20..23 white, below blue.
	assert.equal( guideLevelColor( 27, 20 ), "far" );
	assert.equal( guideLevelColor( 26, 20 ), "high" );
	assert.equal( guideLevelColor( 24, 20 ), "high" );
	assert.equal( guideLevelColor( 23, 20 ), "leaf" );
	assert.equal( guideLevelColor( 20, 20 ), "leaf" );
	assert.equal( guideLevelColor( 19, 20 ), "low" );
	assert.equal( guideLevelColor( 19, undefined ), "leaf" );
});

test("every group opens whatever its content tier: no row is shut", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	const egypt = catalog.monsters.findIndex( row => row.name === "Egypt" );
	const rows = guideMonsterRows( catalog, { ...VIEW, open: egypt } );
	assert.deepEqual( rows.map( row => row.label ), [ "China", "Europe", "Egypt", "Lv100 Scarab" ] );
	assert.deepEqual( rows.map( row => row.color ), [ "group", "group", "group", "far" ] );
});

test("the open group lists its monsters as Lv<level> <name>, the filter hiding those outside its band", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	const rows = guideMonsterRows( catalog, { ...VIEW, open: 0 } );
	assert.deepEqual( rows.map( row => [ row.label, row.color ] ), [
		[ "China", "group" ],
		[ "Lv1 Mangyang", "low" ],
		[ "Lv30 Weasel", "far" ],
		[ "Europe", "group" ],
		[ "Egypt", "group" ]
	] );
	// A level-30 character sees the same tree: only the colours follow
	// the level, the rows do not (native lists every continent).
	assert.deepEqual(
		guideMonsterRows( catalog, { ...VIEW, open: 0, playerLevel: 30 } ).map( row => row.label ),
		guideMonsterRows( catalog, { ...VIEW, open: 0 } ).map( row => row.label )
	);
});

test("the Field tree badges instance places and keeps every place open", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	const dungeon = catalog.areas.findIndex( row => row.name === "Dungeon" );
	const rows = guideAreaRows( catalog, { ...VIEW, open: dungeon } );
	// Field China (1101) hangs under Town (1000), which stays closed.
	assert.deepEqual( rows.map( row => [ row.label, row.color, !!row.badge ] ), [
		[ "Town", "group", false ],
		[ "Dungeon", "group", false ],
		[ "Tomb B3", "leaf", false ],
		[ "Temple", "leaf", false ],
		[ "Hall", "leaf", true ]
	] );
});

test("a pick's target: the icon's point, a town page point or a dungeon floor", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	// Sector 166x97 snaps to its tile's corner, x (46+120)*192, top z
	// (113+1-16)*192 = 18816; the point 12,47 is 6 world units a pixel:
	// world (31944, 18534), region 96x166 at local (72, 102) x10.
	assert.deepEqual( guideMonsterTarget( catalog, 2 ), {
		kind: "world",
		pose: { regionId: (96 << 8) | 166, x: 720, y: 0, z: 1020, angle: 0 }
	} );
	assert.equal( guideMonsterTarget( catalog, 0 ), null, "a main row has no target" );
	assert.deepEqual( guideMonsterTarget( catalog, 7 ), { kind: "dungeon", dungeon: DONWHANG_CAVE_DUNGEON, floor: 1 } );
	const named = name => catalog.areas.findIndex( row => row.name === name );
	assert.deepEqual( guideAreaTarget( catalog, named( "Jangan" ) ), { kind: "page", page: 1, point: [ 480, 492 ] } );
	assert.deepEqual( guideAreaTarget( catalog, named( "Tomb B3" ) ), {
		kind: "dungeon",
		dungeon: TOMB_FIRST_DUNGEON,
		floor: 2
	} );
	assert.equal( guideAreaTarget( catalog, named( "Temple" ) ), null, "the port has no temple page" );
	assert.equal( guideAreaTarget( catalog, named( "Field China" ) )?.kind, "world" );
});

test("the picked hunting ground: its tiles tinted 0x80e9c624, the icon's corner on its point", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	// A frame at one pixel per 6 world units with the page's corner at
	// screen 0,0 and world (31872, 18816) at screen 1000, 500.
	const frame = {
		left: 31872 - 6000,
		right: 31872 - 6000 + 6 * 4000,
		top: 18816 + 3000,
		bottom: 18816 + 3000 - 6 * 3000,
		width: 4000,
		height: 3000,
		ox: 0,
		oy: 0
	};
	const clip = /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 0, 0, 4000, 3000 ]);
	const ground = guideOverlay( catalog, 2, frame, clip );
	assert.equal( ground.quads.length, 3 );
	const [first, second, icon] = ground.quads;
	assert.equal( first.texture, "/assets/images/Media_extracted/interface/worldmap/guide-mask/m2_166x97.png" );
	assert.deepEqual( first.rect, [ 1000, 500, 128, 128 ] );
	assert.deepEqual( second.rect, [ 1128, 500, 128, 128 ] );
	assert.deepEqual( first.color, [ 0xe9 / 255, 0xc6 / 255, 0x24 / 255, 0x80 / 255 ] );
	assert.equal( icon.texture, "/assets/images/Media_extracted/interface/worldmap/wmap_monster_icon.png" );
	assert.deepEqual( ground.icon, [ 1012, 547, 32, 32 ] );
	assert.deepEqual( ground.paths, ground.quads.map( q => q.texture ), "every mask and the icon are demanded" );
	assert.deepEqual( guideOverlay( catalog, 0, frame, clip ).quads, [], "a main row draws nothing" );
	assert.deepEqual( guideOverlay( catalog, 7, frame, clip ).quads, [], "a dungeon page is not the world's" );
});

test("a dungeon pick's ground: its tiles and icon in the floor's own coordinates (issue #3395)", () => {
	// Real rows of the published catalogue (the importer on the pinned
	// v1.696 client): Donwhang floor 1's Bug Ghost and the Qin-Shi
	// tomb's first floor Tomb Soldier. Their pages' origins are the
	// published cave pages' left and top (later-dungeon-map.json), so
	// the ground below is checked against the cave catalogue as well.
	const body = fixture();
	body.pages = {
		...body.pages,
		dunhuang1: { origin: [ 127, 128 ], extent: [ 17, 26 ], tile: [ 256, 256 ], perTile: [ 1, 1 ], id: 2001 },
		jinsi1: { origin: [ 125, 132 ], extent: [ 17, 26 ], tile: [ 128, 128 ], perTile: [ 1, 1 ], id: 2005 },
		temple: { origin: [ 128, 128 ], extent: [ 17, 26 ], tile: [ 128, 128 ], perTile: [ 1, 1 ], id: 2017 }
	};
	body.monsters = [
		...body.monsters,
		monster( "Bug Ghost", [ 12101, 12101, 12101 ], {
			page: "dunhuang1",
			sector: [ -1, -1 ],
			point: [ 432, 507 ],
			tiles: [ [ 128, 127, "worldmap/guide-mask/m8_128x127" ], [ 128, 126, "worldmap/guide-mask/m8_128x126" ] ]
		} ),
		monster( "Tomb Soldier", [ 12201, 12201, 12201 ], {
			page: "jinsi1",
			sector: [ -1, -1 ],
			point: [ 381, 734 ],
			tiles: [
				[ 127, 128, "worldmap/guide-mask/m9_127x128" ],
				[ 127, 127, "worldmap/guide-mask/m9_127x127" ],
				[ 127, 126, "worldmap/guide-mask/m9_127x126" ],
				[ 128, 128, "worldmap/guide-mask/m9_128x128" ],
				[ 128, 127, "worldmap/guide-mask/m9_128x127" ],
				[ 128, 126, "worldmap/guide-mask/m9_128x126" ]
			]
		} ),
		monster( "Temple Guard", [ 12301, 12301, 12301 ], { page: "temple", sector: [ -1, -1 ], point: [ 10, 10 ] } ),
		monster( "Sector Ghost", [ 12401, 12401, 12401 ], {
			page: "dunhuang1",
			sector: [ 128, 128 ],
			point: [ 10, 20 ]
		} )
	];
	const catalog = worldMapGuideCatalog( body );
	const tintColor = [ 0xe9 / 255, 0xc6 / 255, 0x24 / 255, 0x80 / 255 ];
	// Donwhang counts sectors from 128 like the region frame does, so
	// its two tiles stand at the region's west edge, one above the
	// other, each a sector across (1920 of a pose's units).
	const donwhang = guideDungeonGround( catalog, 8 );
	assert.deepEqual( donwhang?.tiles, [
		{
			x: 0,
			z: 0,
			w: 1920,
			h: 1920,
			texture: "/assets/images/Media_extracted/interface/worldmap/guide-mask/m8_128x127.png",
			color: tintColor
		},
		{
			x: 0,
			z: -1920,
			w: 1920,
			h: 1920,
			texture: "/assets/images/Media_extracted/interface/worldmap/guide-mask/m8_128x126.png",
			color: tintColor
		}
	] );
	// The page point 432x507 is the icon's anchor: 1320 east and
	// 1882.5 south of the region origin, in a pose's units.
	assert.deepEqual( donwhang?.icon, {
		x: 1320,
		z: -1882.5,
		size: 32,
		texture: "/assets/images/Media_extracted/interface/worldmap/wmap_monster_icon.png"
	} );
	assert.deepEqual(
		donwhang?.paths,
		donwhang?.tiles.map( tile => tile.texture )
			.concat( donwhang?.icon?.texture ?? "" ),
		"every mask and the icon are demanded"
	);
	// The tomb's first floor starts two sectors west of the frame's
	// origin: its north-west tile stands at (-1920, 1920).
	const tomb = guideDungeonGround( catalog, 9 );
	assert.equal( tomb?.tiles.length, 6 );
	assert.deepEqual( tomb?.tiles[0], {
		x: -1920,
		z: 1920,
		w: 1920,
		h: 1920,
		texture: "/assets/images/Media_extracted/interface/worldmap/guide-mask/m9_127x128.png",
		color: tintColor
	} );
	assert.deepEqual( tomb?.tiles[5], {
		x: 0,
		z: -1920,
		w: 1920,
		h: 1920,
		texture: "/assets/images/Media_extracted/interface/worldmap/guide-mask/m9_128x126.png",
		color: tintColor
	} );
	assert.deepEqual( tomb?.icon, {
		x: -45,
		z: -1410,
		size: 32,
		texture: "/assets/images/Media_extracted/interface/worldmap/wmap_monster_icon.png"
	} );
	// A sector-anchored pick stands on its tile's corner plus its point:
	// tile 128x128 at 256 pixels a sector, the point 10x20 within it.
	assert.deepEqual( guideDungeonGround( catalog, 11 )?.icon, {
		x: 75,
		z: 1770,
		size: 32,
		texture: "/assets/images/Media_extracted/interface/worldmap/wmap_monster_icon.png"
	} );
	assert.equal( guideDungeonGround( catalog, 2 ), null, "a world-page monster has no cave ground" );
	assert.equal( guideDungeonGround( catalog, 0 ), null, "a main row has no ground" );
	assert.equal( guideDungeonGround( catalog, 7 ), null, "a page the build did not publish" );
	assert.equal( guideDungeonGround( catalog, 10 ), null, "the port has no temple floors" );
	assert.equal( guideDungeonGround( catalog, 99 ), null, "no such row" );
});

/*
================
worldPose

The world pose a monster row's target names; fails the test for a page,
dungeon or missing target.
================
*/
function worldPose( catalog, index ) {
	const target = guideMonsterTarget( catalog, index );
	assert.equal( target?.kind, "world" );
	return /** @type {{ kind: "world"; pose: import("../../src/engine/contracts/gameplay").Pose }} */ (target).pose;
}

test("the foot's level filter keeps world entries 0..6 above, snaps to the last and tints by the gap (v1.696 E72300)", () => {
	const body = fixture();
	// The Wolf stands apart from the Mangyang, so the snap names which.
	body.monsters[4].point = [ 40, 10 ];
	const catalog = worldMapGuideCatalog( body );
	assert.notDeepEqual( worldPose( catalog, 4 ), worldPose( catalog, 2 ) );
	// Level 1: Mangyang (1, gap 0) and Wolf (5, gap 4); the Weasel is 29
	// above, the main rows have no level and the Cave Bat is a dungeon's.
	assert.deepEqual( levelFilterEntries( catalog, 1 ), [ 2, 4 ] );
	assert.deepEqual( levelFilterEntries( catalog, 30 ), [ 3 ] );
	assert.deepEqual( levelFilterEntries( catalog, 2 ), [ 4 ], "a species below the player is not kept" );
	// E724F0 moves the view onto each kept entry: the last one is where it rests.
	assert.deepEqual( levelFilterTarget( catalog, 1 ), worldPose( catalog, 4 ) );
	assert.equal( levelFilterTarget( catalog, 200 ), null, "nothing kept, no snap" );
	const frame = { left: 0, right: 64000, top: 64000, bottom: 0, width: 640, height: 640, ox: 0, oy: 0 };
	const clip = /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 0, 0, 640, 640 ]);
	const tinted = levelFilterOverlay( catalog, 1, frame, clip );
	const masks = tinted.quads.filter( quad => quad.texture.includes( "guide-mask" ) );
	assert.equal( masks.length, 2, "Mangyang's two tiles" );
	assert.deepEqual( masks[0].color, [ 0xd9 / 255, 0xd2 / 255, 0xc6 / 255, 0x80 / 255 ], "gap 0..3 is grey" );
	assert.equal( tinted.quads.filter( quad => quad.texture.endsWith( "wmap_monster_icon.png" ) ).length, 2 );
	const red = levelFilterOverlay( catalog, -3, frame, clip ).quads.find( quad =>
		quad.texture.includes( "guide-mask" )
	);
	assert.deepEqual( red?.color, [ 0xb6 / 255, 0x31 / 255, 0x43 / 255, 0x80 / 255 ], "gap 4..6 is red" );
	assert.deepEqual( tinted.paths, tinted.quads.map( quad => quad.texture ) );
});

test("checking the foot's filter snaps the map once the guide is in, and the map starts unfiltered", () => {
	const bytes = new TextEncoder().encode( JSON.stringify( fixture() ) );
	let asked = 0;
	const hud = createMapToolsHud( {
		assets: {
			available: () => 1,
			request: url => (url.endsWith( "worldmap-guide.json" ) ? (asked += 1, 7) : 8),
			take: id => id === 7 ? { kind: /** @type {const} */ ("bytes"), id, buffer: bytes.buffer } : null,
			cancel: () => {}
		},
		base: "https://fixture.invalid/"
	} );
	try {
		hud.step( "Map" );
		assert.equal( asked, 0, "nothing loads while the filter is off and the drawer shut" );
		assert.equal( hud.takeLevelSnap( 1 ), null );
		hud.activate( "map-level-filter", undefined, 1 );
		hud.step( "Map" );
		assert.equal( asked, 1, "checking the filter loads the guide" );
		const catalog = worldMapGuideCatalog( fixture() );
		assert.deepEqual( hud.takeLevelSnap( 1 ), worldPose( catalog, 4 ) );
		assert.equal( hud.takeLevelSnap( 1 ), null, "the snap happens once per check" );
		hud.activate( "map-level-filter", undefined, 1 );
		hud.activate( "map-level-filter", undefined, 1 );
		assert.notEqual( hud.takeLevelSnap( 1 ), null, "checking it again snaps again" );
	} finally {
		hud.dispose();
	}
});

// ============================================================================
// The map tools end to end
// ============================================================================

/*
================
loadedTools

Map tools whose asset owner answers the guide request with the fixture.
================
*/
function loadedTools( body = fixture(), levelFilter = false ) {
	const bytes = new TextEncoder().encode( JSON.stringify( body ) );
	const hud = createMapToolsHud( {
		assets: {
			available: () => 1,
			request: () => 7,
			take: id => id === 7 ? { kind: /** @type {const} */ ("bytes"), id, buffer: bytes.buffer } : null,
			cancel: () => {}
		},
		base: "https://fixture.invalid/"
	} );
	hud.activate( "map-side-tab", undefined, 10 );
	// The filter starts off (E72F30); these tests read the tree whole
	// unless they ask for it (its band is pinned in world-map-tools.test.mjs).
	if ( levelFilter ) hud.activate( "map-level-filter", undefined, 10 );
	hud.step( "Map" );
	return hud;
}

const TEXT = { run: () => ({ width: 40 }), extentHeight: () => 12, quads: () => [] };

function paint( hud, over = {} ) {
	return hud.present( {
		open: true,
		cave: false,
		followed: false,
		floors: [],
		floor: undefined,
		window: [ 400, 100, 650, 422 ],
		clip: [ 408, 140, 634, 360 ],
		full: [ 0, 0, 1600, 900 ],
		catalogue: undefined,
		guidePublished: false,
		characterLevel: 10,
		race: 0,
		hover: null,
		zones: undefined,
		strings: undefined,
		levelNote: undefined,
		icons: [],
		text: TEXT,
		page: 0,
		pan: [ 0, 0 ],
		center: { regionId: 250, x: 0, y: 0, z: 0, angle: 0 },
		size: () => undefined,
		...over
	} );
}

function control( view, id ) {
	return view.controls.find( row => row.id === id );
}

test("the tools load the guide and fill the Monster tab from it", () => {
	const hud = loadedTools();
	let view = paint( hud );
	assert.deepEqual(
		view.controls.filter( row =>
			row.id.startsWith( "map-guide-" ) && !row.id.startsWith( "map-guide-tab" ) &&
			!row.id.startsWith( "map-guide-scroll" )
		).map( row => row.id ),
		[ "map-guide-main:0", "map-guide-main:1", "map-guide-main:5" ]
	);
	assert.equal( control( view, "map-guide-main:5" ).kind, "button", "Egypt opens like any group" );
	hud.activate( "map-guide-main:0", undefined, 10 );
	view = paint( hud );
	assert.equal( control( view, "map-guide-main:0" ).selected, true );
	assert.equal( control( view, "map-guide-monster:2" ).label, "Lv1 Mangyang" );
	// The pick centres on the icon's point and hands the overlay over.
	assert.deepEqual( hud.activate( "map-guide-monster:2", undefined, 10 ), {
		regionId: (96 << 8) | 166,
		x: 720,
		y: 0,
		z: 1020,
		angle: 0
	} );
	assert.equal( hud.guidePick()?.index, 2 );
	assert.equal( hud.mark(), null, "the shard's marker gives way to the overlay" );
	assert.equal( control( paint( hud ), "map-guide-monster:2" ).selected, true, "the picked row's lamp" );
	hud.dispose();
});

test("a guide monster pick holds its point for the minimap until another row replaces it (#3394)", () => {
	const body = fixture();
	// The Wolf stands apart from the Mangyang, so the move names which.
	body.monsters[4].point = [ 40, 10 ];
	const hud = loadedTools( body );
	assert.equal( hud.guideWaypoint(), null );
	hud.activate( "map-guide-main:0", undefined, 10 );
	const mangyang = hud.activate( "map-guide-monster:2", undefined, 10 );
	assert.notEqual( mangyang, null );
	assert.deepEqual( hud.guideWaypoint(), mangyang, "the pick's point is the waypoint" );
	// Another monster's pick moves it to that monster's point.
	hud.activate( "map-guide-main:1", undefined, 10 );
	const wolf = hud.activate( "map-guide-monster:4", undefined, 10 );
	assert.notDeepEqual( wolf, mangyang );
	assert.deepEqual( hud.guideWaypoint(), wolf );
	// A main row's pick clears it.
	hud.activate( "map-guide-main:1", undefined, 10 );
	assert.equal( hud.guideWaypoint(), null, "a main row clears the waypoint" );
	// A dungeon pick has no world point, so it clears it too.
	hud.activate( "map-guide-main:0", undefined, 10 );
	hud.activate( "map-guide-monster:2", undefined, 10 );
	assert.notEqual( hud.guideWaypoint(), null );
	hud.activate( "map-guide-monster:7", undefined, 10 );
	assert.equal( hud.guideWaypoint(), null, "a dungeon pick leaves no point" );
	// An area row's pick clears it as well.
	hud.activate( "map-guide-monster:2", undefined, 10 );
	assert.notEqual( hud.guideWaypoint(), null );
	hud.activate( "map-guide-tab:area", undefined, 10 );
	hud.activate( "map-guide-area:4", undefined, 10 );
	assert.equal( hud.guideWaypoint(), null, "an area row clears the waypoint" );
	hud.dispose();
});

test("the level filter does not reach the Monster tab", () => {
	const hud = loadedTools( fixture(), true );
	const mains = level =>
		paint( hud, { characterLevel: level } ).controls.filter( row => row.id.startsWith( "map-guide-main:" ) )
			.map( row => row.label );
	// Level 1 and level 30 list the same continents, Egypt's Lv100 one too.
	assert.deepEqual( mains( 1 ), [ "China", "Europe", "Egypt" ] );
	assert.deepEqual( mains( 30 ), mains( 1 ) );
	hud.activate( "map-guide-main:0", undefined, 30 );
	const subs = paint( hud, { characterLevel: 30 } ).controls.filter( row =>
		row.id.startsWith( "map-guide-monster:" )
	)
		.map( row => row.label );
	assert.ok( subs.includes( "Lv1 Mangyang" ) && subs.includes( "Lv30 Weasel" ), subs.join() );
	hud.dispose();
});

test("the Field tab fills from the guide and a dungeon row opens its floor's view", () => {
	const hud = loadedTools();
	hud.activate( "map-guide-tab:area", undefined, 10 );
	hud.activate( "map-guide-main:2", undefined, 10 );
	const view = paint( hud );
	assert.equal( control( view, "map-guide-area:3" ).label, "Tomb B3" );
	assert.equal( control( view, "map-guide-area:4" ).kind, "button", "the temple takes a click" );
	// A picked place lights its lamp, as a picked monster does.
	assert.equal( control( view, "map-guide-area:4" ).selected, false );
	hud.activate( "map-guide-area:4", undefined, 10 );
	assert.equal( control( paint( hud ), "map-guide-area:4" ).selected, true, "the picked place's lamp" );
	assert.equal( control( paint( hud ), "map-guide-area:3" ).selected, false );
	assert.equal( hud.activate( "map-guide-area:3", undefined, 10 ), null );
	assert.equal( hud.viewingDungeon(), true );
	hud.dispose();
});

test("a build without the guide keeps the shard's Monster tab and says so on the Field tab", () => {
	const hud = createMapToolsHud( {
		assets: {
			available: () => 1,
			request: () => 7,
			take: () => ({ kind: /** @type {const} */ ("error"), id: 7, error: "404" }),
			cancel: () => {}
		},
		base: "https://fixture.invalid/"
	} );
	hud.activate( "map-side-tab", undefined, 10 );
	const warn = console.warn;
	console.warn = () => {};
	try {
		assert.equal( hud.step( "Map" ), true );
	} finally {
		console.warn = warn;
	}
	assert.equal( hud.guidePick(), null );
	hud.activate( "map-guide-tab:area", undefined, 10 );
	const texts = [];
	paint( hud, { text: { ...TEXT, quads: value => (texts.push( value ), []) } } );
	assert.ok( texts.includes( "No field guide in this build" ) );
	hud.dispose();
});

test("the guide's stock scrollbar: chat-window art, and the thumb drags the tree", () => {
	const many = fixture();
	for ( let i = 0; i < 19; i++ ) many.monsters.push( monster( "Extra" + i, [ 10110 + i, 10810 + i, 10110 + i ] ) );
	const hud = loadedTools( many );
	hud.activate( "map-guide-main:0", undefined, 10 );
	let view = paint( hud );
	for (
		const art of [
			"ifcommon/com_scroll_bar.png",
			"chattingwnd/chat_arrow_up.png",
			"chattingwnd/chat_arrow_down.png",
			"ifcommon/com_scroll_button.png"
		]
	) {
		assert.ok( view.quads.some( quad => quad.texture.endsWith( "/interface/" + art ) ), art );
	}
	assert.equal( view.quads.some( quad => quad.texture.includes( "gd_scroll" ) ), false, "no bone-knot art" );
	const thumb = control( view, "map-guide-scroll-thumb" );
	assert.equal( thumb.draggable, true );
	const top = thumb.rect[1];
	const first = () => paint( hud ).controls.find( row => row.id.startsWith( "map-guide-main:" ) )?.id;
	assert.equal( first(), "map-guide-main:0" );
	// 24 rows (five groups and species, 19 extras) over 8 shown: 16 rows
	// of range on a 146 px travel, 9.125 px a row. A drag past the end
	// stops there and owes nothing back.
	assert.equal( hud.drag( "map-guide-scroll-thumb", 400 ), true );
	view = paint( hud );
	assert.equal( control( view, "map-guide-scroll-thumb" ).rect[1], top + 146, "the thumb reached the end" );
	assert.equal( view.controls.some( row => row.id === "map-guide-main:0" ), false, "the tree scrolled" );
	// A slow drag spends its pixels: 5 px is under a row, 5 more pass one.
	hud.drag( "map-guide-scroll-thumb", -5 );
	assert.equal( control( paint( hud ), "map-guide-scroll-thumb" ).rect[1], top + 146 );
	hud.drag( "map-guide-scroll-thumb", -5 );
	assert.equal( control( paint( hud ), "map-guide-scroll-thumb" ).rect[1], top + Math.round( 15 * 146 / 16 ) );
	assert.equal( hud.drag( "map-pan", 10 ), false, "other drags are not the guide's" );
	hud.dispose();
});

test("the wheel over the guide steps one row a notch, and leaves the map's wheel alone (#3515)", () => {
	const many = fixture();
	for ( let i = 0; i < 19; i++ ) many.monsters.push( monster( "Extra" + i, [ 10110 + i, 10810 + i, 10110 + i ] ) );
	const hud = loadedTools( many );
	hud.activate( "map-guide-main:0", undefined, 10 );
	const thumb = () => control( paint( hud ), "map-guide-scroll-thumb" ).rect[1];
	const top = thumb();
	// The panel stands left of the window: [102, 100, 298, 296] here.
	assert.equal( hud.scroll( 200, 200, 120 ), true );
	assert.equal( thumb(), top + Math.round( 146 / 16 ), "one row, however large the delta" );
	assert.equal( hud.scroll( 200, 200, -3 ), true );
	assert.equal( thumb(), top, "and back" );
	assert.equal( hud.scroll( 200, 200, -120 ), true );
	assert.equal( thumb(), top, "held at the first row" );
	for ( let i = 0; i < 30; i++ ) hud.scroll( 200, 200, 1 );
	assert.equal( thumb(), top + 146, "held at the last row" );
	assert.equal( hud.scroll( 700, 300, 1 ), false, "the map's own area is not the guide's" );
	hud.activate( "map-side-tab", undefined, 10 );
	paint( hud );
	assert.equal( hud.scroll( 200, 200, 1 ), false, "a shut guide takes no wheel" );
	hud.dispose();
});

test("reverse return points are xy_returnicon buttons, the old sign without that art", async () => {
	const { reverseMapButtons } = await import( "../../src/engine/runtime/ui/hud/reverse-map-buttons.ts" );
	const points = [ {
		point: { id: 4, name: "Jangan" },
		rect: /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 100, 200, 16, 16 ])
	} ];
	const clip = /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 0, 0, 800, 600 ]);
	const art = reverseMapButtons( { points, clip, screen: clip, hover: null, has: () => true } );
	assert.deepEqual( art.images[0].rect, [ 92, 192, 32, 32 ], "centred on the point" );
	assert.deepEqual( art.images[0].uv, [ 0, 0, .5, 1 ], "the strip's left half" );
	assert.ok( art.images[0].texture.endsWith( "/worldmap/map/xy_returnicon.png" ) );
	assert.deepEqual( art.hits.map( hit => [ hit.id, hit.rect ] ), [ [ "reverse-map-point:4", [ 92, 192, 32, 32 ] ] ] );
	const hovered = reverseMapButtons( { points, clip, screen: clip, hover: "reverse-map-point:4", has: () => true } );
	assert.ok( hovered.images[0].texture.endsWith( "xy_returnicon_focus.png" ) );
	assert.deepEqual( hovered.images[0].uv, [ 0, 0, 1, 1 ] );
	const bare = reverseMapButtons( { points, clip, screen: clip, hover: null, has: () => false } );
	assert.ok( bare.images[0].texture.endsWith( "wmap_sign_huntingpoint.png" ) );
	assert.deepEqual( bare.hits[0].rect, [ 100, 200, 16, 16 ] );
	assert.ok( bare.paths.some( path => path.endsWith( "xy_returnicon.png" ) ), "the art is still asked for" );
});

test("an open dungeon flag's menu hangs its floors below the icon, 155 x 22 each", async () => {
	const { reverseMapButtons } = await import( "../../src/engine/runtime/ui/hud/reverse-map-buttons.ts" );
	const rect = /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 100, 200, 16, 16 ]);
	const clip = /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 0, 0, 800, 600 ]);
	const points = [ { point: { id: 1, name: "Jangan" }, rect }, {
		point: { id: 5, name: "Tomb-King Shi", group: 2 },
		rect: /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 300, 200, 16, 16 ])
	} ];
	const floors = [ { id: 6, name: "Tomb of Qin-Shi Emperor lv.2" }, { id: 7, name: "Tomb of Qin-Shi Emperor lv.3" } ];
	const shut = reverseMapButtons( { points, clip, screen: clip, hover: null, has: () => true } );
	assert.deepEqual( shut.menu.labels, [], "no menu until the flag is clicked" );
	const open = reverseMapButtons( {
		points,
		menu: { group: 2, floors },
		clip,
		screen: clip,
		hover: "reverse-map-floor:7",
		has: () => true
	} );
	// The flag's icon is [292, 192, 32, 32]: the menu starts 4 px below its
	// bottom-left corner, its first entry 1 px into it (v1.188 0x6973a5, 0x631d60).
	assert.deepEqual( open.menu.hits.map( hit => [ hit.id, hit.rect ] ), [
		[ "reverse-map-floor:6", [ 292, 229, 155, 22 ] ],
		[ "reverse-map-floor:7", [ 292, 251, 155, 22 ] ]
	] );
	assert.deepEqual( open.menu.labels.map( label => label.value ), floors.map( floor => floor.name ) );
	assert.ok( open.menu.images[0].texture.endsWith( "/worldmap/map/map_return.png" ) );
	assert.ok(
		open.menu.images[1].texture.endsWith( "/worldmap/map/map_return_focus.png" ),
		"the hovered entry's focus face"
	);
	const bare = reverseMapButtons( {
		points,
		menu: { group: 2, floors },
		clip,
		screen: clip,
		hover: null,
		has: () => false
	} );
	assert.equal( bare.menu.images[0].texture, "", "a plain plate without the later client's art" );
	assert.deepEqual( bare.menu.hits[0].rect, [ 300, 221, 155, 22 ], "below the old sign's own rect" );
	const elsewhere = reverseMapButtons( {
		points: points.slice( 0, 1 ),
		menu: { group: 2, floors },
		clip,
		screen: clip,
		hover: null,
		has: () => true
	} );
	assert.deepEqual( elsewhere.menu.labels, [], "a flag off the page shows no menu" );
});

test("a floor menu past the map's edge is seen whole and answers only where it is seen", async () => {
	const { reverseMapButtons } = await import( "../../src/engine/runtime/ui/hud/reverse-map-buttons.ts" );
	const ui = /** @type {(rect: [number, number, number, number]) => import("../../src/engine/contracts/ui").UiRect} */ (
		rect => rect
	);
	// The flag sits near the map pane's bottom edge (the pane ends at y 260),
	// with a second point half outside its right edge (it ends at x 400).
	const points = [
		{ point: { id: 5, name: "Tomb-King Shi", group: 2 }, rect: ui( [ 300, 200, 16, 16 ] ) },
		{ point: { id: 9, name: "Edge" }, rect: ui( [ 392, 100, 16, 16 ] ) }
	];
	const floors = [ { id: 6, name: "B1" }, { id: 7, name: "B2" }, { id: 8, name: "B3" } ];
	const open = reverseMapButtons( {
		points,
		menu: { group: 2, floors },
		clip: ui( [ 0, 0, 400, 260 ] ),
		screen: ui( [ 0, 0, 800, 600 ] ),
		hover: null,
		has: () => true
	} );
	// The v1.188 popup is its own window: every entry is drawn on the
	// screen's clip, not the pane's, and each one answers where it stands.
	assert.deepEqual( open.menu.hits.map( hit => hit.rect[1] ), [ 229, 251, 273 ] );
	assert.ok( open.menu.images.every( quad => quad.clip?.[3] === 600 ), "drawn past the pane" );
	// The half-hidden icon answers only over its visible half.
	assert.deepEqual( open.hits.find( hit => hit.id === "reverse-map-point:9" )?.rect, [ 384, 92, 16, 32 ] );
	const gone = reverseMapButtons( {
		points: [ { point: { id: 9, name: "Edge" }, rect: ui( [ 440, 100, 16, 16 ] ) } ],
		clip: ui( [ 0, 0, 400, 260 ] ),
		screen: ui( [ 0, 0, 800, 600 ] ),
		hover: null,
		has: () => true
	} );
	assert.deepEqual( gone.hits, [], "an icon wholly outside the pane takes no click" );
});

test("a long row name ends in an ellipsis with the whole name on hover, and gives up the badge first", () => {
	const hud = loadedTools();
	hud.activate( "map-guide-tab:area", undefined, 10 );
	hud.activate( "map-guide-main:2", undefined, 10 );
	const drawn = [];
	const wide = {
		...TEXT,
		run: value => ({ width: value === "Hall" ? 200 : 40 }),
		quads: ( value, rect, clip, color, style ) => (drawn.push( { value, rect, style } ), [])
	};
	const view = paint( hud, { text: wide } );
	const hall = drawn.find( row => row.value === "Hall" );
	assert.ok( hall );
	assert.equal( hall.rect[2], 220, "the badge's room goes to the name" );
	assert.equal( hall.style.overflow, "clip", "it fits without the badge" );
	assert.equal( drawn.some( row => row.value === "Inst." ), false );
	assert.equal( control( view, "map-guide-area:5" ).helpText, undefined );
	const longer = paint( hud, { text: { ...wide, run: value => ({ width: value === "Hall" ? 300 : 40 }) } } );
	assert.equal( control( longer, "map-guide-area:5" ).helpText, "Hall", "the whole name on hover" );
	hud.dispose();
});
