/*
===========================================================================

pml-layout.test.mjs - PML text-resource fitting and published PML geometry

Exercise the production layout and font owner. Expected line boundaries pin
the owner-approved word-wrap deviation (2026-10-13); native v1.150 wraps per
character. Native screenshots are still required for a complete pixel comparison.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { guideTokens, guideContent } = await import( "../../src/engine/foundation/ui/guide-content.ts" );
const { decodeUiFont } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
/** @type {import("../../src/engine/contracts/ui.ts").UiQuad["color"]} */
const WHITE = [ 1, 1, 1, 1 ];

/*
================
published
================
*/
function published( path ) {
	return JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/" + path, "utf8" ) );
}

const atlas = decodeUiFont( published( "fonts/native-ui-font-atlas.json" ) );
const copy = published( "text/textuisystem.en.json" ).entries;
const charsByUv = new Map(
	Object.values( atlas.fonts ).flatMap( font =>
		Object.entries( font.glyphs ).map( (
			[code, glyph]
		) => [
			glyph.x / atlas.atlasWidth + "," + glyph.y / atlas.atlasHeight,
			String.fromCodePoint( Number( code ) )
		] )
	)
);

/*
================
draw
================
*/
function draw( source, width, font = atlas ) {
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const rect = [ 0, 0, width, 1000 ];
	return guideContent( font, guideTokens( source ), rect, rect, WHITE, () => undefined );
}

/*
================
lines

Read the glyph masks and line origins the production renderer publishes.
================
*/
function lines( result ) {
	const rows = new Map();
	for ( const quad of result.quads ) {
		assert.ok( quad.run && quad.textLayout );
		const y = quad.textLayout.box[1];
		const text = quad.run.glyphs.map( glyph => {
			const value = charsByUv.get( glyph.uv[0] + "," + glyph.uv[1] );
			assert.notEqual( value, undefined );
			return value;
		} ).join( "" );
		rows.set( y, (rows.get( y ) ?? "") + text );
	}
	return Array.from( rows, ( [y, value] ) => ({ y, value }) );
}

test("PML fits trailing spaces, long words and style continuations as text resources", () => {
	const glyph = { x: 0, y: 0, width: 4, height: 10, originX: 0, originY: 10, advanceX: 4 };
	const font = { recordHeight: 10, ascent: 10, descent: 0, glyphs: { 63: glyph } };
	const bold = { ...font, glyphs: { 63: { ...glyph, width: 6, advanceX: 6 } } };
	const fixture = {
		image: "/assets/fonts/test.png",
		atlasWidth: 16,
		atlasHeight: 16,
		fonts: { 0: font, "0:2": bold }
	};
	/** @type {[string, number, number[][]][]} */
	const cases = [
		[ "A B ", 12, [ [ 0, 0 ], [ 4, 0 ], [ 0, 15 ], [ 4, 15 ] ] ],
		[ "ABCDE", 10, [ [ 0, 0 ], [ 4, 0 ], [ 0, 15 ], [ 4, 15 ], [ 0, 30 ] ] ],
		[ "<strong>A</strong>BC", 12, [ [ 0, 0 ], [ 6, 0 ], [ 0, 15 ] ] ],
		// A break out of an empty line advances by its margin alone: the
		// native line height is 0 until a run lands on it (840190 case 3).
		[ "<br> A", 12, [ [ 0, 5 ], [ 4, 5 ] ] ]
	];
	for ( const [source, width, expected] of cases ) {
		const result = draw( source, width, fixture );
		assert.deepEqual( expandTextRuns( result.quads ).map( quad => quad.rect.slice( 0, 2 ) ), expected, source );
	}
});

test("PML runs bottom-align against taller images; block alignment and VAlign shift the block", () => {
	const glyph = { x: 0, y: 0, width: 4, height: 10, originX: 0, originY: 10, advanceX: 4 };
	const font = { recordHeight: 10, ascent: 10, descent: 0, glyphs: { 63: glyph } };
	const fixture = {
		image: "/assets/fonts/test.png",
		atlasWidth: 16,
		atlasHeight: 16,
		fonts: { 0: font, "0:2": font }
	};
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const rect = [ 0, 0, 100, 100 ];
	// One line: text (styled height 10) beside a 20x30 image. The native
	// line is 30 tall and every item sits on its bottom edge (83D000).
	const mixed = guideContent(
		fixture,
		guideTokens( 'A<img src="interface\\test.ddj">B' ),
		rect,
		rect,
		WHITE,
		() => [ 20, 30 ]
	);
	assert.equal( mixed.height, 35 );
	assert.deepEqual(
		expandTextRuns( mixed.quads ).map( quad => quad.rect.slice( 0, 2 ) ),
		[ [ 0, 20 ], [ 4, 0 ], [ 24, 20 ] ]
	);
	assert.deepEqual( mixed.quads.find( quad => quad.texture?.includes( "Media_extracted" ) )?.rect, [ 4, 0, 20, 30 ] );
	// The authored HAlign seeds the first line (535B84): the 8-wide line
	// centers in 100.
	const centered = guideContent( fixture, guideTokens( "AB" ), rect, rect, WHITE, () => undefined, {
		align: "center",
		vAlign: 0
	} );
	assert.deepEqual(
		expandTextRuns( centered.quads ).map( quad => quad.rect.slice( 0, 2 ) ),
		[ [ 46, 0 ], [ 50, 0 ] ]
	);
	// VAlign shifts the whole 15-tall block inside the 100-tall rect,
	// halved for the middle (535C21..535C85); it never changes the height.
	for ( const [vAlign, y] of [ [ 1, 42 ], [ 2, 85 ] ] ) {
		const shifted = guideContent( fixture, guideTokens( "A" ), rect, rect, WHITE, () => undefined, {
			align: "left",
			vAlign
		} );
		assert.equal( shifted.height, 15 );
		assert.deepEqual( expandTextRuns( shifted.quads ).map( quad => quad.rect.slice( 0, 2 ) ), [ [ 0, y ] ] );
	}
});

test("Alchemy processing rolls back an overflowing separator; Att.Grant keeps its native inline placement", () => {
	const node = decodeAuthoredLayout( published( "cif/layouts/ifalchemybox.json" ) ).GDR_ALCHEMYBOX_PML_TEXT;
	assert.ok( node );
	const processing = lines( draw( copy.UIIT_STT_ALCHEMYBOX_MATERIAL_PROCESSING_TEXT, node.rect[2] ) );
	assert.deepEqual( processing, [
		{ y: 0, value: "Material Processingis the disjointing or fusing of " },
		{ y: 20, value: "an item to create alchemy item." }
	] );
	assert.deepEqual( lines( draw( copy.UIIT_STT_ALCHEMYBOX_REINFORCE_ATTR_TEXT, node.rect[2] ) ), [
		{ y: 0, value: "Att.Grant The usage of a certain alchemy items that " },
		{ y: 20, value: "grant attributes or change basic stats of your " },
		{ y: 40, value: "equipment items." }
	] );
});

test("Auto Potion and Item Mall retain authored widths, break pitch and paragraph margins", () => {
	const potion = decodeAuthoredLayout( published( "cif/layouts/ifautopotion.json" ) ).GDR_AUTO_POTION_DESC_PML;
	assert.ok( potion );
	assert.deepEqual( lines( draw( copy[potion.text], potion.rect[2] ) ), [
		{ y: 0, value: "Auto recovery settings" },
		{ y: 20, value: "Set the % of the HP/MP gage to the preferred value, then " },
		{ y: 40, value: "auto potion recovery will be in use. To not use auto " },
		{ y: 60, value: "recovery for a certain category, remove the check from " },
		{ y: 80, value: "the check box." }
	] );
	const mall = decodeAuthoredLayout( published( "cif/layouts/ifitemmallshop.json" ) ).GDR_ITEM_MALL_SHOP_MAIN_PML;
	assert.ok( mall );
	const result = draw( copy.UIIT_STT_SILKMALL_MAIN_EXPLAIN, mall.rect[2] );
	const rows = lines( result );
	assert.deepEqual( rows.slice( 0, 4 ), [
		{ y: 0, value: "What is the Item Mall?" },
		{ y: 20, value: "The Item Mall sells and rents equipments that can be" },
		{ y: 40, value: "used in Silkroad Online. Items can be bought through the use of " },
		{ y: 60, value: "silk." }
	] );
	assert.ok( rows.some( row => row.y === 80 && row.value === "Buying silk" ) );
	assert.ok( rows.some( row => row.value.startsWith( "Bookmark(Currently" ) ) );
});
