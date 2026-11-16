/*
===========================================================================

world-map-open.test.mjs - the world map window opens on the press frame

Native loads interface art synchronously from local PK2, so the M window
appears on the frame its button or hotkey is pressed (frame/admission.ts:
53FF00 and 5405DF). This port draws a window through admission, which
holds the whole window while any of its own art is still decoding, so the
map's chrome must already be resident when the press arrives: the shut map
demands it (world-map.ts, the MAP_CHROME_ART and GUIDE_TREE_ART lists).
Without that demand the resource owner trims the art while the map is
shut - it is no longer wanted and the map's own tile demand keeps the
resident set over its 480-entry budget - and every open waits for the
fetch and decode again.

The lists are complete for every state the window can open in, and the
last test holds them to that: the art the window gates admission on must
be art the shut window already asks for.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { caveMapCatalog } = await import( "../../src/engine/foundation/ui/cave-map.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { panelCloseId } = await import( "../../src/engine/foundation/ui/panels.ts" );
const { GUIDE_SCROLL_THUMB, GUIDE_TREE_ART, paintGuideTree } = await import(
	"../../src/engine/runtime/ui/hud/map-guide-tree.ts"
);
const { MAP_CHROME_ART } = await import( "../../src/engine/runtime/ui/hud/map-chrome.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

const FRAME_MS = 100;
// A frame or two for the asset owner to hand back what the demand asked
// for, and one for the layout that publishes it.
const SETTLE_FRAMES = 40;
// The trim the resource owner runs over a closed map: its resident set
// falls back under budget and every path the shut window does not ask for
// is released.
const CLOSED_FRAMES = 12;
// A press that changes the map's state (the pin) must publish the window on
// its own frame.
const CAVE_CATALOG = "/assets/data/later-dungeon-map.json";
const GUIDE_CATALOG = "/assets/data/worldmap-guide.json";
// Donwhang's cave (world-map-tools' fixture region) and the two floors the
// published catalogue gives it.
const DONWHANG = 32769;
const FLOORS = 2;
// Monster groups in the published guide: more than the tree's eight rows,
// so its scrollbar runs.
const GUIDE_GROUPS = 20;
// Every image the M window's chrome and guide tree can draw. The test build
// publishes none of it, so the fixture serves all of it: a path it did not
// serve would be known absent, and absent art never holds admission - a
// dropped list entry would then pass unnoticed.
const WINDOW_ART = [ ...MAP_CHROME_ART, ...GUIDE_TREE_ART ];
// The fixture reads only a PNG's width and height (its IHDR words at bytes
// 16 and 20), so a bare signature and header stand for that art. No retail
// image is read: CI's source lane has no extracted client.
const ART_SIZE = 16;

/*
================
pngHeader

The first 24 bytes of a PNG of size x size: signature, IHDR length and
type, width, height.
================
*/
function pngHeader( size ) {
	const bytes = Buffer.alloc( 24 );
	Buffer.from( [ 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a ] ).copy( bytes, 0 );
	bytes.writeUInt32BE( 13, 8 );
	bytes.write( "IHDR", 12, "ascii" );
	bytes.writeUInt32BE( size, 16 );
	bytes.writeUInt32BE( size, 20 );
	return bytes;
}

const WORLD = { regionId: 97 * 256 + 160, x: 960, y: 0, z: 960, angle: 0 };
const DUNGEON = { regionId: DONWHANG, x: 100, y: 0, z: 100, angle: 0 };

/*
================
caveCatalog

The published dungeon pages (cave-map.ts): the two Donwhang floors, so
the M window opens on the player's own floor and its spinner has both
arrows.
================
*/
function caveCatalog() {
	const page = ( floorIndex, layer, floorLabel ) => ({
		regionId: DONWHANG,
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
		image: "fixture-floor-" + floorIndex,
		labels: [],
		signs: []
	});
	return {
		format: "sro-later-dungeon-map",
		version: 2,
		imageRoot: "/fixture/dungeon/",
		signIcon: "sign",
		monsterLevelNote: "Fixture note",
		maps: Array.from(
			{ length: FLOORS },
			( _, floorIndex ) => page( floorIndex, floorIndex + 1, "F" + floorIndex )
		)
	};
}

/*
================
guideCatalog

The published world map guide (worldmap-guide.ts) with enough monster
groups for its tree to scroll. The Flat list is the Monster tab's; the
Field tab has no rows and draws the same art.
================
*/
function guideCatalog() {
	const group = index => ({
		name: "Fixture group " + index,
		level: null,
		page: "field",
		main: true,
		order: [ index, index, index ],
		sector: [ 0, 0 ],
		point: [ 0, 0 ],
		tier: 0,
		tiles: []
	});
	return {
		format: "opensro-worldmap-guide-v1",
		imageRoot: "/fixture/guide/",
		pages: {},
		areas: [],
		monsters: Array.from( { length: GUIDE_GROUPS }, ( _, index ) => group( index ) ),
		localinfo: {}
	};
}

/*
================
fixture

The production UI in the world, with the window chrome and the dungeon and
guide catalogues served, stepping one frame at a time so a press frame can
be read on its own.
================
*/
function fixture() {
	const served = pngHeader( ART_SIZE );
	const published = {
		...Object.fromEntries( WINDOW_ART.map( path => [ path, served ] ) ),
		[CAVE_CATALOG]: Buffer.from( JSON.stringify( caveCatalog() ) ),
		[GUIDE_CATALOG]: Buffer.from( JSON.stringify( guideCatalog() ) )
	};
	const f = uiFixture(
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{},
		published
	);
	let state = f.state, now = 0;
	// The last layout that published: the scene on screen, which is what a
	// state assertion reads (a step that changes nothing returns null).
	let last = null;
	/*
	================
	frames

	Advance the clock and run that many layouts.
	================
	*/
	function frames( count ) {
		for ( let i = 0; i < count; i++ ) last = f.rawStep( state, now += FRAME_MS ) ?? last;
		return last;
	}
	return {
		...f,
		get state() {
			return state;
		},
		/*
		================
		settle

		The world with no window open and every image it asked for landed.
		================
		*/
		settle() {
			frames( SETTLE_FRAMES );
			assert.equal( f.ui.stats().panel, "", "the world is up with no window open" );
			assert.equal( f.ui.stats().pending, 0, "every image the world asked for has landed" );
		},
		/*
		================
		moveTo

		Move the character (and its floor) between frames, as play does.
		================
		*/
		moveTo( pose, extra = {} ) {
			state = { ...state, gameplay: { ...state.gameplay, pose, ...extra } };
			return frames( 8 );
		},
		/*
		================
		press

		One press, then the one frame it must be answered on. Null when that
		frame published nothing: the withheld window leaves the last copy on
		screen unchanged.
		================
		*/
		press( id ) {
			f.ui.event( { kind: "activate", id } );
			const view = f.rawStep( state, now += FRAME_MS );
			last = view ?? last;
			return view;
		},
		/*
		================
		step

		A frame that publishes nothing still advances the resource owner.
		================
		*/
		step() {
			return frames( 1 );
		},
		/*
		================
		scene

		The last published layout: the controls on screen.
		================
		*/
		scene() {
			return last;
		},
		/*
		================
		drawerShows

		The guide rows the last published scene drew, in order: the drawer's
		scroll position read off the scene itself.
		================
		*/
		drawerShows() {
			return (last?.controls ?? []).map( control => control.id ).filter( id =>
				id.startsWith( "map-guide-main:" )
			);
		}
	};
}

/*
================
control
================
*/
function control( view, id ) {
	return view?.controls.find( row => row.id === id );
}

/*
================
assertAdmitted

The M window is drawn whole on `view`: its close control and its map
surfaces are in the published scene, no art it gates admission on is still
loading, and the window was not left as a disabled stale copy.
================
*/
function assertAdmitted( view, f, when ) {
	assert.ok( view, when + ": the frame publishes the map window" );
	const ids = view.controls.map( row => row.id );
	assert.ok( ids.includes( panelCloseId( "Map" ) ), when + ": the Map close control is drawn" );
	for ( const id of [ "map-pan", "map-drag" ] ) {
		const row = control( view, id );
		assert.ok( row, when + ": the " + id + " hit area is drawn" );
		assert.notEqual( row.disabled, true, when + ": the " + id + " hit area takes input" );
	}
	assert.deepEqual( f.ui.stats().windowMissing, [], when + ": no map art is still loading" );
}

test("the first press shows the map on its own frame", () => {
	const f = fixture();
	try {
		f.settle();
		assertAdmitted( f.press( "toggle-window:Map" ), f, "first open" );
	} finally {
		f.dispose();
	}
});

test("every later open shows the map on its own frame", () => {
	const f = fixture();
	try {
		f.settle();
		assertAdmitted( f.press( "toggle-window:Map" ), f, "first open" );
		f.press( "toggle-window:Map" );
		assert.equal( f.ui.stats().panel, "", "the second press closes the map" );
		// The closed map no longer draws its chrome: the resource owner's
		// trim drops what the map batch held unless the shut window asks.
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		assertAdmitted( f.press( "toggle-window:Map" ), f, "reopen" );
	} finally {
		f.dispose();
	}
});

test("one press opens the map, the next closes it", () => {
	const f = fixture();
	try {
		f.settle();
		f.press( "toggle-window:Map" );
		assert.equal( f.ui.stats().panel, "Map" );
		f.press( "toggle-window:Map" );
		assert.equal( f.ui.stats().panel, "" );
	} finally {
		f.dispose();
	}
});

test("a dungeon floor the globe left is drawn whole when the pin brings it back", () => {
	const f = fixture();
	try {
		f.moveTo( DUNGEON, { navigationFloor: 1 } );
		f.settle();
		// The floor page: its spinner is the art the world page never draws.
		const opened = f.press( "toggle-window:Map" );
		assertAdmitted( opened, f, "dungeon open" );
		assert.ok( control( opened, "map-floor-down" ), "the floor spinner is drawn on the floor page" );
		f.press( "map-globe" );
		assert.equal( control( f.step(), "map-floor-down" ), undefined, "the globe shows the world page" );
		f.press( "toggle-window:Map" );
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		assertAdmitted( f.press( "toggle-window:Map" ), f, "reopen on the world page" );
		// The pin follows the character again: the floor page and its spinner
		// art are drawn on the pin's own frame.
		const back = f.press( "map-pin" );
		assertAdmitted( back, f, "pin back to the floor" );
		assert.ok( control( back, "map-floor-down" ), "the floor spinner is drawn again" );
	} finally {
		f.dispose();
	}
});

test("a guide drawer opened, closed and reopened is drawn whole on the press frame", () => {
	const f = fixture();
	try {
		f.settle();
		assertAdmitted( f.press( "toggle-window:Map" ), f, "first open" );
		f.press( "map-side-tab" );
		f.press( "map-guide-tab:monster" );
		f.step();
		assert.ok( control( f.scene(), "map-guide-tab:monster" ), "the drawer is open" );
		f.press( "toggle-window:Map" );
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		const reopened = f.press( "toggle-window:Map" );
		assertAdmitted( reopened, f, "drawer reopen" );
		assert.ok( control( reopened, "map-guide-tab:monster" ), "the drawer is drawn with the window" );
	} finally {
		f.dispose();
	}
});

test("a state switch inside the open map is drawn whole on its own frame", () => {
	const f = fixture();
	try {
		f.moveTo( DUNGEON, { navigationFloor: 1 } );
		f.settle();
		assertAdmitted( f.press( "toggle-window:Map" ), f, "floor open" );
		// These are the states the floor page does not draw, so the trim takes
		// them unless the open map keeps asking for its art too.
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		const drawer = f.press( "map-side-tab" );
		assertAdmitted( drawer, f, "the side tab opens the drawer" );
		assert.ok( control( drawer, "map-guide-tab:monster" ), "the drawer is drawn on its own frame" );
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		const world = f.press( "map-globe" );
		assertAdmitted( world, f, "the globe shows the world page" );
		assert.equal( control( world, "map-floor-down" ), undefined, "the floor spinner is off the world page" );
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		const floor = f.press( "map-pin" );
		assertAdmitted( floor, f, "the pin brings the floor back" );
		assert.ok( control( floor, "map-floor-down" ), "the floor spinner is drawn again" );
	} finally {
		f.dispose();
	}
});

test("a town page's globe is drawn whole on its own frame", () => {
	const f = fixture();
	try {
		// Donwhang's town page: the window draws the globe and no floor
		// spinner. (Jangan's globe lands the world view on two fortress icons
		// that share one page, which the fixture's control-identity check
		// refuses - a separate defect.)
		f.moveTo( { regionId: 103 * 256 + 153, x: 960, y: 0, z: 960, angle: 0 } );
		f.settle();
		const town = f.press( "toggle-window:Map" );
		assertAdmitted( town, f, "town page open" );
		assert.ok( control( town, "map-globe" ), "the globe is drawn on the town page" );
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		assertAdmitted( f.press( "map-globe" ), f, "the globe shows the world page off a town" );
	} finally {
		f.dispose();
	}
});

test("the guide drawer keeps its scroll while the map is shut and the player moves", () => {
	const f = fixture();
	const home = f.state.gameplay.pose;
	try {
		f.settle();
		f.press( "toggle-window:Map" );
		f.press( "map-side-tab" );
		f.press( "map-guide-tab:monster" );
		f.step();
		// Scroll the tree to its last page: one row per press of the down
		// arrow, and the rows shown are that page's.
		for ( let i = 0; i < GUIDE_GROUPS; i++ ) f.press( "map-guide-scroll-down" );
		const scrolled = f.drawerShows();
		assert.ok( scrolled.length > 0, "the drawer lists its groups" );
		assert.equal(
			scrolled[0],
			"map-guide-main:" + (GUIDE_GROUPS - scrolled.length),
			"the tree is on its last page"
		);
		f.press( "toggle-window:Map" );
		// The player walks into a cave while the map is shut: a shut map that
		// painted its drawer here read the cave's own (empty) list and clamped
		// the scroll to its first page.
		f.moveTo( DUNGEON, { navigationFloor: 1 } );
		for ( let i = 0; i < CLOSED_FRAMES; i++ ) f.step();
		f.moveTo( home );
		assertAdmitted( f.press( "toggle-window:Map" ), f, "reopen after the move" );
		assert.deepEqual( f.drawerShows(), scrolled, "the tree is still on its last page" );
	} finally {
		f.dispose();
	}
});

// ============================================================================

/*
================
mapAssets

The asset port the map tools load through: the guide catalogue lands, so
the drawer has rows, and every other request is a file this build lacks.
================
*/
/** @returns {any} */
function mapAssets() {
	const urls = new Map();
	let next = 0;
	return {
		available: () => 8,
		request( url ) {
			const id = ++next;
			urls.set( id, url );
			return id;
		},
		take( id ) {
			const url = urls.get( id );
			urls.delete( id );
			return url?.endsWith( GUIDE_CATALOG ) ?
				{ kind: "bytes", buffer: Buffer.from( JSON.stringify( guideCatalog() ) ) } :
				{ kind: "error", error: "absent", absent: true };
		},
		cancel( id ) {
			urls.delete( id );
		}
	};
}

/*
================
presentFrame

One map HUD frame in one state: the paths it demands, and the paths an open
window pushes between beginWindow and endWindow - the art admission gates.
The authored HUD nodes' buttons and the frame's shared chrome are not among
them (the UI demands those every frame: hud warmPaths, commonWindowPaths).
================
*/
/**
 * @param {{ panel: string, pose: any, caveMaps?: any, drawer?: boolean, tab?: string, hover?: string | null }} state
 */
function presentFrame( { panel, pose, caveMaps, drawer = false, tab = "monster", hover = null } ) {
	const hud = createWorldMapHud( {
		assets: mapAssets(),
		base: "http://fixture.invalid",
		experimental: { state: () => ({ saved: experimentalOptions() }), restore: () => {} }
	} );
	const paths = [], gated = [];
	const input = /** @type {any} */ ({
		next: { entities: [] },
		game: { pose },
		pose,
		panel,
		w: 1200,
		h: 900,
		full: [ 0, 0, 1200, 900 ],
		hover,
		hudData: { map: {}, zones: {}, strings: {} },
		trackedQuest: 0,
		minimapResources: { positions: () => undefined },
		...(caveMaps ? { caveMaps } : {}),
		mapTeleport: { view: () => {}, viewCave: () => {}, hide: () => {} },
		reverseScrollHud: { mapping: () => false },
		text: {
			path: () => null,
			run: value => ({ width: value.length * 6 }),
			extentHeight: () => 12,
			quads: () => []
		},
		resources: { has: () => true, size: () => [ 16, 16 ] },
		paths,
		quads: [],
		controls: [],
		hudCopy: key => key,
		beginWindow: () => [ 0, 0, 0, paths.length ],
		endWindow: mark => gated.push( ...paths.slice( mark[3] ) ),
		windowBox: () => {},
		authoredButton: () => {},
		closeButton: () => {},
		image: ( rect, path ) => paths.push( path )
	});
	try {
		if ( drawer ) {
			hud.activate( "map-side-tab", pose );
			hud.activate( "map-guide-tab:" + tab, pose );
			// The drawer's catalogue lands here, so the tree draws its rows.
			for ( let i = 0; i < 4; i++ ) hud.step( /** @type {any} */ ({}), "Map" );
		}
		hud.present( input );
		return { paths: [ ...paths ], gated: [ ...gated ] };
	} finally {
		hud.dispose();
	}
}

/*
================
treeArt

The guide tree's paths for one interaction state. The map HUD cannot press
a tree control (map-tools passes `pressed: null`), so the press face is
painted here on its own.
================
*/
function treeArt( pressed ) {
	const out = { quads: [], controls: [], paths: [] };
	paintGuideTree(
		/** @type {any} */ ({
			panel: [ 0, 0, 298, 296 ],
			full: [ 0, 0, 1200, 900 ],
			rows: [ { id: "map-guide-main:0", label: "Fixture group", group: true, open: false, color: "group" } ],
			scroll: 0,
			empty: "-",
			hover: null,
			pressed,
			text: { run: () => ({ width: 10 }), quads: () => [] },
			size: () => [ 16, 16 ]
		}),
		out
	);
	return out.paths;
}

test("the shut map demands every image the open window gates its admission on", () => {
	const cave = caveMapCatalog( caveCatalog() );
	const states = [
		{ name: "world page", state: { pose: WORLD } },
		{ name: "town page", state: { pose: { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 } } },
		{ name: "dungeon floor", state: { pose: DUNGEON, caveMaps: cave } },
		{ name: "Field drawer", state: { pose: WORLD, drawer: true, tab: "area" } },
		{ name: "Monster drawer", state: { pose: WORLD, drawer: true, tab: "monster" } },
		{ name: "dungeon drawer", state: { pose: DUNGEON, caveMaps: cave, drawer: true, tab: "monster" } }
	];
	for ( const { name, state } of states ) {
		const shut = presentFrame( { ...state, panel: "" } );
		const open = presentFrame( { ...state, panel: "Map" } );
		const demanded = new Set( shut.paths );
		assert.ok( open.gated.length > 0, name + ": the window drew its chrome" );
		assert.deepEqual(
			open.gated.filter( path => !demanded.has( path ) ),
			[],
			name + ": every gated image is demanded by the map while it is shut"
		);
	}
	// The scrollbar buttons' hover face: the only art the window draws on a
	// pointer state. Its press face comes through the tree painter alone.
	const drawer = { pose: WORLD, drawer: true, tab: "monster" };
	const hovered = presentFrame( { ...drawer, panel: "Map", hover: GUIDE_SCROLL_THUMB } );
	const demanded = new Set( presentFrame( { ...drawer, panel: "" } ).paths );
	const faces = ( paths, face ) => paths.filter( path => path.endsWith( face ) );
	assert.ok( faces( hovered.gated, "_focus.png" ).length > 0, "the hovered scrollbar button draws its face" );
	const pressed = treeArt( GUIDE_SCROLL_THUMB );
	assert.ok( faces( pressed, "_press.png" ).length > 0, "the pressed scrollbar button draws its face" );
	assert.deepEqual(
		[ ...faces( hovered.gated, "_focus.png" ), ...faces( pressed, "_press.png" ) ].filter( path =>
			!demanded.has( path )
		),
		[],
		"every scrollbar face is demanded by the map while it is shut"
	);
});
