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
	guideGroups,
	guideLevelColor,
	guideMonsterRows,
	guideMonsterTarget,
	guideOverlay,
	guideRace,
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

const VIEW = { race: 0, playerLevel: 10, filterCap: undefined, open: null };

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

test("the open group lists its monsters as Lv<level> <name>, the filter hiding those above it", () => {
	const catalog = worldMapGuideCatalog( fixture() );
	const rows = guideMonsterRows( catalog, { ...VIEW, open: 0 } );
	assert.deepEqual( rows.map( row => [ row.label, row.color ] ), [
		[ "China", "group" ],
		[ "Lv1 Mangyang", "low" ],
		[ "Lv30 Weasel", "far" ],
		[ "Europe", "group" ],
		[ "Egypt", "group" ]
	] );
	assert.deepEqual(
		guideMonsterRows( catalog, { ...VIEW, open: 0, filterCap: 16 } ).map( row => row.label ),
		[ "China", "Lv1 Mangyang", "Europe", "Egypt" ]
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

// ============================================================================
// The map tools end to end
// ============================================================================

/*
================
loadedTools

Map tools whose asset owner answers the guide request with the fixture.
================
*/
function loadedTools( body = fixture() ) {
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

test("the Field tab fills from the guide and a dungeon row opens its floor's view", () => {
	const hud = loadedTools();
	hud.activate( "map-guide-tab:area", undefined, 10 );
	hud.activate( "map-guide-main:2", undefined, 10 );
	const view = paint( hud );
	assert.equal( control( view, "map-guide-area:3" ).label, "Tomb B3" );
	assert.equal( control( view, "map-guide-area:4" ).kind, "button", "the temple takes a click" );
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

test("the guide's own scrollbar: gd_scroll art, and the thumb drags the tree", () => {
	const many = fixture();
	for ( let i = 0; i < 20; i++ ) many.monsters.push( monster( "Extra" + i, [ 10110 + i, 10810 + i, 10110 + i ] ) );
	const hud = loadedTools( many );
	hud.activate( "map-guide-main:0", undefined, 10 );
	let view = paint( hud );
	for ( const art of [ "gd_scroll_line.png", "gd_scroll_up.png", "gd_scroll_down.png", "gd_scroll_button.png" ] ) {
		assert.ok( view.quads.some( quad => quad.texture.endsWith( "/guide/" + art ) ), art );
	}
	const thumb = control( view, "map-guide-scroll-thumb" );
	assert.equal( thumb.draggable, true );
	const top = thumb.rect[1];
	const first = () => paint( hud ).controls.find( row => row.id.startsWith( "map-guide-main:" ) )?.id;
	assert.equal( first(), "map-guide-main:0" );
	// 24 rows (the Lv30 Weasel is over the filter) over 8 shown: 16 rows
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

test("reverse return points are xy_returnicon buttons, the old sign without that art", async () => {
	const { reverseMapButtons } = await import( "../../src/engine/runtime/ui/hud/reverse-map-buttons.ts" );
	const points = [ {
		point: { id: 4, name: "Jangan" },
		rect: /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 100, 200, 16, 16 ])
	} ];
	const clip = /** @type {import("../../src/engine/contracts/ui").UiRect} */ ([ 0, 0, 800, 600 ]);
	const art = reverseMapButtons( { points, clip, hover: null, has: () => true } );
	assert.deepEqual( art.images[0].rect, [ 92, 192, 32, 32 ], "centred on the point" );
	assert.deepEqual( art.images[0].uv, [ 0, 0, .5, 1 ], "the strip's left half" );
	assert.ok( art.images[0].texture.endsWith( "/worldmap/map/xy_returnicon.png" ) );
	assert.deepEqual( art.hits.map( hit => [ hit.id, hit.rect ] ), [ [ "reverse-map-point:4", [ 92, 192, 32, 32 ] ] ] );
	const hovered = reverseMapButtons( { points, clip, hover: "reverse-map-point:4", has: () => true } );
	assert.ok( hovered.images[0].texture.endsWith( "xy_returnicon_focus.png" ) );
	assert.deepEqual( hovered.images[0].uv, [ 0, 0, 1, 1 ] );
	const bare = reverseMapButtons( { points, clip, hover: null, has: () => false } );
	assert.ok( bare.images[0].texture.endsWith( "wmap_sign_huntingpoint.png" ) );
	assert.deepEqual( bare.hits[0].rect, [ 100, 200, 16, 16 ] );
	assert.ok( bare.paths.some( path => path.endsWith( "xy_returnicon.png" ) ), "the art is still asked for" );
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
