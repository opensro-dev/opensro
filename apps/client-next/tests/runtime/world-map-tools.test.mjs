/*
===========================================================================

world-map-tools.test.mjs - the world map's own tools (issue #3167)

Pins the monster directory and the level filter at the foundation, the
tools' own state (guide tree, species pick, floor choice, dungeon view)
and the v1.188 chrome's places (worldmapguide.2dt, matched against the
v1.188 screenshots) against a stub text sink, and the whole window end
to end in the UI fixture: the filter is on by default, the side tab
opens the guide, a group opens to its species, a pick marks the
species, the dungeon entrances open their maps, and a dungeon starts on
the player's floor.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { MONSTER_LEVEL_FILTER_SPAN, decodeHuntingGuide, dungeonDirectory, filterHuntingGuide, monsterDirectory } =
	await import( "../../src/engine/foundation/ui/hunting-guide.ts" );
const { caveDungeonFloors, caveMapCatalog, cavePageCenter } = await import(
	"../../src/engine/foundation/ui/cave-map.ts"
);
const { createMapToolsHud } = await import( "../../src/engine/runtime/ui/hud/map-tools.ts" );
const { worldMapDungeonPose } = await import( "../../src/engine/foundation/ui/world-map.ts" );

/*
================
monster
================
*/
function monster( refObjId, name, level ) {
	return { refObjId, name, nameKey: "SN_FIXTURE_" + refObjId, level };
}

const TIGER = monster( 101, "Tiger", 5 ), BANDIT = monster( 102, "Bandit", 25 ), GIANT = monster( 103, "Giant", 45 );

/*
================
catalogue

Region 100 (sectors are keyed rz*256+rx, anchors in world units) holds
all three species, the Tiger twice; region 200 holds only the Giant;
region 50 the Tiger and the Bandit.
================
*/
function catalogue() {
	// Dungeon anchors in region-local coordinates (issue #3239):
	// Donwhang's cave (32769) and every Tomb floor region.
	const dungeons = new Map( [
		[ 32769, [
			{ monster: BANDIT, x: 600, z: 600 },
			{ monster: TIGER, x: 500, z: 700 }
		] ]
	] );
	for ( const region of [ 32775, 32774, 32773, 32772, 32771, 32770 ] ) {
		dungeons.set( region, [ { monster: GIANT, x: 300, z: 300 } ] );
	}
	return {
		monsters: [ TIGER, BANDIT, GIANT ],
		regions: new Map( [
			[ 100, [
				{ monster: GIANT, x: 19300, z: 40 },
				{ monster: TIGER, x: 19250, z: 30 },
				{ monster: TIGER, x: 19260, z: 35 },
				{ monster: BANDIT, x: 19270, z: 45 }
			] ],
			[ 200, [ { monster: GIANT, x: 38500, z: 60 } ] ],
			[ 50, [
				{ monster: BANDIT, x: 9700, z: 20 },
				{ monster: TIGER, x: 9650, z: 25 }
			] ]
		] ),
		dungeons,
		sections: []
	};
}

test("the directory groups species by region at their first anchor, sorted by level", () => {
	const regions = monsterDirectory( catalogue() );
	assert.deepEqual( regions.map( row => row.region ), [ 50, 100, 200 ] );
	assert.deepEqual( regions[0]?.monsters.map( row => row.monster.refObjId ), [ 101, 102 ] );
	assert.equal( regions[0]?.minLevel, 5 );
	assert.equal( regions[0]?.maxLevel, 25 );
	const home = defined( regions[1] );
	assert.deepEqual( home.monsters.map( row => row.monster.refObjId ), [ 101, 102, 103 ] );
	assert.equal( home.minLevel, 5 );
	assert.equal( home.maxLevel, 45 );
	// The Tiger's row in region 100 is its first anchor there, not the second.
	assert.equal( home.monsters[0]?.x, 19250 );
	assert.equal( home.monsters[0]?.z, 30 );
});

test("the level filter's cap hides species above it and the regions it empties", () => {
	const capped = monsterDirectory( catalogue(), 30 );
	assert.deepEqual( capped.map( row => row.region ), [ 50, 100 ] );
	assert.deepEqual( defined( capped[1] ).monsters.map( row => row.monster.refObjId ), [ 101, 102 ] );
	assert.equal( defined( capped[1] ).maxLevel, 25 );
	assert.deepEqual( monsterDirectory( catalogue(), 4 ), [] );
});

test("every dungeon floor fills from the catalogue's dungeon anchors (issue #3239)", () => {
	const cat = catalogue();
	// All ten pages: Donwhang's four share its one region, the Tomb's
	// six are a region each; every floor's list is non-empty.
	for ( const floor of CAVE.maps ) {
		const row = dungeonDirectory( cat, floor.regionId );
		assert.ok( row && row.monsters.length > 0, "floor " + floor.floorLabel + " lists no species" );
	}
	// Sorted by level, at the first anchor, and capped like the world list.
	assert.deepEqual(
		dungeonDirectory( cat, 32769 )?.monsters.map( row => row.monster.refObjId ),
		[ 101, 102 ]
	);
	assert.deepEqual(
		dungeonDirectory( cat, 32769, 10 )?.monsters.map( row => row.monster.refObjId ),
		[ 101 ]
	);
	assert.equal( dungeonDirectory( cat, 32769, 4 ), undefined );
	assert.equal( dungeonDirectory( cat, 12345 ), undefined );
});

test("the decoded catalogue keeps dungeon anchors apart from the outdoor regions", () => {
	const cat = decodeHuntingGuide( {
		format: "sro-hunting-guide",
		version: 1,
		rows: [ {
			refObjId: 101,
			nameKey: "SN_FIXTURE_TIGER",
			name: "Fixture Tiger",
			level: 5,
			points: [ { regionId: REGION_A, x: 500, z: 700 } ],
			dungeonPoints: [ { regionId: 32769, x: 500, z: 700 } ]
		} ]
	} );
	assert.deepEqual( [ ...cat.dungeons.keys() ], [ 32769 ] );
	assert.equal( cat.dungeons.get( 32769 )?.[0]?.monster.refObjId, 101 );
	// The dungeon anchor never becomes an outdoor region or section.
	assert.equal( [ ...cat.regions.keys() ].includes( 32769 ), false );
	// A dungeon point in an outdoor region is refused whole.
	assert.throws( () =>
		decodeHuntingGuide( {
			format: "sro-hunting-guide",
			version: 1,
			rows: [ {
				refObjId: 101,
				nameKey: "SN_FIXTURE_TIGER",
				name: "Fixture Tiger",
				level: 5,
				points: [],
				dungeonPoints: [ { regionId: REGION_A, x: 1, z: 1 } ]
			} ]
		} )
	);
});

test("filterHuntingGuide drops the species, their anchors and their sections together", () => {
	const filtered = filterHuntingGuide( catalogue(), 30 );
	assert.deepEqual( filtered.monsters.map( row => row.refObjId ), [ 101, 102 ] );
	assert.deepEqual( [ ...filtered.regions.keys() ].sort( ( a, b ) => a - b ), [ 50, 100 ] );
	assert.equal( defined( filtered.regions.get( 100 ) ).length, 3 );
	const sectioned = new Set( filtered.sections.flatMap( section => section.monsters.map( row => row.refObjId ) ) );
	assert.deepEqual( [ ...sectioned ].sort( ( a, b ) => a - b ), [ 101, 102 ] );
});

/*
================
page
================
*/
function page( regionId, floorIndex, layer, floorLabel ) {
	return {
		regionId,
		floorIndex,
		layer,
		floorLabel,
		caption: "Fixture " + floorLabel,
		columns: 4,
		rows: 2,
		width: 512,
		height: 256,
		left: 130,
		top: 120,
		image: "fixture",
		labels: [],
		signs: []
	};
}

const CAVE = caveMapCatalog( {
	format: "sro-later-dungeon-map",
	version: 2,
	imageRoot: "/x/",
	signIcon: "icon",
	monsterLevelNote: "Fixture note",
	maps: [
		page( 32769, 2, 3, "DH_A01_FLOOR03" ),
		page( 32769, 0, 1, "DH_A01_FLOOR01" ),
		page( 32769, 3, 4, "DH_A01_FLOOR04" ),
		page( 32769, 1, 2, "DH_A01_FLOOR02" ),
		page( 32770, 0, 6, "QT_A01_FLOOR06" ),
		page( 32772, 0, 4, "QT_A01_FLOOR04" ),
		page( 32775, 0, 1, "QT_A01_FLOOR01" ),
		page( 32771, 0, 5, "QT_A01_FLOOR05" ),
		page( 32774, 0, 2, "QT_A01_FLOOR02" ),
		page( 32773, 0, 3, "QT_A01_FLOOR03" )
	]
} );

test("a dungeon's floors come in floor order, Donwhang by index and the Tomb by layer", () => {
	assert.deepEqual( caveDungeonFloors( CAVE, 32769 ).map( row => row.floorIndex ), [ 0, 1, 2, 3 ] );
	assert.deepEqual(
		caveDungeonFloors( CAVE, 32773 ).map( row => row.regionId ),
		[ 32775, 32774, 32773, 32772, 32771, 32770 ]
	);
	assert.deepEqual( caveDungeonFloors( CAVE, 12345 ), [] );
	assert.deepEqual( caveDungeonFloors( undefined, 32769 ), [] );
});

test("a page's centre is the pose its own projection centres on", () => {
	// mapX/mapY in caveMapView put this pose at (width/2, height/2).
	assert.deepEqual( cavePageCenter( page( 32769, 0, 1, "DH_A01_FLOOR01" ) ), {
		regionId: 32769,
		x: 7680,
		y: 0,
		z: -15360,
		angle: 0
	} );
});

/*
================
tools
================
*/
function tools() {
	return createMapToolsHud( {
		assets: { available: () => 0, request: () => 1, take: () => null, cancel: () => {} },
		base: "https://fixture.invalid/"
	} );
}

const TEXT = { run: () => ({ width: 40 }), extentHeight: () => 12, quads: () => [] };

/*
================
paint
================
*/
function paint( hud, over = {} ) {
	return hud.present( {
		open: true,
		cave: false,
		followed: false,
		floors: [],
		floor: undefined,
		window: [ 100, 100, 800, 600 ],
		clip: [ 108, 140, 784, 500 ],
		full: [ 0, 0, 1600, 900 ],
		catalogue: catalogue(),
		guidePublished: true,
		characterLevel: 20,
		zones: { "50": "Fixture Field" },
		strings: { SN_ZONE_23001: "Fixture Tomb", SN_ZONE_23002: "Fixture Cave" },
		levelNote: "Fixture note",
		icons: [],
		text: TEXT,
		page: 0,
		pan: [ 0, 0 ],
		center: { regionId: 250, x: 0, y: 0, z: 0, angle: 0 },
		size: () => undefined,
		...over
	} );
}

/*
================
ids
================
*/
function ids( view ) {
	return view.controls.map( row => row.id );
}

test("the level filter starts on and its toggle lifts the cap", () => {
	const hud = tools();
	assert.equal( hud.filterCap( 20 ), 20 + MONSTER_LEVEL_FILTER_SPAN );
	assert.equal( hud.filterCap( undefined ), undefined );
	assert.equal( hud.activate( "map-level-filter", catalogue(), 20 ), null );
	assert.equal( hud.filterCap( 20 ), undefined );
	assert.equal( hud.activate( "map-level-filter", catalogue(), 20 ), null );
	assert.equal( hud.filterCap( 20 ), 20 + MONSTER_LEVEL_FILTER_SPAN );
	assert.equal( hud.activate( "map-size", catalogue(), 20 ), undefined, "foreign ids are not the tools'" );
	hud.dispose();
});

test("the foot's checkbox is the one level filter switch, at its layout place", () => {
	const hud = tools();
	// Window 100,100 800x600: the checkbox (614,426 in the 650x422
	// layout) keeps its distance from the right edge and the bottom.
	const view = paint( hud );
	const switches = view.controls.filter( row => row.id === "map-level-filter" );
	assert.equal( switches.length, 1 );
	assert.deepEqual( switches[0].rect, [ 864, 704, 16, 16 ] );
	assert.equal( switches[0].selected, true );
	assert.equal( hud.activate( "map-level-filter", catalogue(), 20 ), null );
	assert.equal( hud.filterCap( 20 ), undefined );
	assert.equal( control( paint( hud ), "map-level-filter" )?.selected, false );
	assert.ok( paint( hud ).quads.some( quad => quad.texture.endsWith( "com_checkbutton_off.png" ) ) );
	hud.dispose();
});

test("a dungeon page has one level filter switch, its foot checkbox", () => {
	// Two painters once drew map-level-filter on a dungeon page, and the
	// UI refused the frame ("Duplicate UI control identity").
	const hud = tools();
	const floors = caveDungeonFloors( CAVE, 32769 );
	hud.activate( "map-side-tab", catalogue(), 20 );
	const view = paint( hud, { cave: true, floors, floor: floors[0] } );
	assert.equal( view.controls.filter( row => row.id === "map-level-filter" ).length, 1 );
	assert.equal( new Set( ids( view ) ).size, view.controls.length, "every control identity is unique" );
	hud.dispose();
});

test("the guide tree opens a group in place and a pick returns the anchor pose", () => {
	const hud = tools();
	const cat = catalogue();
	assert.equal( ids( paint( hud ) ).includes( "map-region:50" ), false, "the guide starts shut" );
	hud.activate( "map-side-tab", cat, 20 );
	let view = paint( hud );
	defined( control( view, "map-guide" ) );
	// At level 20 the cap is 26: the Giant's region 200 is hidden whole.
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-region:" ) ),
		[ "map-region:50", "map-region:100" ]
	);
	assert.equal( control( view, "map-region:50" )?.label?.startsWith( "Fixture Field  Lv." ), true );
	// The first row stands at 14,37 of the guide panel (here at the
	// screen's left edge: the window at 100 leaves no room beside it).
	assert.deepEqual( control( view, "map-region:50" )?.rect, [ 14, 137, 248, 28 ] );
	hud.activate( "map-region:100", cat, 20 );
	view = paint( hud );
	assert.equal( control( view, "map-region:100" )?.selected, true, "the group stands open" );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-region:" ) || id.startsWith( "map-monster:" ) ),
		[ "map-region:50", "map-region:100", "map-monster:100:101", "map-monster:100:102" ],
		"the open group's species follow it"
	);
	assert.equal( control( view, "map-monster:100:102" )?.label, "Lv25 Bandit" );
	// The Tiger's first anchor in region 100 is world (19250, 30): the
	// sector is (100, 0), so the pose is local x10.
	assert.deepEqual( hud.activate( "map-monster:100:101", cat, 20 ), {
		regionId: 100,
		x: 500,
		y: 0,
		z: 300,
		angle: 0
	} );
	assert.deepEqual( hud.mark(), { regionId: 100, x: 500, z: 300, refObjId: 101 } );
	view = paint( hud );
	assert.equal( control( view, "map-monster:100:101" )?.selected, true, "the picked species' lamp" );
	assert.ok( view.quads.some( quad => quad.texture.endsWith( "gd_contents_lamp.png" ) ) );
	// The same species picked from region 50's group centres there instead.
	hud.activate( "map-region:50", cat, 20 );
	assert.equal( ids( paint( hud ) ).includes( "map-monster:100:101" ), false, "one group open at a time" );
	assert.deepEqual( hud.activate( "map-monster:50:101", cat, 20 ), {
		regionId: 50,
		x: 500,
		y: 0,
		z: 250,
		angle: 0
	} );
	// Lifting the filter brings the Giant and its region back.
	hud.activate( "map-region:100", cat, 20 );
	hud.activate( "map-level-filter", cat, 20 );
	view = paint( hud );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-monster:" ) ),
		[ "map-monster:100:101", "map-monster:100:102", "map-monster:100:103" ]
	);
	assert.equal( ids( view ).includes( "map-region:200" ), true );
	// The toggle shuts the group again.
	hud.activate( "map-region:100", cat, 20 );
	assert.equal( ids( paint( hud ) ).some( id => id.startsWith( "map-monster:" ) ), false );
	hud.dispose();
});

test("the guide panel: its frame, the Field and Monster tabs and the tree's scrollbar", () => {
	const hud = tools();
	const cat = catalogue();
	hud.activate( "map-side-tab", cat, 20 );
	// A screen reaching left of the window: the panel stands beside it,
	// 298 wide, and the side tab moves to its left edge, pointing back.
	let view = paint( hud, { full: [ -400, 0, 2000, 900 ] } );
	assert.deepEqual( control( view, "map-guide" )?.rect, [ -198, 100, 298, 296 ] );
	assert.deepEqual( control( view, "map-side-tab" )?.rect, [ -198 - 15, 196, 12, 96 ] );
	assert.ok( view.quads.some( quad => quad.texture.endsWith( "com_side_button.png" ) ) );
	assert.deepEqual( control( view, "map-guide-tab:area" )?.rect, [ -178, 108, 56, 24 ] );
	assert.deepEqual( control( view, "map-guide-tab:monster" )?.rect, [ -118, 108, 56, 24 ] );
	assert.equal( control( view, "map-guide-tab:monster" )?.selected, true, "the Monster tab opens first" );
	assert.deepEqual( control( view, "map-guide-scroll-up" )?.rect, [ 66, 156, 16, 16 ] );
	assert.deepEqual( control( view, "map-guide-scroll-down" )?.rect, [ 66, 334, 16, 16 ] );
	// Port-only: a screen too narrow for both stops the panel at its edge.
	view = paint( hud );
	assert.deepEqual( control( view, "map-guide" )?.rect, [ 0, 100, 298, 296 ] );
	assert.deepEqual( control( view, "map-side-tab" )?.rect, [ -15, 196, 12, 96 ] );
	// The arrows step one row and hold at either end; six rows (three
	// groups, three species) fit the eight, so both ends are row 0.
	hud.activate( "map-level-filter", cat, 20 );
	hud.activate( "map-region:100", cat, 20 );
	const first = () =>
		paint( hud ).controls.find( row => row.id.startsWith( "map-region:" ) || row.id.startsWith( "map-monster:" ) )
			?.id;
	assert.equal( first(), "map-region:50" );
	hud.activate( "map-guide-scroll-up", cat, 20 );
	assert.equal( first(), "map-region:50", "the top end holds" );
	hud.activate( "map-guide-scroll-down", cat, 20 );
	assert.equal( first(), "map-region:50", "the bottom end holds" );
	// The Field tab stands with its own (empty) tree.
	hud.activate( "map-guide-tab:area", cat, 20 );
	view = paint( hud );
	assert.equal( control( view, "map-guide-tab:area" )?.selected, true );
	assert.equal( ids( view ).some( id => id.startsWith( "map-region:" ) ), false );
	// The side tab shuts it all.
	hud.activate( "map-side-tab", cat, 20 );
	view = paint( hud );
	assert.equal( control( view, "map-guide" ), undefined );
	assert.deepEqual( control( view, "map-side-tab" )?.rect, [ 85, 196, 12, 96 ] );
	hud.dispose();
});

test("floor picks hold per dungeon and a viewed dungeon opens on its first floor", () => {
	const hud = tools();
	const indoors = { regionId: 32769, x: 0, y: 0, z: 0, angle: 0 };
	assert.equal( hud.cavePage( CAVE, indoors ), undefined, "no pick yet: the map's own floor stands" );
	hud.activate( "map-floor:32769:2", undefined, undefined, true );
	assert.equal( hud.cavePage( CAVE, indoors )?.page.floorIndex, 2 );
	// Walking into the other dungeon forgets the pick.
	const tomb = { regionId: 32773, x: 0, y: 0, z: 0, angle: 0 };
	assert.equal( hud.cavePage( CAVE, tomb ), undefined );
	hud.activate( "map-floor:32770:0", undefined, undefined, true );
	assert.equal( hud.cavePage( CAVE, tomb )?.page.regionId, 32770 );
	// From outside, an entrance opens the dungeon on its first floor,
	// centred on that page.
	const outside = { regionId: 250, x: 0, y: 0, z: 0, angle: 0 };
	assert.equal( hud.cavePage( CAVE, outside ), undefined );
	hud.activate( "map-dungeon:2", undefined, undefined );
	const view = defined( hud.cavePage( CAVE, outside ) );
	assert.equal( view.page.regionId, 32775 );
	assert.deepEqual( view.center, cavePageCenter( view.page ) );
	// A floor picked at the entrance shows outside, but walking in
	// starts on the character's own floor instead.
	hud.activate( "map-floor:32771:0", undefined, undefined, false );
	assert.equal( hud.cavePage( CAVE, outside )?.page.regionId, 32771 );
	assert.equal( hud.cavePage( CAVE, tomb ), undefined );
	hud.clearView();
	assert.equal( hud.viewingDungeon(), false );
	assert.equal( hud.cavePage( CAVE, outside ), undefined );
	hud.dispose();
});

test("the dungeon page wears the later client's chrome and its arrows walk the floors", () => {
	const hud = tools();
	const floors = caveDungeonFloors( CAVE, 32769 );
	const outside = { regionId: 250, x: 0, y: 0, z: 0, angle: 0 };
	const shown = () => defined( hud.cavePage( CAVE, outside ) ).page;
	hud.activate( "map-dungeon:1", undefined, undefined );
	assert.equal( shown().floorIndex, 0, "viewing opens on the first floor" );
	// The pin stands on the world too, without the globe.
	let view = paint( hud );
	defined( control( view, "map-pin" ) );
	assert.equal( control( view, "map-globe" ), undefined );
	assert.equal( view.quads.some( quad => quad.texture.endsWith( "wmap_button_passivity.png" ) ), true );
	view = paint( hud, { followed: true } );
	assert.equal( defined( control( view, "map-pin" ) ).selected, true );
	assert.equal( view.quads.some( quad => quad.texture.endsWith( "wmap_button_automatic.png" ) ), true );
	// On the dungeon page: globe, pin and the selector at the first floor.
	view = paint( hud, { cave: true, floors, floor: shown() } );
	defined( control( view, "map-globe" ) );
	defined( control( view, "map-pin" ) );
	assert.equal( view.controls.some( row => row.id.startsWith( "map-floor:" ) ), false, "the floor strip is gone" );
	assert.equal( defined( control( view, "map-floor-down" ) ).disabled, true );
	assert.equal( defined( control( view, "map-floor-up" ) ).disabled, false );
	hud.activate( "map-floor-down", undefined, undefined, false );
	assert.equal( shown().floorIndex, 0, "the down end holds" );
	hud.activate( "map-floor-up", undefined, undefined, false );
	assert.equal( shown().floorIndex, 1 );
	view = paint( hud, { cave: true, floors, floor: shown() } );
	assert.equal( defined( control( view, "map-floor-down" ) ).disabled, false );
	hud.activate( "map-floor-up", undefined, undefined, false );
	paint( hud, { cave: true, floors, floor: shown() } );
	hud.activate( "map-floor-up", undefined, undefined, false );
	assert.equal( shown().floorIndex, 3 );
	view = paint( hud, { cave: true, floors, floor: shown() } );
	assert.equal( defined( control( view, "map-floor-up" ) ).disabled, true );
	hud.activate( "map-floor-up", undefined, undefined, false );
	assert.equal( shown().floorIndex, 3, "the up end holds" );
	hud.activate( "map-floor-down", undefined, undefined, false );
	assert.equal( shown().floorIndex, 2 );
	// The entrance's own icon opens its dungeon, as its label does.
	hud.clearView();
	hud.activate( "map-dungeon-icon:2", undefined, undefined );
	assert.equal( defined( hud.cavePage( CAVE, outside ) ).page.regionId, 32775 );
	hud.dispose();
});

test("the dungeon page draws the atlas globe and pin and demands both (issue #3239)", () => {
	const hud = tools();
	const floors = caveDungeonFloors( CAVE, 32769 );
	const view = paint( hud, { cave: true, floors, floor: floors[0] } );
	assert.equal( view.quads.some( quad => quad.texture.endsWith( "wmap_big_button_world.png" ) ), true );
	assert.equal( view.quads.some( quad => quad.texture.endsWith( "wmap_button_passivity.png" ) ), true );
	assert.equal(
		view.paths.includes( "/assets/images/Media_extracted/interface/worldmap/wmap_big_button_world.png" ),
		true
	);
	assert.equal(
		view.paths.includes( "/assets/images/Media_extracted/interface/worldmap/wmap_button_passivity.png" ),
		true
	);
	hud.dispose();
});

test("the dungeon page's monster list fills for its floor and a pick marks the species", () => {
	const hud = tools();
	const cat = catalogue();
	const floors = caveDungeonFloors( CAVE, 32769 );
	// Shut, the cave page carries the side tab; opening the guide lists
	// the shown floor's species from the catalogue, not "Loading...".
	let view = paint( hud, { cave: true, floors, floor: floors[0] } );
	defined( control( view, "map-side-tab" ) );
	assert.equal( ids( view ).some( id => id.startsWith( "map-cave-monster:" ) ), false );
	hud.activate( "map-side-tab", cat, 20 );
	view = paint( hud, { cave: true, floors, floor: floors[0] } );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-cave-monster:" ) ),
		[ "map-cave-monster:32769:101", "map-cave-monster:32769:102" ]
	);
	// The next floor of the same cave lists its species too.
	view = paint( hud, { cave: true, floors, floor: floors[1] } );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-cave-monster:" ) ),
		[ "map-cave-monster:32769:101", "map-cave-monster:32769:102" ]
	);
	// A Tomb floor lists its own region's species.
	const tomb = caveDungeonFloors( CAVE, 32773 );
	view = paint( hud, { cave: true, floors: tomb, floor: tomb[0] } );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-cave-monster:" ) ),
		[]
	);
	hud.activate( "map-level-filter", cat, 20 );
	view = paint( hud, { cave: true, floors: tomb, floor: tomb[0] } );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-cave-monster:" ) ),
		[ "map-cave-monster:32775:103" ],
		"lifting the level filter shows the floor's Giant"
	);
	// Picking a species marks its anchor on the page.
	hud.activate( "map-cave-monster:32775:103", cat, 20 );
	assert.deepEqual( hud.mark(), { regionId: 32775, x: 300, z: 300, refObjId: 103 } );
	view = paint( hud, { cave: true, floors: tomb, floor: tomb[0] } );
	assert.equal( defined( control( view, "map-cave-monster:32775:103" ) ).selected, true );
	hud.dispose();
});

test("the floor spinner and the foot note stand at their layout places", () => {
	const hud = tools();
	const floors = caveDungeonFloors( CAVE, 32769 );
	// Window 100,100 800x600: the spinner (611,90 in the 650x422 layout)
	// keeps its distance from the right edge; map2 shows its black box
	// only between the arrows' outer rows.
	const written = [];
	const text = {
		...TEXT,
		quads: ( value, rect, clip, color, style ) => (written.push( { value, rect, style } ), [])
	};
	const view = paint( hud, { cave: true, floors, floor: floors[0], text } );
	const box = view.quads.find( quad => quad.texture === "" && quad.rect[2] === 28 && quad.rect[3] === 45 );
	assert.deepEqual( box?.rect, [ 861, 190, 28, 45 ], "the black box behind the arrows" );
	assert.deepEqual( defined( control( view, "map-floor-up" ) ).rect, [ 862, 190, 28, 12 ] );
	assert.deepEqual( defined( control( view, "map-floor-down" ) ).rect, [ 862, 223, 28, 12 ] );
	const label = written.find( row => row.value === "B1" );
	assert.deepEqual( label?.rect, [ 865, 206, 23, 17 ] );
	// The note is the catalogue's own string, right-aligned against the box.
	const line = written.find( row => row.value === "Fixture note" );
	assert.equal( line?.style?.hAlign, 2 );
	assert.deepEqual( line?.rect, [ 123, 707, 733, 10 ] );
	assert.ok( view.quads.some( quad => quad.texture.endsWith( "wmap_wnd_bottom_frame.png" ) ) );
	const filter = control( view, "map-level-filter" );
	assert.equal( filter?.helpText, "Fixture note" );
	hud.activate( "map-level-filter", undefined, undefined );
	assert.equal( hud.filterCap( 20 ), undefined, "the foot's box lifts the filter" );
	hud.dispose();
});

test("a door is named by the client's text and its control is the icon drawn there", () => {
	const hud = tools();
	const center = defined( worldMapDungeonPose( 0x8001 ) );
	let view = paint( hud, { center } );
	const marker = defined( control( view, "map-dungeon-icon:1" ) );
	assert.equal( marker.helpText, "Open the Fixture Cave map" );
	// The world page drew a 32px icon over the door's point: the
	// control takes that icon's rect, so hovering what is seen works.
	const [x, y, w, h] = marker.rect;
	const icon = [ x + w / 2 - 20, y + h / 2 - 12, 32, 32 ];
	view = paint( hud, { center, icons: [ [ 0, 0, 8, 8 ], icon ] } );
	assert.deepEqual( defined( control( view, "map-dungeon-icon:1" ) ).rect, icon );
	// A door the text does not name stays the plain icon.
	view = paint( hud, { center, strings: { SN_ZONE_23001: "Fixture Tomb" } } );
	assert.equal( control( view, "map-dungeon:1" ), undefined );
	assert.equal( control( view, "map-dungeon-icon:1" ), undefined );
	hud.dispose();
});

const GUIDE_PATH = "/transport/references/" + "b".repeat( 64 ) + ".json";
const REGION_A = 60 * 256 + 100, REGION_B = 61 * 256 + 100, REGION_C = 62 * 256 + 100;
const GUIDE_BYTES = new TextEncoder().encode( JSON.stringify( {
	format: "sro-hunting-guide",
	version: 1,
	rows: [
		{
			refObjId: 101,
			nameKey: "SN_FIXTURE_TIGER",
			name: "Fixture Tiger",
			level: 5,
			points: [
				{ regionId: REGION_A, x: 500, z: 700 },
				{ regionId: REGION_A, x: 900, z: 300 },
				{ regionId: REGION_B, x: 400, z: 400 }
			],
			dungeonPoints: [
				{ regionId: 32769, x: 500, z: 700 },
				{ regionId: 32775, x: 300, z: 300 }
			]
		},
		{
			refObjId: 102,
			nameKey: "SN_FIXTURE_BANDIT",
			name: "Fixture Bandit",
			level: 25,
			points: [ { regionId: REGION_A, x: 600, z: 600 } ],
			dungeonPoints: [ { regionId: 32769, x: 600, z: 600 } ]
		},
		{
			refObjId: 103,
			nameKey: "SN_FIXTURE_GIANT",
			name: "Fixture Giant",
			level: 45,
			points: [
				{ regionId: REGION_A, x: 800, z: 800 },
				{ regionId: REGION_C, x: 500, z: 500 }
			]
		}
	]
} ) );
const SOURCE = { url: "https://fixture.invalid" + GUIDE_PATH, bytes: GUIDE_BYTES.byteLength };
const SETTLE_STEPS = 12, STEP_MS = 100;

/*
================
fixture

The production UI at level 20 (filter cap 30) with the fixture guide
offered by the session; the pose can be moved between steps.
================
*/
function fixture() {
	const f = uiFixture(
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{},
		{ [GUIDE_PATH]: GUIDE_BYTES }
	);
	let state = {
		...f.state,
		session: { ...f.state.session, huntingGuide: SOURCE },
		gameplay: { ...f.state.gameplay, progression: { level: 20, masteries: [] } }
	};
	let now = 0, latest;
	/*
	================
	step
	================
	*/
	function step() {
		for ( let index = 0; index < SETTLE_STEPS; index++ ) latest = f.ui.step( state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	/*
	================
	activate
	================
	*/
	function activate( id ) {
		f.ui.event( { kind: "activate", id } );
		return step();
	}
	step();
	return {
		...f,
		activate,
		step,
		/*
		================
		moveTo
		================
		*/
		moveTo( pose, extra = {} ) {
			state = { ...state, gameplay: { ...state.gameplay, pose, ...extra } };
			return step();
		}
	};
}

/*
================
control
================
*/
function control( view, id ) {
	return view.controls.find( row => row.id === id );
}

test("the map opens with the filter and list on, and the drawer honours the filter", () => {
	const f = fixture();
	try {
		let view = f.activate( "open-window:Map" );
		assert.equal( defined( control( view, "map-level-filter" ) ).selected, true, "the filter is on by default" );
		defined( control( view, "map-side-tab" ) );
		view = f.activate( "map-side-tab" );
		defined( control( view, "map-region:" + REGION_A ) );
		defined( control( view, "map-region:" + REGION_B ) );
		assert.equal( control( view, "map-region:" + REGION_C ), undefined, "the Giant's region is over the cap" );
		view = f.activate( "map-region:" + REGION_A );
		defined( control( view, "map-monster:" + REGION_A + ":101" ) );
		defined( control( view, "map-monster:" + REGION_A + ":102" ) );
		assert.equal( control( view, "map-monster:" + REGION_A + ":103" ), undefined, "the Giant is over the cap" );
		view = f.activate( "map-level-filter" );
		assert.equal( defined( control( view, "map-level-filter" ) ).selected, false );
		defined( control( view, "map-monster:" + REGION_A + ":103" ), "lifting the filter shows the Giant" );
		view = f.activate( "map-level-filter" );
		assert.equal( control( view, "map-monster:" + REGION_A + ":103" ), undefined, "the filter hides it again" );
		view = f.activate( "map-monster:" + REGION_A + ":101" );
		assert.equal(
			defined( control( view, "map-monster:" + REGION_A + ":101" ) ).selected,
			true,
			"the picked species is marked"
		);
	} finally {
		f.dispose();
	}
});

/*
================
pageShown

The cave page image the last frame painted, by its published name.
================
*/
function pageShown( f, image ) {
	return f.scenes.at( -1 )?.quads.some( quad => quad.texture.endsWith( image ) ) ?? false;
}

test("dungeon entrances open their map and a dungeon starts on the player's floor", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Map" );
		defined( control( f.step(), "map-pin" ), "the follow pin stands on the world map" );
		// Stand at the Donwhang cave mouth the world map itself names.
		f.moveTo( defined( worldMapDungeonPose( 0x8001 ), "the cave mouth pose" ) );
		let view = f.step();
		defined( control( view, "map-dungeon:1" ), "the cave entrance is offered" );
		// The entrance's own icon takes the click too (issue #3210):
		// the control is the client's xy_dungeon icon drawn at the
		// door, so hovering it shows the same popup as the label
		// (issue #3238: the popup used to answer to the name alone).
		const icon = defined( control( view, "map-dungeon-icon:1" ) );
		assert.equal( icon.helpText, "Open the Donwhang Stone Cave map", "the marker carries the popup text" );
		assert.equal(
			defined( control( view, "map-dungeon:1" ) ).helpText,
			icon.helpText,
			"marker and label name the same popup"
		);
		assert.equal(
			f.scenes.at( -1 )?.quads.some( quad =>
				quad.texture.endsWith( "xy_dungeon.png" ) &&
				quad.rect[0] <= icon.rect[0] && quad.rect[1] <= icon.rect[1] &&
				quad.rect[0] + quad.rect[2] >= icon.rect[0] + icon.rect[2] &&
				quad.rect[1] + quad.rect[3] >= icon.rect[1] + icon.rect[3]
			),
			true,
			"the control is the door icon the world page painted"
		);
		view = f.activate( "map-dungeon-icon:1" );
		// The dungeon page wears the later client's chrome: globe and
		// pin at the top right, the floor selector down the right edge.
		defined( control( view, "map-globe" ), "the globe back to the world" );
		defined( control( view, "map-pin" ) );
		assert.equal( control( view, "map-floor:32769:0" ), undefined, "the floor strip is gone" );
		assert.equal(
			defined( control( view, "map-floor-down" ) ).disabled,
			true,
			"viewing opens on the first floor"
		);
		assert.equal( pageShown( f, "map_world_donf01.png" ), true, "the first floor opens" );
		// Viewed from above ground, the page carries no "you are
		// here" sign (issue #3239): the only character sign in the
		// frame would sit in the map's wide clip.
		const caveMarker = () =>
			(f.scenes.at( -1 )?.quads ?? []).some(
				quad => quad.texture.endsWith( "mm_sign_character.png" ) && (quad.clip?.[2] ?? 0) > 300
			);
		assert.equal( caveMarker(), false, "no player sign on a dungeon viewed from outside" );
		// The page's monster list fills from the catalogue (issue #3239).
		view = f.activate( "map-side-tab" );
		defined( control( view, "map-cave-monster:32769:101" ) );
		defined( control( view, "map-cave-monster:32769:102" ) );
		view = f.activate( "map-floor-up" );
		assert.equal( defined( control( view, "map-floor-down" ) ).disabled, false );
		assert.equal( pageShown( f, "map_world_donf02.png" ), true );
		defined( control( view, "map-cave-monster:32769:101" ), "the second floor lists its species too" );
		// The globe is the way back.
		view = f.activate( "map-globe" );
		defined( control( view, "map-side-tab" ), "the world page's side tab is back" );
		assert.equal( control( view, "map-floor-up" ), undefined );
		// Inside on the second floor, the selector starts there.
		view = f.moveTo( { regionId: 32769, x: 100, y: 0, z: 100, angle: 0 }, { navigationFloor: 1 } );
		defined( control( view, "map-side-tab" ), "the side tab stays on the cave page (issue #3239)" );
		assert.equal(
			view.controls.some( row => row.id.startsWith( "map-region:" ) ),
			false,
			"the world drawer's regions stay on the world"
		);
		assert.equal(
			(f.scenes.at( -1 )?.quads ?? []).some(
				quad => quad.texture.endsWith( "mm_sign_character.png" ) && (quad.clip?.[2] ?? 0) > 300
			),
			true,
			"the player sign stands on their own floor"
		);
		assert.equal( pageShown( f, "map_world_donf02.png" ), true, "the player's own floor is shown" );
		assert.equal( defined( control( view, "map-floor-down" ) ).disabled, false );
		view = f.activate( "map-floor-up" );
		assert.equal( pageShown( f, "map_world_donf03.png" ), true );
		view = f.activate( "map-floor-up" );
		assert.equal( pageShown( f, "map_world_donf04.png" ), true );
		assert.equal(
			defined( control( view, "map-floor-up" ) ).disabled,
			true,
			"the last floor has no floor below"
		);
	} finally {
		f.dispose();
	}
});

test("the globe stands off the world page beside the pin, at their layout places", () => {
	const hud = tools();
	// Window 100,100 800x600: the pin (605,39) and the globe (567,39)
	// keep their distance from the window's right edge.
	let view = paint( hud );
	assert.deepEqual( defined( control( view, "map-pin" ) ).rect, [ 855, 139, 36, 36 ] );
	assert.equal( control( view, "map-globe" ), undefined, "the world page needs no way back to itself" );
	assert.equal( control( view, "map-follow" ), undefined, "no captioned AUTO_MOVE button" );
	assert.equal( control( view, "map-locate" ), undefined, "v1.188 has no location button" );
	view = paint( hud, { page: 1 } );
	assert.deepEqual( defined( control( view, "map-globe" ) ).rect, [ 817, 139, 36, 36 ], "a town page shows it" );
	assert.ok( view.paths.some( path => path.endsWith( "wmap_big_button_world.png" ) ) );
	hud.dispose();
});

test("the globe inside a dungeon shows the world until the pin brings the floor back", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Map" );
		// Standing on the cave's second floor, the M window opens on it.
		let view = f.moveTo( { regionId: 32769, x: 100, y: 0, z: 100, angle: 0 }, { navigationFloor: 1 } );
		assert.equal( pageShown( f, "map_world_donf02.png" ), true, "the player's floor is shown" );
		defined( control( view, "map-globe" ) );
		// The globe leaves the cave page for the world, and it holds
		// frame after frame while the player stays inside.
		view = f.activate( "map-globe" );
		view = f.step();
		assert.equal( pageShown( f, "map_world_donf02.png" ), false, "the floor page is gone" );
		assert.equal( control( view, "map-globe" ), undefined, "the world page has no globe" );
		assert.equal( control( view, "map-floor-up" ), undefined, "nor the floor selector" );
		defined( control( view, "map-dungeon-icon:1" ), "the world page offers the cave entrance" );
		// The pin follows the player again: back to their floor.
		view = f.activate( "map-pin" );
		assert.equal( pageShown( f, "map_world_donf02.png" ), true, "the pin restores the floor page" );
		// The entrance's icon opens the cave from the world page too.
		f.activate( "map-globe" );
		assert.equal( pageShown( f, "map_world_donf02.png" ), false );
		f.activate( "map-dungeon-icon:1" );
		assert.equal( pageShown( f, "map_world_donf02.png" ), true, "the entrance restores the floor page" );
	} finally {
		f.dispose();
	}
});
