/*
===========================================================================

region-banner.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const load = async path => {
	return import( sourceFileUrl( path ).href );
};
const { createRegionBanner } = await load( "src/engine/runtime/ui/hud/region-banner.ts" );
const { regionBannerText, regionBannerQuads } = await load( "src/engine/foundation/ui/region-banner.ts" );
const codes = { 1: "RN_A", 2: "RN_A", 3: "RN_B", 4: "xxx" },
	zones = {
		RN_A_01: "Jangan",
		RN_A_02: "City",
		RN_A_03: "xxx",
		RN_B_01: "Field",
		RN_B_02: "Outskirts",
		RN_B_03: "Monster level 1–3"
	};
test("authored level line and missing region keys never infer nearby monster levels", () => {
	assert.equal( regionBannerText( 4, codes, zones ), null );
	assert.equal( regionBannerText( 5, codes, zones ), null );
	assert.equal( regionBannerText( 1, codes, zones ).level, "" );
	assert.equal( regionBannerText( 3, codes, zones ).level, "Monster level 1–3" );
});
test("same named region does not restart, hold is idle, fade finishes and world exit resets", () => {
	const b = createRegionBanner();
	b.step( 1, codes, zones, 0 );
	assert.equal( b.alpha(), 0 );
	b.step( 2, codes, zones, 250 );
	assert.equal( b.alpha(), 128 / 255 );
	b.step( 1, codes, zones, 500 );
	assert.equal( b.alpha(), 1 );
	assert.equal( b.step( 2, codes, zones, 2000 ), false );
	b.step( 2, codes, zones, 3250 );
	assert.equal( b.alpha(), 128 / 255 );
	b.step( 1, codes, zones, 3500 );
	assert.equal( b.alpha(), 0 );
	assert.equal( b.step( 1, codes, zones, 5000 ), false );
	b.step( undefined, codes, zones, 6000 );
	assert.equal( b.value(), null );
	b.step( 1, codes, zones, 7000 );
	b.step( 1, codes, zones, 7500 );
	assert.equal( b.alpha(), 1 );
	b.step( 4, codes, zones, 7600 );
	assert.equal( b.alpha(), 0 );
	assert.equal( b.value(), null );
});
test("native detail coordinates and font indices survive viewport resizing", () => {
	for ( const width of [ 800, 1920 ] ) {
		const calls = [];
		regionBannerQuads( regionBannerText( 3, codes, zones ), 1, width, 600, ( ...args ) => {
			calls.push( args );
			return [];
		}, 23 );
		assert.deepEqual( calls.map( c => c[4].fontIndex ), [ 4, 3, 1 ] );
		assert.deepEqual( calls[1][1], [ width / 2 - 343, 156, 691, 15 ] );
		assert.deepEqual( calls[2][1], [ width / 2 - 214, 177, 427, 13 ] );
	}
});

test("native region decoration shares text opacity, precedes glyphs and centers after resize", () => {
	const art = { texture: "/assets/images/Media_extracted/interface/game/area_deco.png", uv: [ 0, 0, 1, 1 ] };
	for ( const width of [ 800, 1200, 1920 ] ) {
		const quads = regionBannerQuads( regionBannerText( 3, codes, zones ), .5, width, 900, () => [], 23, art );
		assert.deepEqual( quads, [ {
			rect: [ Math.trunc( width / 2 ) - 372, 95, 744, 104 ],
			clip: [ 0, 0, width, 900 ],
			uv: art.uv,
			texture: art.texture,
			color: [ 1, 1, 1, .5 ]
		} ] );
		assert.deepEqual(
			regionBannerQuads( regionBannerText( 3, codes, zones ), 0, width, 900, () => [], 23, art ),
			[]
		);
	}
});

test("the 1.5x title scales its text runs: painted glyphs equal scaled glyph quads", async () => {
	const { readFile } = await import( "node:fs/promises" );
	const { decodeUiFont, titleText } = await load( "src/engine/foundation/rendering/ui-glyphs.ts" );
	const { expandTextRuns } = await load( "src/engine/foundation/rendering/text-run.ts" );
	const atlas = decodeUiFont(
		JSON.parse( await readFile( "../../.generated/client-public/assets/fonts/native-ui-font-atlas.json", "utf8" ) )
	);
	const runs = ( ...args ) => titleText( atlas, ...args ), glyphs = ( ...args ) => expandTextRuns( runs( ...args ) );
	for ( const width of [ 800, 1366, 1920 ] ) {
		const value = regionBannerText( 3, codes, zones );
		const painted = expandTextRuns( regionBannerQuads( value, 1, width, 900, runs, 23 ) ),
			expected = regionBannerQuads( value, 1, width, 900, glyphs, 23 );
		assert.ok( expected.length > 10 );
		assert.deepEqual( painted, expected );
		// The title glyphs really are 1.5x their font size.
		const first = expected[0], glyph = atlas.fonts["4"].glyphs[String( "F".codePointAt( 0 ) )];
		assert.equal( first.rect[2], glyph.width * 1.5 );
	}
});
