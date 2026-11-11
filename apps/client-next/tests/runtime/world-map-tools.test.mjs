/*
===========================================================================

world-map-tools.test.mjs - the world map's own tools (issue #3167)

Pins the monster directory and the level filter at the foundation, the
tools' own state (drawer walk, species pick, floor choice, dungeon
view) against a stub text sink, and the whole window end to end in the
UI fixture: the filter and the list are on by default, the drawer
walks regions to species, a pick marks the species, the dungeon
entrances open their maps, and a dungeon starts on the player's floor.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { MONSTER_LEVEL_FILTER_SPAN, filterHuntingGuide, monsterDirectory } = await import(
	"../../src/engine/foundation/ui/hunting-guide.ts"
);
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
	version: 1,
	imageRoot: "/x/",
	signIcon: "icon",
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
		floors: [],
		floor: undefined,
		window: [ 100, 100, 800, 600 ],
		clip: [ 108, 140, 784, 500 ],
		full: [ 0, 0, 1600, 900 ],
		catalogue: catalogue(),
		characterLevel: 20,
		zones: { "50": "Fixture Field" },
		text: TEXT,
		page: 0,
		pan: [ 0, 0 ],
		center: { regionId: 250, x: 0, y: 0, z: 0, angle: 0 },
		huntingVisible: false,
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

test("the drawer walks regions to species and a pick returns the anchor pose", () => {
	const hud = tools();
	const cat = catalogue();
	assert.equal( ids( paint( hud ) ).includes( "map-region:50" ), false, "the drawer starts closed" );
	hud.activate( "map-list", cat, 20 );
	let view = paint( hud );
	// At level 20 the cap is 30: the Giant's region 200 is hidden whole.
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-region:" ) ),
		[ "map-region:50", "map-region:100" ]
	);
	assert.equal(
		view.controls.find( row => row.id === "map-region:50" )?.label?.startsWith( "Fixture Field" ),
		true
	);
	hud.activate( "map-region:100", cat, 20 );
	view = paint( hud );
	assert.deepEqual(
		ids( view ).filter( id => id.startsWith( "map-monster:" ) ),
		[ "map-monster:100:101", "map-monster:100:102" ]
	);
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
	assert.equal( paint( hud ).controls.find( row => row.id === "map-monster:100:101" )?.selected, true );
	// The same species picked from region 50's list centres there instead.
	assert.deepEqual( hud.activate( "map-monster:50:101", cat, 20 ), {
		regionId: 50,
		x: 500,
		y: 0,
		z: 250,
		angle: 0
	} );
	// Lifting the filter brings the Giant and its region back.
	hud.activate( "map-level-filter", cat, 20 );
	assert.deepEqual(
		ids( paint( hud ) ).filter( id => id.startsWith( "map-monster:" ) ),
		[ "map-monster:100:101", "map-monster:100:102", "map-monster:100:103" ]
	);
	hud.activate( "map-drawer-back", cat, 20 );
	assert.equal( ids( paint( hud ) ).includes( "map-region:200" ), true );
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
			]
		},
		{
			refObjId: 102,
			nameKey: "SN_FIXTURE_BANDIT",
			name: "Fixture Bandit",
			level: 25,
			points: [ { regionId: REGION_A, x: 600, z: 600 } ]
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
		defined( control( view, "map-list" ) );
		view = f.activate( "map-list" );
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

test("dungeon entrances open their map and a dungeon starts on the player's floor", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Map" );
		// Stand at the Donwhang cave mouth the world map itself names.
		f.moveTo( defined( worldMapDungeonPose( 0x8001 ), "the cave mouth pose" ) );
		let view = f.step();
		defined( control( view, "map-dungeon:1" ), "the cave entrance is offered" );
		view = f.activate( "map-dungeon:1" );
		for ( const floor of [ 0, 1, 2, 3 ] ) defined( control( view, "map-floor:32769:" + floor ) );
		assert.equal( defined( control( view, "map-floor:32769:0" ) ).selected, true, "the first floor opens" );
		view = f.activate( "map-floor:32769:2" );
		assert.equal( defined( control( view, "map-floor:32769:2" ) ).selected, true );
		// Inside on the second floor, the selector starts there.
		view = f.moveTo( { regionId: 32769, x: 100, y: 0, z: 100, angle: 0 }, { navigationFloor: 1 } );
		assert.equal( control( view, "map-level-filter" ), undefined, "the cave strip replaces the world tools" );
		assert.equal(
			defined( control( view, "map-floor:32769:1" ) ).selected,
			true,
			"the player's own floor is selected"
		);
		view = f.activate( "map-floor:32769:3" );
		assert.equal( defined( control( view, "map-floor:32769:3" ) ).selected, true );
	} finally {
		f.dispose();
	}
});
