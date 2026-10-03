/*
===========================================================================

native-map-actions.test.mjs - tests for world-map.ts, action-layout.ts,
minimap-markers.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const asset = p => JSON.parse( readFileSync( "../../.generated/client-public/assets/" + p, "utf8" ) );
const { decodeMapLabels, decodeMapIcons, worldMapPresentation } = await import(
	"../../src/engine/foundation/ui/world-map.ts"
);
const { mapLabelVisible } = await import( "../../src/engine/foundation/ui/world-map.ts" );
const { decodeActionSlots } = await import( "../../src/engine/foundation/ui/action-layout.ts" );
const { rosterPositions } = await import( "../../src/engine/foundation/ui/minimap-markers.ts" );
test("published map labels resolve and share the scrolling tile origin", () => {
	const labels = decodeMapLabels(
		asset( "data/worldmap-localinfo.json" ),
		asset( "text/textdataname.en.json" ).entries
	);
	assert.ok( labels.length > 50 );
	const label = labels.find( r => r.page === 1 && r.x === 413 && r.y === 459 );
	assert.ok( label?.text );
	const p = { regionId: 0x62a8, x: 0, y: 0, z: 0, angle: 0 }, clip = [ 10, 20, 640, 384 ];
	const q = worldMapPresentation( p, 1, clip, [ 0, 0 ], p, labels );
	const got = q.labels.find( r => r.label === label );
	assert.equal( defined( got ).x, q.background[0].rect[0] + 413 );
	assert.equal( defined( got ).y, q.background[0].rect[1] + 459 );
	assert.ok( q.labels.every( r => r.label.page === 1 ) );
});
test("retail action records occupy the authored group/index slots", () => {
	const rows = decodeActionSlots( asset( "data/actionwnddata.json" ) ), layout = asset( "cif/layouts/ifaction.json" );
	for ( const row of rows ) {
		assert.ok(
			Object.values( layout.controlsByName ).some( n => n.type === "CIFSlotWithHelp" && n.id === row.slot )
		);
		assert.ok(
			existsSync(
				"../../.generated/client-public/assets/images/Media_extracted/icon/" +
					row.icon.replace( /\.ddj$/i, ".png" )
			),
			row.icon
		);
	}
	assert.equal( defined( rows.find( r => r.id === 1000 ) ).slot, 100 );
	assert.equal( defined( rows.find( r => r.id === 4000 ) ).slot, 400 );
	assert.equal( defined( rows.find( r => r.id === 5000 ) ).slot, 407 );
	assert.throws( () => decodeActionSlots( { rows: [ "1\t1\tname\tname\ticon\t2\t8" ] } ), /binding/ );
});
test("world map drag keeps the clamped position so reversing moves at once (579920)", () => {
	const p = { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 }, clip = [ 0, 0, 640, 384 ];
	const edge = worldMapPresentation( p, 1, clip, [ 5000, 0 ], p );
	assert.equal( edge.background[0].rect[0], 0, "overshoot pins the left edge" );
	const back = worldMapPresentation( p, 1, clip, [ edge.pan[0] - 10, edge.pan[1] ], p );
	assert.equal( back.background[0].rect[0], -10, "the stored pan has no overshoot to unwind" );
	// Opening Jangan from Donhwang centres far off the page (57A570 clamps it);
	// the returned pan is the pinned edge, so the first drag step moves the map.
	const far = { regionId: 101 * 256 + 152, x: 0, y: 0, z: 0, angle: 0 },
		pinned = worldMapPresentation( p, 1, clip, [ 0, 0 ], far );
	assert.equal( pinned.background[0].rect[0], 0 );
	const moved = worldMapPresentation( p, 1, clip, [ pinned.pan[0] - 10, pinned.pan[1] ], far );
	assert.equal( moved.background[0].rect[0], -10 );
});
test("world map admits native town and fortress icons and clips town click areas", () => {
	const icons = decodeMapIcons( asset( "data/worldmap-localinfo.json" ) ),
		town = icons.find( i => i.destination === 1 && i.page === 0 );
	assert.equal( defined( town ).x, (167 - 66) * 32 + 16 );
	assert.equal( defined( town ).y, (113 - 98) * 32 + 2 );
	assert.ok( icons.some( i => i.path.includes( "/icon/npc/fortress_manager.png" ) ) );
	const p = { regionId: 0x62a8, x: 0, y: 0, z: 0, angle: 0 };
	const q = worldMapPresentation( p, 0, [ 0, 0, 640, 384 ], [ 0, 0 ], p, [], icons );
	assert.ok( q.overlay.some( q => q.texture.endsWith( "city_jangan.png" ) ) );
	assert.ok( q.hits.some( h => h.icon.destination === 1 ) );
	assert.ok( q.hits.every( ( { rect: r } ) => r[0] >= 0 && r[1] >= 0 && r[0] + r[2] <= 640 && r[1] + r[3] <= 384 ) );
});
test("world map paints in 57FE60 order and projects every marker like the player arrow", () => {
	const icons = decodeMapIcons( asset( "data/worldmap-localinfo.json" ) ).filter( i => i.page === 1 );
	const p = { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 }, clip = [ 0, 0, 640, 384 ];
	const sign = "/assets/images/Media_extracted/interface/worldmap/wmap_sign_party.png";
	const q = worldMapPresentation( p, 1, clip, [ 0, 0 ], p, [], icons, [ {
		regionId: p.regionId,
		x: p.x,
		z: p.z,
		rotation: 0,
		path: sign
	} ] );
	// 57FE60: page bitmap, then the 6200 overlay, then every marker pass; 57CE80
	// draws mm_sign_character last so no icon can cover the arrow.
	assert.equal( q.background.length, 1 );
	assert.ok(
		q.overlay.length > 0 && q.overlay.every( r => r.texture.includes( "/xy_" ) || r.texture.includes( "/icon/" ) )
	);
	assert.equal(
		defined( q.markers.at( -1 ) ).texture,
		"/assets/images/Media_extracted/interface/minimap/mm_sign_character.png"
	);
	// A marker standing exactly where the player stands must land on the arrow:
	// 57B1C0/57B550/57CE80 reuse the 575660 projection verbatim.
	assert.equal( q.markers.length, 2 );
	assert.deepEqual( q.markers[0].rect, defined( q.markers.at( -1 ) ).rect );
	assert.deepEqual( q.markers[0].rect.slice( 2 ), [ 16, 16 ], "16x16 centred, native corners -8/+8" );
	// Rotation rides through (hunting points are the only rotated caller).
	const spun = worldMapPresentation( p, 1, clip, [ 0, 0 ], p, [], [], [ {
		regionId: p.regionId,
		x: p.x,
		z: p.z,
		rotation: 1.25,
		path: sign
	} ] );
	assert.equal( spun.markers[0].rotation, 1.25 );
	// A dungeon region has no page projection, the same guard the arrow uses.
	const below = worldMapPresentation( p, 1, clip, [ 0, 0 ], p, [], [], [ {
		regionId: 0x8001,
		x: 0,
		z: 0,
		rotation: 0,
		path: sign
	} ] );
	assert.equal( below.markers.length, 1 );
});
test("both map surfaces share one roster admission and prefer a live pose", () => {
	const local = { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 };
	const game = {
		fortress: { worldId: 0x10001 },
		social: {
			localName: "me",
			self: 1,
			members: [
				{ id: 1, name: "me", region: local.regionId, x: 0, y: 0, z: 0, war: 0x10001 },
				{ id: 2, name: "mate", region: local.regionId, x: 100, y: 0, z: 200, war: 0x10001 },
				{ id: 3, name: "elsewhere", region: local.regionId, x: 5, y: 0, z: 5, war: 7 }
			]
		}
	};
	assert.deepEqual( rosterPositions( local, game, [] ), [ {
		kind: "party",
		regionId: local.regionId,
		x: 100,
		y: 0,
		z: 200
	} ], "self and other-war rows are refused" );
	const live = [ { gid: 9, kind: "player", name: "mate", regionId: local.regionId, x: 700, y: 1, z: 800 } ];
	assert.deepEqual( rosterPositions( local, game, live ), [ {
		kind: "party",
		regionId: local.regionId,
		x: 700,
		y: 1,
		z: 800
	} ], "a visible member beats the roster broadcast" );
});
test("macro world map resolves and projects town labels on page 0", () => {
	const labels = decodeMapLabels(
		asset( "data/worldmap-localinfo.json" ),
		asset( "text/textdataname.en.json" ).entries
	);
	const icons = decodeMapIcons( asset( "data/worldmap-localinfo.json" ) );
	const p = { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 }, clip = [ 0, 0, 640, 384 ];
	const q = worldMapPresentation( p, 0, clip, [ 0, 0 ], p, labels, icons );
	const townNames = [ "Jangan", "Hotan", "Donwhang", "Constantinople", "Samarkand" ];
	for ( const name of townNames ) {
		const found = q.labels.find( l => l.label.text === name );
		assert.ok( found, `town label ${name} must exist on page 0` );
		assert.equal( found.label.page, 0 );
	}
	const jangan = q.labels.find( l => l.label.text === "Jangan" );
	const janganIcon = q.hits.find( h => h.icon.destination === 1 );
	assert.ok( janganIcon, "Jangan town icon hit must exist on page 0" );
	// Jangan label and icon must coincide geographically
	assert.ok( Math.abs( defined( jangan ).x - (janganIcon.rect[0] + janganIcon.rect[2] / 2) ) < 40 );
	assert.ok( Math.abs( defined( jangan ).y - (janganIcon.rect[1] + janganIcon.rect[3] / 2) ) < 40 );
});

test("only labels reaching the map window are laid out", () => {
	const labels = decodeMapLabels(
		asset( "data/worldmap-localinfo.json" ),
		asset( "text/textdataname.en.json" ).entries
	);
	const p = { regionId: 0x62a8, x: 0, y: 0, z: 0, angle: 0 }, clip = [ 10, 20, 640, 384 ];
	const projected = worldMapPresentation( p, 1, clip, [ 0, 0 ], p, labels ).labels;
	const shown = projected.filter( row => mapLabelVisible( [ row.x - 40, row.y, 80, 12 ], clip ) );
	assert.ok( shown.length > 0 && shown.length < projected.length, `${shown.length} of ${projected.length}` );
	// The ink margin keeps a label just past the edge, whose glyphs can still show.
	assert.equal( mapLabelVisible( [ 0, 0, 10, 10 ], [ 25, 0, 100, 100 ] ), true );
	assert.equal( mapLabelVisible( [ 0, 0, 10, 10 ], [ 27, 0, 100, 100 ] ), false );
	assert.equal( mapLabelVisible( [ 200, 50, 10, 10 ], [ 0, 0, 100, 100 ] ), false );
	assert.equal( mapLabelVisible( [ 50, 50, 10, 10 ], [ 0, 0, 100, 100 ] ), true );
});
