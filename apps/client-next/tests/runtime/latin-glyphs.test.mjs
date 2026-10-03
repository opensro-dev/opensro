/*
===========================================================================

latin-glyphs.test.mjs - published Latin coverage reaches the text renderer

Exercise every native font slot in normal and bold styles. Turkish and
neighboring Latin alphabets must use real atlas cells, never the question
mark fallback, while measurement and painting agree on glyph advances.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

const { titleText } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const atlas = JSON.parse(
	readFileSync( "../../.generated/client-public/assets/fonts/native-ui-font-atlas.json", "utf8" )
);
const LATIN_SAMPLE = "ÇçĞğİıÖöŞşÜüÀéñŁłŒœŽž";

test("extended Latin glyphs survive atlas admission and every native font style", () => {
	for ( const fontIndex of [ 0, 1, 2, 3, 4 ] ) {
		for ( const fontStyle of [ 0, 2 ] ) {
			const face = fontStyle === 2 ? atlas.fonts[fontIndex].styles["2"] : atlas.fonts[fontIndex];
			const effective = { ...atlas, fonts: { ...atlas.fonts, [fontIndex + ":2"]: face } };
			const quads = expandTextRuns(
				titleText( effective, LATIN_SAMPLE, [ 0, 0, 2000, 100 ], [ 0, 0, 2000, 100 ], [
					1,
					1,
					1,
					1
				], { fontIndex, fontStyle, vAlign: 0 } )
			);
			assert.equal( quads.length, LATIN_SAMPLE.length );
			for ( let index = 0; index < LATIN_SAMPLE.length; index++ ) {
				const code = LATIN_SAMPLE.codePointAt( index );
				assert.ok( code );
				const glyph = face.glyphs[code], quad = quads[index];
				assert.ok( glyph && quad, `font ${fontIndex}:${fontStyle}, U+${code.toString( 16 )}` );
				assert.notDeepEqual( [ glyph.x, glyph.y ], [ face.glyphs[63].x, face.glyphs[63].y ] );
				assert.deepEqual( quad.uv, [
					glyph.x / atlas.atlasWidth,
					glyph.y / atlas.atlasHeight,
					glyph.width / atlas.atlasWidth,
					glyph.height / atlas.atlasHeight
				] );
			}
		}
	}
});

test("Turkish chat retains Unicode code units through request, receipt and broadcast", () => {
	/** @type {import('../../src/engine/contracts/network').WireFrame[]} */
	const sent = [];
	const chat = createChat( frame => sent.push( frame ) );
	chat.bootstrap( { character: { name: "Player" } } );
	chat.request( 1, LATIN_SAMPLE, "", 0 );
	const request = sent[0];
	assert.ok( request );
	assert.equal( request.opcode, 0x7367 );
	assert.equal(
		new TextDecoder( "utf-16le", { fatal: true } ).decode( request.payload.subarray( 4 ) ),
		LATIN_SAMPLE
	);
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	const text = Buffer.from( LATIN_SAMPLE, "utf16le" );
	const broadcast = new Uint8Array( 7 + text.length ), bytes = new DataView( broadcast.buffer );
	broadcast[0] = 1;
	bytes.setUint32( 1, 2, true );
	bytes.setUint16( 5, LATIN_SAMPLE.length, true );
	broadcast.set( text, 7 );
	chat.receive( { opcode: 0x3667, payload: broadcast }, 1, "Peer" );
	assert.deepEqual( chat.state().lines.map( line => line.text ), [ LATIN_SAMPLE, LATIN_SAMPLE ] );
});
