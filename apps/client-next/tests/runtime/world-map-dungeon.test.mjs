/*
===========================================================================

world-map-dungeon.test.mjs - the M map inside a dungeon keeps its page

5758D0 probes town pages 1..5 for the local player's point and returns 0
when none holds it; 57FE60 then leaves the loaded page and the stored
page untouched. A cave region therefore never derives a page: the window
shows the page it already had. 57A570 then recentres on the dungeon's map
point (575660): Donwhang Stone Cave and the Ch'in Tomb each have one. The
port used to centre on cave coordinates outside the overview box, which
clamped the view onto the Europe edge.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );
const { worldMapDungeonPose } = await import( "../../src/engine/foundation/ui/world-map.ts" );

// Jangan town (page 1): sector 168 x 97 sits inside its authored bounds.
const JANGAN_POSE = { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 };
// Ch'in Tomb B1: dungeon region 0x8007, no page on the world map.
const CAVE_POSE = { regionId: 0x8007, x: 960, y: 0, z: 960, angle: 0 };

/*
================
fixture

One map HUD presented once per pose, in order; returns the window
caption and the painted quad textures of the last presentation.
================
*/
/**
 * @param {object[]} poses
 * @returns {{ caption: string, textures: string[] }}
 */
function present( poses ) {
	const hud = createWorldMapHud( {
		assets: { available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		base: "http://fixture.invalid",
		experimental: { state: () => ({ saved: experimentalOptions() }), restore: () => {} }
	} );
	/** @type {{ caption: string }} */
	const seen = { caption: "" };
	/** @type {any} */
	let quads = [];
	for ( const pose of poses ) {
		quads = [];
		/** @type {any} */
		const input = {
			next: { entities: [] },
			game: { pose, social: { self: 1, localName: "Me", members: [] } },
			pose,
			panel: "Map",
			w: 1200,
			h: 900,
			full: [ 0, 0, 1200, 900 ],
			hover: null,
			hudData: { map: {} },
			trackedQuest: 0,
			minimapResources: { positions: () => undefined },
			mapTeleport: { view: () => {}, viewCave: () => {}, hide: () => {} },
			reverseScrollHud: { mapping: () => false },
			text: {
				path: () => null,
				run: ( /** @type {string} */ value ) => ({ width: value.length * 6 }),
				extentHeight: () => 12,
				quads: () => []
			},
			resources: { has: () => true, size: () => undefined },
			paths: [],
			quads,
			controls: [],
			hudCopy: ( /** @type {string} */ k ) => k,
			beginWindow: () => [ 0, 0, 0, 0 ],
			endWindow: () => {},
			windowBox: ( /** @type {string} */ caption ) => {
				seen.caption = caption;
			},
			authoredButton: () => {},
			closeButton: () => {},
			image: () => {}
		};
		hud.present( input );
	}
	hud.dispose();
	return { caption: seen.caption, textures: quads.map( ( /** @type {any} */ q ) => q.texture ) };
}

test("the world map keeps its town page inside a dungeon", () => {
	const { caption, textures } = present( [ JANGAN_POSE, CAVE_POSE ] );
	// 57FE60 with a page-less point loads nothing: the Jangan page, its
	// caption and its picture stay exactly as they were. The caption is
	// 576949's local-map prefix plus the town string (#2276).
	assert.equal( caption, "UIIT_PAG_LOCALMAP - UIIT_STT_JANGAN" );
	assert.ok( textures.some( texture => texture.endsWith( "map_jangan.png" ) ) );
	assert.ok( !textures.some( texture => texture.includes( "map_world_" ) ) );
});

test("a world map first opened inside a dungeon derives no town page", () => {
	const { caption, textures } = present( [ CAVE_POSE ] );
	// The page field starts at 0 (the overview record): nothing is
	// derived from the cave, so no town page appears either.
	assert.equal( caption, "UIIT_PAG_WORLDMAP" );
	assert.ok(
		!textures.some( texture => /map_(jangan|donhwang|khotan|samarkand|constantinople)\.png/.test( texture ) )
	);
});

/*
================
overviewTiles

The overview tiles a pose's map shows, without the sign drawn over them.
================
*/
/**
 * @param {object} pose
 * @returns {string[]}
 */
function overviewTiles( pose ) {
	return present( [ pose ] ).textures.filter( texture => texture.includes( "map_world_" ) ).sort();
}

test("each dungeon has the map point 575660 gives it", () => {
	// 7200, 2102 and 2465, 2693 from sectors 0x87 and 0x5C, 192 units each.
	assert.deepEqual( worldMapDungeonPose( 0x8007 ), { regionId: (102 << 8) | 172, x: 960, y: 0, z: 1820, angle: 0 } );
	assert.deepEqual( worldMapDungeonPose( 0x8002 ), worldMapDungeonPose( 0x8007 ) );
	assert.deepEqual( worldMapDungeonPose( 0x8001 ), { regionId: (106 << 8) | 147, x: 1610, y: 0, z: 50, angle: 0 } );
	assert.equal( worldMapDungeonPose( 0x8008 ), null );
	assert.equal( worldMapDungeonPose( JANGAN_POSE.regionId ), null );
});

test("the overview centres on the dungeon's map point", () => {
	for ( const regionId of [ 0x8001, 0x8007 ] ) {
		const tiles = overviewTiles( { ...CAVE_POSE, regionId } );
		assert.ok( tiles.length > 0 );
		assert.deepEqual( tiles, overviewTiles( worldMapDungeonPose( regionId ) ) );
	}
	assert.notDeepEqual( overviewTiles( CAVE_POSE ), overviewTiles( { ...CAVE_POSE, regionId: 0x8001 } ) );
});
