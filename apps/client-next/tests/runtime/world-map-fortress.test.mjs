/*
===========================================================================

world-map-fortress.test.mjs - the fortress page the world map opens (#3513)

v1.150's worldmap_mapinfo.txt row 1001 is the Jangan fortress page
(map_fort_jangan, world INS_FORT_JA), and localinfo rows 91006-91008 are
the fort_worldmap icons on the world page that open it, as a city's icon
opens its town. 576949 captions it UIIT_PAG_LOCALMAP, the separator, then
SN_FORTRESS_JANGAN. 5758D0 picks a page by position from towns 1..5 only,
so the fortress page is never picked so.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );
const { decodeMapIcons, worldMapImagePaths, worldMapPageAt, worldMapPages } = await import(
	"../../src/engine/foundation/ui/world-map.ts"
);

const COPY = { UIIT_PAG_WORLDMAP: "Entire map", UIIT_PAG_LOCALMAP: "Regional map", UIIT_STT_JANGAN: "Jangan" };
const STRINGS = { SN_FORTRESS_JANGAN: "Jangan Fortress" };
// localinfo 91006, one of the three fort_worldmap icons (destination 1001).
const FORT_ICON = [
	"91006",
	"2",
	"interface\\worldmap\\map\\fort_worldmap.ddj",
	"",
	"",
	"",
	"1001",
	"0",
	"167",
	"93",
	"23",
	"18",
	"24",
	"44",
	"0",
	"0",
	"0",
	"0",
	"0",
	"1"
];
// Standing beside the icon, on the world page.
const POSE = { regionId: 93 * 256 + 167, x: 230, y: 0, z: 1200, angle: 0 };

/*
================
mapHud

A map window over the fixture icons; frame() presents one frame and returns
its caption and controls.
================
*/
function mapHud( iconRows = [ FORT_ICON ] ) {
	const hud = createWorldMapHud( {
		assets: { available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		base: "http://fixture.invalid",
		experimental: { state: () => ({ saved: experimentalOptions() }), restore: () => {} }
	} );
	const mapIcons = decodeMapIcons( { rows: iconRows } );
	const frame = () => {
		let caption = "";
		const controls = [];
		hud.present(
			/** @type {any} */ ({
				next: { entities: [] },
				game: { pose: POSE },
				pose: POSE,
				panel: "Map",
				w: 1200,
				h: 900,
				full: [ 0, 0, 1200, 900 ],
				hover: null,
				hudData: { map: {}, mapIcons, mapLabels: [], strings: STRINGS },
				trackedQuest: 0,
				minimapResources: { positions: () => undefined },
				mapTeleport: { view: () => {}, viewCave: () => {}, hide: () => {} },
				reverseScrollHud: { mapping: () => false },
				text: {
					path: () => null,
					run: ( value ) => ({ width: value.length * 6 }),
					extentHeight: () => 12,
					quads: () => []
				},
				resources: { has: () => true, size: () => undefined },
				paths: [],
				quads: [],
				controls,
				hudCopy: ( key ) => COPY[key] ?? "",
				beginWindow: () => [ 0, 0, 0, 0 ],
				endWindow: () => {},
				windowBox: ( title ) => {
					caption = title;
				},
				authoredButton: () => {},
				closeButton: () => {},
				image: () => {}
			})
		);
		return { caption, controls };
	};
	return { hud, frame };
}

test("the fortress icon opens the Jangan fortress page, captioned by its object text", () => {
	const { hud, frame } = mapHud();
	try {
		const world = frame();
		assert.equal( world.caption, "Entire map" );
		const icon = world.controls.find( row => row.id === "map-town:1001" );
		assert.ok( icon, "the fort_worldmap icon is a hit target" );
		assert.equal( icon.label, "Jangan Fortress" );
		hud.activate( "map-town:1001", POSE );
		assert.equal( frame().caption, "Regional map - Jangan Fortress" );
	} finally {
		hud.dispose();
	}
});

test("the fortress page is drawable but never picked by position", () => {
	const page = worldMapPages().find( row => row.id === 1001 );
	assert.ok( page );
	assert.ok( worldMapImagePaths().some( path => path.endsWith( "map_fort_jangan.png" ) ) );
	// Inside the fortress's own regions (69..74 x 71..67) the window keeps
	// its position pick to the towns, as 5758D0 probes towns 1..5.
	assert.equal( worldMapPageAt( { regionId: 70 * 256 + 71, x: 900, y: 0, z: 900, angle: 0 } ), 0 );
	// A town still picks itself.
	assert.equal( worldMapPageAt( { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 } ), 1 );
});

test("the three fortress icons are three distinct controls that each open the fortress page", () => {
	// localinfo 91006-91008 all open 1001; one identity per page made the
	// UI refuse the frame ("Duplicate UI control identity: map-town:1001").
	const rows = [ "91006", "91007", "91008" ].map( id => [ id, ...FORT_ICON.slice( 1 ) ] );
	const { hud, frame } = mapHud( rows );
	try {
		const ids = frame().controls.map( row => row.id ).filter( id => id.startsWith( "map-town:" ) );
		assert.equal( ids.length, 3 );
		assert.equal( new Set( ids ).size, 3 );
		for ( const id of ids ) {
			hud.activate( "map-town:1", POSE );
			hud.activate( id, POSE );
			assert.equal( frame().caption, "Regional map - Jangan Fortress", id );
		}
	} finally {
		hud.dispose();
	}
});
