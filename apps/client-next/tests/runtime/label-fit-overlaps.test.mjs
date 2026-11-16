/*
===========================================================================

label-fit-overlaps.test.mjs - "Fit window labels" keeps colliding text whole

Issue #3674: the port's overlap repair (ui-glyphs resolveTextOverlaps)
cut every label whose ink entered a neighbouring text box to "Compe...",
"Retained ...", "Guild ma...". Under the Experimental "Fit window labels"
(port-only, on by default) a colliding label wraps inside its box, slides
clear of its neighbours' boxes, or stays whole where its ink misses
theirs; only a label none of those places is shortened. Off keeps the
shortened run as the only repair.

The unit cases lay text with the published font; the window cases drive
the UI gallery recipes the issue names and read the scene back to text.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

const { titleText, resolveTextOverlaps } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { extendedRecipes } = await import( "../../tools/ui-gallery/extended.ts" );
const { OPENERS } = await import( "../../tools/ui-gallery/entry-recipes.mjs" );
const { guildSnapshot } = await import( "../../tools/ui-gallery/scenarios.ts" );

const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

/**
 * @typedef {[number, number, number, number]} Rect
 */

/** @type {Rect} */
const CLIP = [ 0, 0, 1600, 900 ];
/** @type {Rect} */
const WHITE = [ 1, 1, 1, 1 ];
// Glyphs on one baseline further apart than this are separate texts.
const WORD_GAP = 6;
// Glyphs of one line end within the font's descent of each other.
const BASELINE_SLACK = 4;
// Glyph cells may share an edge; an overlap deeper than this is ink on ink.
const INK_SLACK = .5;

// Each atlas cell's character, by its top-left corner.
const CHARACTERS = new Map();
for ( const face of Object.values( fontAtlas.fonts ) ) {
	for ( const [code, glyph] of Object.entries( face.glyphs ) ) {
		const key = Math.round( glyph.x ) + "," + Math.round( glyph.y );
		if ( !CHARACTERS.has( key ) ) CHARACTERS.set( key, String.fromCodePoint( Number( code ) ) );
	}
}

/*
================
character
================
*/
function character( glyph ) {
	const key = Math.round( glyph.uv[0] * fontAtlas.atlasWidth ) + "," +
		Math.round( glyph.uv[1] * fontAtlas.atlasHeight );
	return CHARACTERS.get( key ) ?? "?";
}

/*
================
lines

The text of each drawn line: consecutive glyphs on one bottom edge (within
the font's descent) with no gap wider than WORD_GAP, in paint order, as
{ text, rect } with the line's left, top, right and bottom.
================
*/
function lines( quads ) {
	/** @type {{ text: string; rect: number[]; }[]} */
	const out = [];
	/** @type {readonly number[] | null} */
	let previous = null;
	for ( const glyph of expandTextRuns( quads ).filter( q => q.texture === fontAtlas.image ) ) {
		const [x, y, w, h] = glyph.rect, line = out.at( -1 );
		if (
			!line || !previous || Math.abs( y + h - (previous[1] + previous[3]) ) > BASELINE_SLACK ||
			x < previous[0] || x - (previous[0] + previous[2]) > WORD_GAP
		) {
			out.push( { text: character( glyph ), rect: [ x, y, x + w, y + h ] } );
		} else {
			line.text += character( glyph );
			line.rect = [
				Math.min( line.rect[0], x ),
				Math.min( line.rect[1], y ),
				Math.max( line.rect[2], x + w ),
				Math.max( line.rect[3], y + h )
			];
		}
		previous = glyph.rect;
	}
	return out;
}

/*
================
inkCrossings

The pairs of text runs whose glyphs overlap: one label drawn on another.
================
*/
function inkCrossings( quads ) {
	const runs = quads.filter( q => q.run && q.texture === fontAtlas.image ).map( q => expandTextRuns( [ q ] ) );
	const out = [];
	for ( let i = 0; i < runs.length; i++ ) {
		for ( let j = i + 1; j < runs.length; j++ ) {
			const crossed = runs[i].some( a =>
				runs[j].some( b =>
					Math.min( a.rect[0] + a.rect[2], b.rect[0] + b.rect[2] ) - Math.max( a.rect[0], b.rect[0] ) >
						INK_SLACK &&
					Math.min( a.rect[1] + a.rect[3], b.rect[1] + b.rect[3] ) - Math.max( a.rect[1], b.rect[1] ) >
						INK_SLACK
				)
			);
			if ( crossed ) {
				out.push( [
					lines( [ quads.filter( q => q.run )[i] ] ),
					lines( [ quads.filter( q => q.run )[j] ] )
				] );
			}
		}
	}
	return out;
}

/*
================
inkRight

The right edge of a run's drawn glyphs.
================
*/
function inkRight( quads ) {
	return Math.max( ...expandTextRuns( quads ).map( q => q.rect[0] + q.rect[2] ) );
}

// ============================================================================

test("off, a colliding label is shortened as before", () => {
	const label = titleText( fontAtlas, "Retained number", [ 34, 106, 64, 19 ], CLIP, WHITE, { hAlign: 1, vAlign: 0 } );
	const count = titleText( fontAtlas, "48", [ 105, 106, 41, 19 ], CLIP, WHITE, { overflow: "clip", vAlign: 0 } );
	assert.deepEqual( lines( resolveTextOverlaps( [ ...label, ...count ] ) ).map( l => l.text ), [
		"Retained ...",
		"48"
	] );
});

test("a label with a space wraps inside its box's columns", () => {
	/** @type {Rect} */
	const box = [ 34, 106, 64, 19 ];
	const label = titleText( fontAtlas, "Retained number", box, CLIP, WHITE, { hAlign: 1, vAlign: 0 } );
	const count = titleText( fontAtlas, "48", [ 105, 106, 41, 19 ], CLIP, WHITE, { overflow: "clip", vAlign: 0 } );
	const drawn = lines( resolveTextOverlaps( [ ...label, ...count ], true ) );
	assert.deepEqual( drawn.map( l => l.text ), [ "Retained", "number", "48" ] );
	for ( const line of drawn.slice( 0, 2 ) ) {
		assert.ok( line.rect[0] >= box[0] && line.rect[2] <= box[0] + box[2], `${line.text} inside its box` );
	}
	assert.ok( drawn[0].rect[3] <= drawn[1].rect[1] + 3, "the second line sits under the first" );
});

test("a wrap that would reach the rows above and below is refused", () => {
	// The split box's label with a row 2 pixels over and under its box:
	// the pair (two ink heights and a pixel) is taller than the 19-pixel
	// box, so it would put ink in both rows.
	/** @type {Rect} */
	const box = [ 34, 106, 64, 19 ];
	const label = titleText( fontAtlas, "Retained number", box, CLIP, WHITE, { hAlign: 1, vAlign: 0 } );
	const count = titleText( fontAtlas, "48", [ 105, 106, 41, 19 ], CLIP, WHITE, { overflow: "clip", vAlign: 0 } );
	const above = titleText( fontAtlas, "Above", [ 34, 92, 64, 12 ], CLIP, WHITE, { hAlign: 1, vAlign: 1 } );
	const below = titleText( fontAtlas, "Below", [ 34, 127, 64, 12 ], CLIP, WHITE, { hAlign: 1, vAlign: 1 } );
	const painted = resolveTextOverlaps( [ ...above, ...label, ...count, ...below ], true );
	const drawn = lines( painted ).map( l => l.text );
	assert.ok( !drawn.includes( "number" ), `no wrapped second line in ${JSON.stringify( drawn )}` );
	assert.ok( drawn.includes( "Retained ..." ), "a value-style label falls back to its shortened run" );
	assert.deepEqual( inkCrossings( painted ), [] );
	// Without the rows the same label wraps (the case above).
	assert.ok( lines( resolveTextOverlaps( [ ...label, ...count ], true ) ).some( l => l.text === "number" ) );
});

test("a single word slides clear of its neighbour's box, at most its overflow", () => {
	// ifguildwaragree.txt: the stake label (33, 51 wide) and the gold field
	// at 108, laid at the window's origin.
	/** @type {Rect} */
	const box = [ 33, 117, 51, 14 ];
	/** @type {Rect} */
	const field = [ 108, 117, 71, 14 ];
	const label = titleText( fontAtlas, "Compensation", box, CLIP, WHITE, { vAlign: 1, label: true } );
	const gold = titleText( fontAtlas, "400", field, CLIP, WHITE, { hAlign: 2, vAlign: 1 } );
	const painted = resolveTextOverlaps( [ ...label, ...gold ], true );
	const [stake] = lines( painted );
	assert.equal( stake.text, "Compensation" );
	assert.ok( stake.rect[2] <= field[0], `ends at ${stake.rect[2]}, before the field` );
	assert.ok(
		inkRight( label ) - stake.rect[2] <= inkRight( label ) - box[0] - box[2],
		"moved no more than it overflowed"
	);
});

test("a label stays whole where its ink misses a centred neighbour's ink; a value keeps to its field", () => {
	// ifapprenticeship.txt row: "Lv" (156, 14), the slot at 172 (19 wide)
	// with no room to slide, the rank name centred in 104 at 201.
	const lv = titleText( fontAtlas, "Lv", [ 156, 46, 14, 17 ], CLIP, WHITE, { vAlign: 1 } );
	const rank = titleText( fontAtlas, "Member", [ 201, 46, 104, 17 ], CLIP, WHITE, { hAlign: 1, vAlign: 1 } );
	const slot = label =>
		titleText( fontAtlas, "80(80)", [ 172, 46, 19, 17 ], CLIP, WHITE, { vAlign: 1, ...(label ? { label } : {}) } );
	const drawn = label =>
		lines( resolveTextOverlaps( [ ...lv, ...slot( label ), ...rank ], true ) ).map( l => l.text );
	assert.deepEqual( drawn( true ), [ "Lv80(80)", "Member" ] );
	// The character panel's honour value cut to its 28-pixel field is the
	// same rule: a value never runs past its field's art.
	assert.deepEqual( drawn( false ), [ "Lv8...", "Member" ] );
});

// ============================================================================

/*
================
openRecipe

The scene a gallery recipe settles into, with "Fit window labels" on or
off. "open-window:<panel>" opens a registry panel the way its sidebar
button does; "opener:<name>" runs the gallery's opener of that name.
================
*/
async function openRecipe( id, fit ) {
	const recipe = id.startsWith( "open-window:" ) ?
		{ open: context => context.ui.event( { kind: "activate", id } ) } :
		id.startsWith( "opener:" ) ?
		{ open: context => OPENERS[id.slice( "opener:".length )]( context, context.step ) } :
		extendedRecipes.find( r => r.id === id );
	assert.ok( recipe, id );
	const f = uiFixture();
	let now = 0, seen = null;
	f.state.gameplay.social = guildSnapshot();
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { fitWindowLabels: fit } ) } );
	const step = async () => {
		for ( let i = 0; i < 6; i++ ) seen = f.ui.step( f.state, now += 300 ) ?? seen;
	};
	await step();
	// The gallery's recipe context, built on the test fixture (extended.ts RecipeContext).
	await recipe.open(
		/** @type {any} */ ({
			...f,
			state: f.state,
			step,
			advance: milliseconds => {
				now += milliseconds;
			},
			controls: () => seen?.controls ?? []
		})
	);
	await step();
	await step();
	const quads = f.scenes.at( -1 ).quads;
	f.dispose();
	return quads;
}

// Each recipe the issue names and the label it cut, as drawn with the row
// on (its lines) and off (the shortened run).
const WINDOWS = [
	{ id: "breadth:guild-union", on: [ "Number of", "guilds" ], off: "Number o..." },
	{ id: "open-window:Guild", on: [ "Guild", "master" ], off: "Guild ma..." },
	{ id: "reach:guild-war-agreement", on: [ "Compensation" ], off: "Compe..." },
	{ id: "inspect:guild-war-declaration", on: [ "Compensation" ], off: "Compe..." },
	{ id: "inspect:inventory-split", on: [ "Retained", "number", "Distributed", "number" ], off: "Retained ..." },
	// #3712: the academy's level value, the matching search labels and
	// the job ranking's title of level.
	{ id: "breadth:academy-master", on: [ "Lv80(80)" ], off: "Lv8..." },
	{ id: "inspect:academy-matching", on: [ "Recruitment", "Condition", "Honor", "Grade" ], off: "Recr..." },
	{ id: "opener:Job ranking", on: [ "Apprentice", "Merchant" ], off: "Apprentice M..." }
];

for ( const { id, on, off } of WINDOWS ) {
	test(`${id}: ${on.join( " " )} is drawn whole and on no other text`, async () => {
		const quads = await openRecipe( id, true );
		const drawn = lines( quads ).map( l => l.text );
		for ( const text of on ) assert.ok( drawn.includes( text ), `${text} in ${JSON.stringify( drawn )}` );
		assert.ok( !drawn.some( text => text.endsWith( "..." ) && text.startsWith( off.slice( 0, 3 ) ) ), "no cut" );
		assert.deepEqual( inkCrossings( quads ), [] );
		assert.ok( lines( await openRecipe( id, false ) ).some( l => l.text === off ), `off draws ${off}` );
	});
}

// The academy's dismiss plate (party/pt_button.ddj) is 44 pixels wide.
const DISMISS_PLATE = 44;

/*
================
lineNamed
================
*/
function lineNamed( quads, text ) {
	const line = lines( quads ).find( l => l.text === text );
	assert.ok( line, `${text} in ${JSON.stringify( lines( quads ).map( l => l.text ) )}` );
	return line;
}

test("a caption wider than its plate takes the narrow face (#3712)", async () => {
	for ( const fit of [ true, false ] ) {
		const matching = await openRecipe( "inspect:academy-matching", fit );
		const graduates = lineNamed( matching, "Graduates" ), member = lineNamed( matching, "Member" );
		// On, the heads keep a gap; off, "Graduates" runs into "Member" as before.
		assert.equal( graduates.rect[2] < member.rect[0], fit, `fit ${fit}: ${graduates.rect} | ${member.rect}` );
		const dismiss = lineNamed( await openRecipe( "breadth:academy-master", fit ), "Dismiss" );
		assert.equal( dismiss.rect[2] - dismiss.rect[0] < DISMISS_PLATE - 4, fit, `fit ${fit}: ${dismiss.rect}` );
	}
});

test("a grade column is headed Grade, not level, with the row on or off (#3712)", async () => {
	for ( const fit of [ true, false ] ) {
		for ( const id of [ "open-window:Guild", "opener:Job ranking" ] ) {
			const drawn = lines( await openRecipe( id, fit ) ).map( l => l.text );
			assert.ok( drawn.some( text => text.startsWith( "Grade" ) ), `${id}: ${JSON.stringify( drawn )}` );
			assert.ok( !drawn.some( text => /^level|Level ?level/.test( text ) ), `${id}: ${JSON.stringify( drawn )}` );
		}
	}
});
