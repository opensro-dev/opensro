/*
===========================================================================

text-run.test.mjs - a laid-out string travels as one quad

Every text the UI draws is a run quad; expandTextRuns turns it back into
the glyph quads its layout made, value for value. Overlap fitting tests
each glyph's ink, not the run's bounding box.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
const { textRunQuad, expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { resolveTextOverlaps } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );

const white = /** @type {const} */ ([ 1, 1, 1, 1 ]);

/** @typedef {import("../../src/engine/contracts/ui").UiQuad} UiQuad */
/** @typedef {import("../../src/engine/contracts/ui").UiTextLayout} UiTextLayout */

/*
================
glyph

A glyph quad as the private glyph layout makes it.
================
*/
/**
 * @param {number} x @param {number} y @param {number} width @param {number} height
 * @returns {UiQuad}
 */
function glyph( x, y, width, height ) {
	return {
		rect: [ x, y, width, height ],
		uv: [ x / 100, 0, .01, .01 ],
		color: white,
		texture: "font",
		clip: [ 0, 0, 500, 500 ]
	};
}

/*
================
laidOut

A run of glyph quads carrying a layout, as titleText publishes it.
================
*/
/**
 * @param {UiQuad[]} glyphs @param {UiTextLayout} layout
 * @returns {UiQuad}
 */
function laidOut( glyphs, layout ) {
	return Object.freeze( { ...defined( textRunQuad( glyphs ) ), textLayout: layout } );
}

test("a run expands back into the glyph quads it was made from", () => {
	const quads = [ glyph( 12, 22, 5, 8 ), glyph( 18, 21, 6, 9 ), glyph( 30, 23, 4, 7 ) ];
	const run = defined( textRunQuad( quads ) );
	assert.ok( Object.isFrozen( run ) );
	assert.deepEqual( run.rect, [ 12, 21, 22, 9 ] );
	const expanded = expandTextRuns( [ run ] );
	assert.deepEqual( expanded, quads );
	// Moving the run's rect moves every glyph: window code shifts quads that way.
	const moved = expandTextRuns( [ {
		...run,
		rect: [ run.rect[0] + 100, run.rect[1] + 5, run.rect[2], run.rect[3] ]
	} ] );
	assert.deepEqual( moved.map( q => q.rect[0] ), [ 112, 118, 130 ] );
	assert.deepEqual( moved.map( q => q.rect[1] ), [ 27, 26, 28 ] );
	assert.equal( textRunQuad( [] ), null );
});

test("overlap fitting tests each glyph's ink, not the run's box", () => {
	// "A   B" overflows its own narrow box; another label's (disjoint) box sits
	// in the word gap. Overlapping boxes are layers and are never fitted.
	const shortened = defined( textRunQuad( [ glyph( 0, 0, 3, 10 ) ] ) );
	const words = laidOut( [ glyph( 0, 0, 6, 10 ), glyph( 60, 0, 6, 10 ) ], {
		box: [ 0, 0, 15, 12 ],
		fitted: shortened
	} );
	const neighbor = laidOut( [ glyph( 22, 0, 6, 10 ) ], { box: [ 20, 0, 20, 12 ], fitted: undefined } );
	const painted = expandTextRuns( resolveTextOverlaps( [ words, neighbor ] ) );
	assert.equal( painted.length, 3, "no ink enters the neighbor, so the wide label keeps both glyphs" );
	// Ink that does enter the neighbor's box selects the shortened run; one
	// with no drawable glyph (null) draws nothing.
	const reaching = laidOut( [ glyph( 0, 0, 6, 10 ), glyph( 24, 0, 6, 10 ) ], {
		box: [ 0, 0, 15, 12 ],
		fitted: shortened
	} );
	const swapped = expandTextRuns( resolveTextOverlaps( [ reaching, neighbor ] ) );
	assert.deepEqual( swapped.map( q => q.rect ), [ [ 0, 0, 3, 10 ], [ 22, 0, 6, 10 ] ] );
	const empty = laidOut( [ glyph( 0, 0, 6, 10 ), glyph( 24, 0, 6, 10 ) ], { box: [ 0, 0, 15, 12 ], fitted: null } );
	assert.equal( expandTextRuns( resolveTextOverlaps( [ empty, neighbor ] ) ).length, 1 );
});

test("fitting a control group scales text runs with their artwork", async () => {
	const { fitUiGroup } = await import( "../../src/engine/foundation/ui/layout.ts" );
	const screen = /** @type {const} */ ([ 0, 0, 400, 300 ]);
	/** @type {UiQuad} */
	const art = { rect: [ 0, 0, 800, 600 ], uv: [ 0, 0, 1, 1 ], color: white, texture: "panel", clip: screen };
	const glyphs = [ glyph( 40, 50, 6, 10 ), glyph( 47, 52, 5, 8 ) ];
	const run = defined( textRunQuad( glyphs ) );
	/** @type {UiQuad[]} */
	const runs = [ art, run ], plain = [ art, ...glyphs ];
	fitUiGroup( runs, [], 0, 0, [ 0, 0, 400, 300 ], screen );
	fitUiGroup( plain, [], 0, 0, [ 0, 0, 400, 300 ], screen );
	assert.deepEqual( expandTextRuns( runs ), plain );
	assert.deepEqual( plain[1].rect, [ 20, 25, 3, 5 ] );
});
