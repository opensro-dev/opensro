/*
===========================================================================

world-map-dungeon.test.mjs - the M map inside a dungeon keeps its page

5758D0 probes town pages 1..5 for the local player's point and returns 0
when none holds it; 57FE60 then leaves the loaded page and the stored
scroll untouched (no LoadMapPage, no recentre). A cave region therefore
never derives a page: the window shows the page it already had. The port
used to re-derive the page from the cave pose every frame, land on the
overview and centre it on cave sector coordinates outside the overview
box, which clamped the view onto the Europe edge.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

// The authored automatic-move button sits near the 652-wide window's right edge.
const AUTO_MOVE_RECT = [ 560, 8, 80, 22 ];
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
			hudData: { map: { GDR_WM_BTN_AUTO_MOVE: { rect: AUTO_MOVE_RECT } } },
			trackedQuest: 0,
			minimapResources: { positions: () => undefined },
			mapTeleport: { view: () => {} },
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
			authoredLabeledButton: () => {},
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
	// caption and its picture stay exactly as they were.
	assert.equal( caption, "UIIT_STT_JANGAN" );
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
