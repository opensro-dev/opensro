/*
===========================================================================

world-map-monster-list.test.mjs - the M window's monster list (#3554)

The list scrolls and its groups can be told apart. The whole UI, with a
hunting guide of more groups than the tree shows at once (eight), takes
the mouse wheel over the list and a drag of the scrollbar's thumb. The
map tools merge the region sectors that share a shown name into one
group, as the later client's Monster tree has one row per hunting area,
and a species picked from a merged group still centres on its own
sector.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { WORLDMAP_GUIDE_CATALOG } = await import( "../../src/engine/foundation/ui/worldmap-guide.ts" );
const { directoryByName, monsterDirectory } = await import( "../../src/engine/foundation/ui/hunting-guide.ts" );
const { createMapToolsHud } = await import( "../../src/engine/runtime/ui/hud/map-tools.ts" );

const GUIDE_PATH = "/transport/references/" + "c".repeat( 64 ) + ".json";
const GROUPS = 12;
// Region sectors keyed rz*256+rx, one species each, all inside the
// level 20 filter band.
const REGIONS = Array.from( { length: GROUPS }, ( _, index ) => (60 + index) * 256 + 100 );
const GUIDE_BYTES = new TextEncoder().encode( JSON.stringify( {
	format: "sro-hunting-guide",
	version: 1,
	rows: REGIONS.map( ( regionId, index ) => ({
		refObjId: 200 + index,
		nameKey: "SN_FIXTURE_" + index,
		name: "Fixture " + index,
		level: 20 + (index % 5),
		points: [ { regionId, x: 500, z: 500 } ]
	}) )
} ) );
const SOURCE = { url: "https://fixture.invalid" + GUIDE_PATH, bytes: GUIDE_BYTES.byteLength };
const SETTLE_STEPS = 12, STEP_MS = 100;
const THUMB = "map-guide-scroll-thumb";
const VISIBLE_ROWS = 8;
const DRAG_MOVES = 200;

/*
================
fixture

The production UI at level 20 with the fixture guide offered by the
session and the M window's monster drawer open. The build's v1.696
guide catalogue is absent, so the drawer lists the session's guide.
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
		{ [GUIDE_PATH]: GUIDE_BYTES, [WORLDMAP_GUIDE_CATALOG]: { absent: true } }
	);
	const state = {
		...f.state,
		session: { ...f.state.session, huntingGuide: SOURCE },
		gameplay: { ...f.state.gameplay, progression: { level: 20, masteries: [] } }
	};
	let now = 0, latest;
	/*
	================
	send
	================
	*/
	function send( event ) {
		if ( event ) f.ui.event( event );
		for ( let index = 0; index < SETTLE_STEPS; index++ ) latest = f.ui.step( state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	send();
	send( { kind: "activate", id: "open-window:Map" } );
	return { ...f, send, view: send( { kind: "activate", id: "map-side-tab" } ) };
}

/*
================
groupsShown

The group rows the tree shows, in screen order.
================
*/
function groupsShown( view ) {
	return view.controls.filter( row => row.id.startsWith( "map-region:" ) ).map( row => row.id );
}

test("the wheel over the monster list scrolls it a row a notch, and only over the list", () => {
	const f = fixture();
	try {
		const first = groupsShown( f.view );
		assert.equal( first.length, VISIBLE_ROWS, "eight rows at a time" );
		const row = defined( f.view.controls.find( c => c.id === first[0] ) );
		const x = row.rect[0] + 10, y = row.rect[1] + 10;
		let view = f.send( { kind: "scroll", x, y, delta: 1 } );
		assert.deepEqual( groupsShown( view ).slice( 0, VISIBLE_ROWS - 1 ), first.slice( 1 ), "one notch, one row" );
		for ( let index = 0; index < GROUPS; index++ ) view = f.send( { kind: "scroll", x, y, delta: 1 } );
		const last = groupsShown( view );
		assert.equal( last.length, VISIBLE_ROWS, "the far end holds a full page" );
		assert.equal( last.includes( first[0] ), false, "the wheel reached the later groups" );
		for ( let index = 0; index < GROUPS; index++ ) view = f.send( { kind: "scroll", x, y, delta: -1 } );
		assert.deepEqual( groupsShown( view ), first, "the wheel back up returns to the first group" );
		// Over the map itself, right of the panel, the list stays put.
		view = f.send( { kind: "scroll", x: x + 600, y, delta: 1 } );
		assert.deepEqual( groupsShown( view ), first );
	} finally {
		f.dispose();
	}
});

test("dragging the scrollbar's thumb down moves the list to the last groups", () => {
	const f = fixture();
	try {
		const first = groupsShown( f.view );
		const thumb = defined( f.view.controls.find( c => c.id === THUMB ) );
		assert.equal( thumb.draggable, true );
		// The pointer layer sends a drag per move, a pixel or two each.
		for ( let index = 0; index < DRAG_MOVES; index++ ) f.ui.event( { kind: "drag", id: THUMB, dx: 0, dy: 1 } );
		const view = f.send();
		const last = groupsShown( view );
		assert.equal( last.length, VISIBLE_ROWS );
		assert.equal( last.includes( first[0] ), false, "the thumb's drag moved the list" );
		assert.equal( defined( view.controls.find( c => c.id === THUMB ) ).rect[1] > thumb.rect[1], true );
	} finally {
		f.dispose();
	}
});

/*
================
monster
================
*/
function monster( refObjId, name, level ) {
	return { refObjId, name, nameKey: "SN_FIXTURE_" + refObjId, level };
}

const TIGER = monster( 101, "Tiger", 21 ), BANDIT = monster( 102, "Bandit", 25 ), GIANT = monster( 103, "Giant", 45 );

/*
================
catalogue

Sectors 50 and 100 both lie in "Fixture Field" (each holds the Tiger);
sector 200 is "Fixture Hills". Anchors are world units.
================
*/
function catalogue() {
	return {
		monsters: [ TIGER, BANDIT, GIANT ],
		regions: new Map( [
			[ 100, [ { monster: TIGER, x: 19250, z: 30 }, { monster: GIANT, x: 19300, z: 40 } ] ],
			[ 50, [ { monster: TIGER, x: 9650, z: 25 }, { monster: BANDIT, x: 9700, z: 20 } ] ],
			[ 200, [ { monster: GIANT, x: 38500, z: 60 } ] ]
		] ),
		dungeons: new Map(),
		sections: []
	};
}

const ZONES = { "50": "Fixture Field", "100": "Fixture Field", "200": "Fixture Hills" };

test("directoryByName merges the sectors of one name, each species once at its first sector", () => {
	const names = new Map( Object.entries( ZONES ).map( ( [region, name] ) => [ Number( region ), name ] ) );
	const merged = directoryByName( monsterDirectory( catalogue() ), names );
	assert.deepEqual(
		merged.map( row => [ row.region, row.minLevel, row.maxLevel ] ),
		[ [ 50, 21, 45 ], [ 200, 45, 45 ] ],
		"one group per name, keeping the lowest sector's id and spanning both sectors' levels"
	);
	assert.deepEqual(
		merged[0].monsters.map( row => [ row.monster.name, row.region ] ),
		[ [ "Tiger", 50 ], [ "Bandit", 50 ], [ "Giant", 100 ] ]
	);
});

const TEXT = { run: () => ({ width: 40 }), extentHeight: () => 12, quads: () => [] };

/*
================
paint
================
*/
function paint( hud ) {
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
		zones: ZONES,
		strings: {},
		levelNote: "Fixture note",
		icons: [],
		text: TEXT,
		page: 0,
		pan: [ 0, 0 ],
		center: { regionId: 250, x: 0, y: 0, z: 0, angle: 0 },
		size: () => undefined
	} );
}

test("the drawer shows one group per zone name, and a pick centres on the species' own sector", () => {
	const hud = createMapToolsHud( {
		assets: { available: () => 0, request: () => 1, take: () => null, cancel: () => {} },
		base: "https://fixture.invalid/"
	} );
	const cat = catalogue();
	try {
		hud.activate( "map-side-tab", cat, 20 );
		hud.activate( "map-level-filter", cat, 20 );
		hud.activate( "map-region:50", cat, 20 );
		const view = paint( hud );
		const groups = view.controls.filter( row => row.id.startsWith( "map-region:" ) );
		assert.deepEqual(
			groups.map( row => row.label ),
			[ "Fixture Field  Lv.21~45", "Fixture Hills  Lv.45~45" ],
			"no two groups read alike"
		);
		assert.deepEqual(
			view.controls.filter( row => row.id.startsWith( "map-monster:" ) ).map( row => row.id ),
			[ "map-monster:50:101", "map-monster:50:102", "map-monster:100:103" ]
		);
		// The Giant's anchor in sector 100 is world (19300, 40): local x10.
		assert.deepEqual( hud.activate( "map-monster:100:103", cat, 20 ), {
			regionId: 100,
			x: 1000,
			y: 0,
			z: 400,
			angle: 0
		} );
	} finally {
		hud.dispose();
	}
});
