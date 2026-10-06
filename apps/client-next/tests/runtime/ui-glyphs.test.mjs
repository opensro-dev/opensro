/*
===========================================================================

ui-glyphs.test.mjs - tests for ui-glyphs.ts, text.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";

const { titleText, titleTextBox, titleColoredText, resolveTextOverlaps } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/ui-glyphs.ts" ).href
);
const { expandTextRuns } = await import( sourceFileUrl( "src/engine/foundation/rendering/text-run.ts" ).href );
/*
================
glyphQuads

The glyph quads a laid-out line stands for: its text run expanded.
================
*/
const glyphQuads = ( ...args ) => expandTextRuns( titleText( ...args ) );

const atlas = JSON.parse(
	await readFile( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json", "utf8" )
);
const rect = [ 10, 20, 100, 41 ], clip = [ 0, 0, 200, 200 ], color = [ 1, 1, 1, 1 ];

test("UI measurement uses the same font slot as painting and isolates cached widths", async () => {
	const { createUiText } = await import( sourceFileUrl( "src/engine/runtime/ui/text/text.ts" ).href );
	const bytes = new TextEncoder().encode( JSON.stringify( atlas ) );
	const text = createUiText( {
		available: () => 1,
		request: () => 1,
		take: () => ({ kind: "bytes", buffer: bytes.buffer }),
		cancel: () => {}
	}, "http://fixture.invalid" );
	text.step();
	text.step();
	try {
		for ( const fontIndex of [ 0, 2, 0, 1, 2 ] ) {
			const value = "Jangan",
				font = atlas.fonts[String( fontIndex )],
				width = Array.from( value ).reduce( ( sum, c ) => sum + font.glyphs[c.codePointAt( 0 )].advanceX, 0 );
			assert.equal( text.run( value, 0, fontIndex ).width, width );
			const box = [ 100 - Math.floor( width / 2 ), 20, width, text.extentHeight( fontIndex ) ];
			assert.deepEqual(
				expandTextRuns( text.quads( value, box, clip, color, { fontIndex, vAlign: 0 } ) ).map( q => q.rect ),
				glyphQuads( atlas, value, box, clip, color, { fontIndex, vAlign: 0 } ).map( q => q.rect )
			);
		}
	} finally {
		text.dispose();
	}
});
test("native dialog blank lines and countdown color spans survive text layout", () => {
	const lines = expandTextRuns( titleTextBox( atlas, "A\n\nA", rect, clip, color ) );
	assert.equal( lines[1].rect[1] - lines[0].rect[1], 28 );
	const runs = expandTextRuns(
		titleColoredText( atlas, '<sml2>A<font color="255,255,208,81">7</font>A</sml2>', rect, clip, color )
	);
	assert.deepEqual( runs.map( q => q.color ), [ color, [ 1, 208 / 255, 81 / 255, 1 ], color ] );
	assert.deepEqual(
		runs.map( q => q.rect ),
		glyphQuads( atlas, "A7A", rect, clip, color, { vAlign: 0 } ).map( q => q.rect )
	);
});
test("authored font slots select distinct native masks without scaling the default font", () => {
	const small = glyphQuads( atlas, "Connect", rect, clip, color, { fontIndex: 0 } );
	const title = glyphQuads( atlas, "Connect", rect, clip, color, { fontIndex: 2 } );
	assert.notDeepEqual( small.map( q => q.uv ), title.map( q => q.uv ) );
	assert.ok( title.at( -1 ).rect[0] > small.at( -1 ).rect[0], "Title text uses its own larger advances" );
	for ( const [index, quads] of [ [ 0, small ], [ 2, title ] ] ) {
		const glyph = atlas.fonts[index].glyphs["67"];
		assert.equal( quads[0].rect[2], glyph.width );
		assert.equal( quads[0].uv[0], glyph.x / atlas.atlasWidth );
	}
	assert.throws(
		() => glyphQuads( atlas, "A", rect, clip, color, { fontIndex: 99 } ),
		/Missing retail UI font slot/
	);
});
test("top-aligned title captions retain the native line box even in a short control", () => {
	const box = [ 10, 20, 53, 15 ], style = { fontIndex: 2, hAlign: 0, vAlign: 0 };
	const top = glyphQuads( atlas, "ID", box, clip, color, style );
	const center = glyphQuads( atlas, "ID", box, clip, color, { ...style, vAlign: 1 } );
	assert.equal( top[0].rect[1] - center[0].rect[1], 1 );
	assert.equal( top[0].rect[1], 20 + atlas.fonts["2"].ascent - atlas.fonts["2"].glyphs["73"].originY );
	assert.deepEqual( top[0].clip, clip, "Native statics can extend into free space without cropping their line" );
});
test("control characters emit no quads and authored symbols resolve past the replacement glyph", () => {
	// Shipped guide text carries literal newlines (UIIT_STT_GAMEGUIDE_START_2);
	// drawing them through the '?' fallback invented visible glyphs.
	const clean = glyphQuads( atlas, "ABC", rect, clip, color, { vAlign: 0 } );
	const mixed = glyphQuads( atlas, "A\nB\tC\rD", rect, clip, color, { vAlign: 0 } );
	assert.equal( mixed.length, 4 );
	assert.deepEqual( mixed.slice( 0, 3 ).map( q => q.rect ), clean.map( q => q.rect ) );
	const end = q => q.rect[0] + q.rect[2];
	assert.equal(
		end( mixed.at( -1 ) ),
		end( glyphQuads( atlas, "ABCD", rect, clip, color, { vAlign: 0 } ).at( -1 ) )
	);
	const mark = glyphQuads( atlas, "※", rect, clip, color, { vAlign: 0 } );
	assert.equal( mark.length, 1 );
	const fallback = atlas.fonts["0"].glyphs["63"], drawn = atlas.fonts["0"].glyphs["8251"];
	assert.ok( drawn && drawn.x !== fallback.x || drawn.y !== fallback.y, "U+203B resolves to its own atlas cell" );
	assert.deepEqual( mark[0].uv, [
		drawn.x / atlas.atlasWidth,
		drawn.y / atlas.atlasHeight,
		drawn.width / atlas.atlasWidth,
		drawn.height / atlas.atlasHeight
	] );
});

const readText = ( quads, font ) =>
	quads.map( q =>
		String.fromCodePoint(
			Number(
				defined(
					Object.entries( font.glyphs ).find( ( [, g] ) =>
						g.x / atlas.atlasWidth === q.uv[0] && g.y / atlas.atlasHeight === q.uv[1]
					)
				)[0]
			)
		)
	).join( "" );
test("single-line fitting prevents localized labels and long names invading adjacent controls", () => {
	for ( const fontIndex of [ 0, 1, 2, 3, 4 ] ) {
		for ( const fontStyle of [ 0, 2 ] ) {
			for ( const hAlign of [ 0, 1, 2 ] ) {
				const font = fontStyle === 2 ? atlas.fonts[fontIndex].styles["2"] : atlas.fonts[fontIndex];
				// Decode the raw publication's bold variants exactly as the runtime does.
				const effective = { ...atlas, fonts: { ...atlas.fonts, [fontIndex + ":2"]: font } };
				for (
					const value of [ "Retained number", "Distributed number", "Virgo Wind'sVane", "W".repeat( 300 ) ]
				) {
					for ( const width of [ 0, 1, 8, 9, 64, 202 ] ) {
						const box = [ 34, 106, width, 19 ],
							quads = glyphQuads( effective, value, box, clip, color, {
								fontIndex,
								fontStyle,
								hAlign,
								vAlign: 0,
								overflow: "ellipsis"
							} );
						for ( const q of quads ) {
							assert.ok( q.clip[0] >= box[0] );
							assert.ok( q.clip[0] + q.clip[2] <= box[0] + width );
							assert.equal( q.clip[1], clip[1] );
						}
						const rendered = readText( quads, font ),
							advance = s =>
								Array.from( s ).reduce( ( n, c ) => n + font.glyphs[c.codePointAt( 0 )].advanceX, 0 );
						assert.ok(
							advance( rendered ) <= width,
							JSON.stringify( { fontIndex, fontStyle, hAlign, width, rendered } )
						);
						if ( advance( value ) <= width ) assert.equal( rendered, value );
						else if ( width >= 3 * font.glyphs["46"].advanceX ) assert.ok( rendered.endsWith( "..." ) );
					}
				}
			}
		}
	}
});
test("edit overflow preserves all input glyphs, alignment and clipping for caret ownership", () => {
	const value = "12345678901234567890", box = [ 30, 20, 28, 14 ];
	const quads = glyphQuads( atlas, value, box, box, color, { hAlign: 2, overflow: "clip" } );
	assert.equal( readText( quads, atlas.fonts["0"] ), value );
	assert.ok( quads[0].rect[0] < box[0] );
	assert.deepEqual( quads[0].clip, box );
});

test("overflow prefixes agree with 350 bounded executions of native 782AC0", async () => {
	const oracle = JSON.parse( await readFile( "tests/fixtures/native/native-text-prefix.json", "utf8" ) );
	assert.equal( oracle.cases.length, 350 );
	assert.equal( oracle.function, "0x00782AC0" );
	let compared = 0;
	for ( const row of oracle.cases ) {
		const font = row.fontStyle === 2 ? atlas.fonts[row.fontIndex].styles["2"] : atlas.fonts[row.fontIndex];
		const advance = s => Array.from( s ).reduce( ( n, c ) => n + font.glyphs[c.codePointAt( 0 )].advanceX, 0 ),
			width = row.budget + advance( "..." );
		// Native admits a too-wide first glyph. The port deliberately bounds that
		// edge case too, tested separately; compare the common nonempty domain here.
		if ( advance( row.value ) <= width || advance( row.value[0] ) > row.budget ) continue;
		const effective = { ...atlas, fonts: { ...atlas.fonts, [row.fontIndex + ":2"]: font } };
		const quads = glyphQuads( effective, row.value, [ 0, 0, width, 20 ], clip, color, {
			fontIndex: row.fontIndex,
			fontStyle: row.fontStyle,
			overflow: "ellipsis"
		} );
		assert.equal( readText( quads, font ), row.prefix + "...", JSON.stringify( row ) );
		compared++;
	}
	assert.ok( compared > 100 );
});

test("shared projection fits actual neighboring-column collisions, preserving native overflow into free space", () => {
	const box = [ 34, 106, 64, 19 ],
		native = titleText( atlas, "Retained number", box, clip, color, { hAlign: 1, vAlign: 0 } );
	const read = quads => readText( expandTextRuns( quads ), atlas.fonts[0] );
	assert.equal( read( resolveTextOverlaps( native ) ), "Retained number" );
	const neighbor = titleText( atlas, "48", [ 105, 106, 41, 19 ], clip, color, { overflow: "clip", vAlign: 0 } );
	const output = resolveTextOverlaps( [ ...native, ...neighbor ] );
	assert.equal( read( output ), "Retained ...48" );
	assert.ok( output.every( q => !("textLayout" in q) ), "GPU scene carries no layout sidecars" );
	assert.equal( read( native ), "Retained number", "retained source projection stays immutable" );
	const far = titleText( atlas, "48", [ 150, 106, 41, 19 ], clip, color, { overflow: "clip", vAlign: 0 } );
	assert.equal( read( resolveTextOverlaps( [ ...native, ...far ] ) ), "Retained number48" );
	const below = titleText( atlas, "48", [ 105, 150, 41, 19 ], clip, color, { overflow: "clip", vAlign: 0 } );
	assert.equal( read( resolveTextOverlaps( [ ...native, ...below ] ) ), "Retained number48" );
	// Native right-aligned captions and trailing denominations deliberately grow
	// outside narrow alignment rectangles; fitting all boxes broke purchase UI.
	for ( const [value, hAlign] of [ [ "Quantity", 2 ], [ "Gold", 0 ] ] ) {
		const run = titleText( atlas, value, [ 80, 20, 12, 16 ], clip, color, { hAlign } );
		assert.equal( read( resolveTextOverlaps( run ) ), value );
	}
	const faded = native.map( q => ({ ...q, color: [ 1, 1, 1, .25 ] }) );
	const fitted = resolveTextOverlaps( [ ...faded, ...neighbor ] );
	assert.ok(
		expandTextRuns( fitted ).slice( 0, 12 ).every( q => q.color[3] === .25 ),
		"post-layout alpha survives fitting"
	);
	const anchored = native.map( q => ({ ...q, characterAnchor: 1 }) );
	assert.equal( read( resolveTextOverlaps( [ ...anchored, ...neighbor ] ) ), "Retained number48" );
	// Every layout belongs to a run; a loose glyph with a layout is a contract break.
	assert.throws(
		() => resolveTextOverlaps( expandTextRuns( native ) ),
		/text layout without a text run/
	);
});
