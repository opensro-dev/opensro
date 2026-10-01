/*
===========================================================================

guide-pml.test.mjs - native paragraph scopes and safe combined font attributes

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { guideTokens, guideContent } = await import( "../../src/engine/foundation/ui/guide-content.ts" );
const { tooltipDescription } = await import( "../../src/engine/foundation/ui/tooltip-description.ts" );

test("paragraph margins restore across nested scopes and explicit line breaks", () => {
	const tokens = guideTokens( '<sml2><p line_margin="12">A<br><p line_margin="5">B</p>C</p>D</sml2>' );
	const glyph = { x: 0, y: 0, width: 4, height: 10, originX: 0, originY: 10, advanceX: 4 };
	const font = { recordHeight: 10, ascent: 10, descent: 0, glyphs: { 63: glyph } };
	const atlas = { image: "/assets/fonts/test.png", atlasWidth: 16, atlasHeight: 16, fonts: { 0: font, "0:2": font } };
	const result = guideContent(
		atlas,
		tokens,
		[ 0, 0, 100, 100 ],
		[ 0, 0, 100, 100 ],
		[ 1, 1, 1, 1 ],
		() => undefined
	);
	assert.deepEqual( result.quads.map( quad => quad.rect[1] ), [ 0, 22, 37, 59 ] );
	assert.deepEqual( tooltipDescription( '<sml2><p line_margin="5">A</p><p>B</p></sml2>' ).map( row => row.value ), [
		"A",
		"B"
	] );
});

test("font color and bold attributes share scoped native state", () => {
	const text = guideTokens( '<font style="bold" color="255,255,153,81">A<font line_margin="12">B</font></font>C' )
		.filter( token => token.kind === "text" );
	assert.deepEqual( text.map( token => token.strong ), [ true, true, false ] );
	assert.deepEqual( text[0].color, [ 1, 153 / 255, 81 / 255, 1 ] );
	assert.equal( text[2].color, null );
});

test("native markup refuses script attributes, escaped paths and malformed scopes", () => {
	for (
		const source of [
			'<p line_margin="999999999999999999">X</p>',
			"<p>X",
			"</p>",
			'<font color="255,1,2,999">X</font>',
			'<font color="255,1,2,3" color="255,3,2,1">X</font>',
			'<font onclick="alert(1)">X</font>',
			"<script>alert(1)</script>",
			'<img src="https://example.com/test.ddj">',
			'<img src="interface\\..\\secret.ddj">'
		]
	) assert.throws( () => guideTokens( source ), source );
});
