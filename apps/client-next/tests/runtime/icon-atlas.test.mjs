/*
===========================================================================

icon-atlas.test.mjs - the icon atlas manifest as a pure mapping

Port-only, not native. The build's manifest (scripts/build
/ui_icon_atlas.py) parses into the entry lookup the packer and the image
owner share: an icon's sheet and its uv rect, computed from the pixel
rect and the sheet's size. Anything malformed rejects the whole manifest
- the per-icon path is the fallback for every icon or none.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { iconAtlas } = await import( "../../src/engine/foundation/ui/icon-atlas.ts" );

const sheet = "/assets/ui-atlas/sheet-0.png";
const manifest = {
	version: 1,
	sheets: [ sheet ],
	sheetSize: [ [ 2048, 2047 ] ],
	icons: {
		"/assets/images/Media_extracted/icon/a.png": [ 0, 2, 2, 32, 32 ],
		"/assets/images/Media_extracted/icon/b.png": [ 0, 40, 2, 64, 128 ]
	}
};

test("a manifest maps each icon to its sheet and uv rect", () => {
	const atlas = iconAtlas( manifest );
	assert.ok( atlas );
	assert.deepEqual( atlas.sheets, [ sheet ] );
	assert.deepEqual( atlas.entry( "/assets/images/Media_extracted/icon/a.png" ), {
		sheet,
		uv: [ 2 / 2048, 2 / 2047, 32 / 2048, 32 / 2047 ]
	} );
	assert.deepEqual( atlas.entry( "/assets/images/Media_extracted/icon/b.png" ), {
		sheet,
		uv: [ 40 / 2048, 2 / 2047, 64 / 2048, 128 / 2047 ]
	} );
	assert.equal( atlas.entry( "/assets/images/Media_extracted/icon/absent.png" ), null );
	assert.equal( atlas.entry( "/assets/images/Media_extracted/interface/frame.png" ), null );
});

test("a malformed manifest maps nothing", () => {
	const bad = [
		null,
		{},
		{ ...manifest, version: 2 },
		{ ...manifest, sheets: [] },
		{ ...manifest, sheetSize: [] },
		{ ...manifest, sheetSize: [ [ 2048 ] ] },
		{ ...manifest, sheetSize: [ [ 2048, 0 ] ] },
		{ ...manifest, sheets: [ "/assets/elsewhere/sheet.png" ] },
		{ ...manifest, icons: null },
		{ ...manifest, icons: {} },
		{ ...manifest, icons: { "/a.png": [ 1, 2, 2, 32, 32 ] } },
		{ ...manifest, icons: { "/a.png": [ 0, 2, 2, 0, 32 ] } },
		{ ...manifest, icons: { "/a.png": [ 0, 2, 2, 32, 32.5 ] } },
		{ ...manifest, icons: { "/a.png": [ 0, -1, 2, 32, 32 ] } },
		{ ...manifest, icons: { "/a.png": [ 0, 2020, 2, 32, 32 ] } },
		{ ...manifest, icons: { "/a.png": [ 0, 2, 2, 32 ] } }
	];
	for ( const value of bad ) assert.equal( iconAtlas( value ), null, JSON.stringify( value ) );
});
