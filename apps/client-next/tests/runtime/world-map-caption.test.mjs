/*
===========================================================================

world-map-caption.test.mjs - the map window's mode caption (#2276)

Native CIFWorldMap_SetModeCaption (5768A0): mode 0 captions the window
UIIT_PAG_WORLDMAP ("Entire map"); a town page (mode 1, 576949) captions
it UIIT_PAG_LOCALMAP ("Regional map"), the wide separator at 0xBD959C
(space dash space), then the town's own string (UIIT_STT_JANGAN, ...).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

const COPY = {
	UIIT_PAG_WORLDMAP: "Entire map",
	UIIT_PAG_LOCALMAP: "Regional map",
	UIIT_STT_JANGAN: "Jangan"
};

/*
================
captionAt

Present one map frame at pose and return the window's caption.
================
*/
function captionAt( pose ) {
	const hud = createWorldMapHud( {
		assets: { available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		base: "http://fixture.invalid",
		experimental: { state: () => ({ saved: experimentalOptions() }), restore: () => {} }
	} );
	try {
		let caption = "";
		hud.present( /** @type {any} */ ({
			next: { entities: [] },
			game: { pose },
			pose,
			panel: "Map",
			w: 1200,
			h: 900,
			full: [ 0, 0, 1200, 900 ],
			hover: null,
			hudData: { map: {} },
			trackedQuest: 0,
			minimapResources: { positions: () => undefined },
			mapTeleport: { view: () => {} },
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
			controls: [],
			hudCopy: ( key ) => COPY[key] ?? "",
			beginWindow: () => [ 0, 0, 0, 0 ],
			endWindow: () => {},
			windowBox: ( title ) => {
				caption = title;
			},
			authoredButton: () => {},
			authoredLabeledButton: () => {},
			closeButton: () => {},
			image: () => {}
		}) );
		return caption;
	} finally {
		hud.dispose();
	}
}

test("the overview captions as the entire map, a town page as its regional map", () => {
	// Sector (160, 97): outside every town page, so the overview shows.
	assert.equal(
		captionAt( { regionId: 97 * 256 + 160, x: 960, y: 0, z: 960, angle: 0 } ),
		"Entire map"
	);
	// Sector (168, 97) with the in-page offset: the Jangan page (bounds
	// 166..170 x 96..99) shows, captioned with its own town string.
	assert.equal(
		captionAt( { regionId: 97 * 256 + 168, x: 960, y: 0, z: 960, angle: 0 } ),
		"Regional map - Jangan"
	);
});
