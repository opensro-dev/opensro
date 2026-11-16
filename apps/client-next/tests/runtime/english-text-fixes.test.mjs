/*
===========================================================================

english-text-fixes.test.mjs - the port-only English text rows, on and off

"Correct English typos" swaps a misspelled retail string for its corrected
text; "Fit window labels" breaks a speech board between words and trims the
creation screen's "Unique check" caption to its plate. Off, each
draws the original: the retail spelling, and a board broken at whichever
glyph passes its width (CGFontTexture_UpdateExtent 7824F0).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { drawCreation } = await import( "../../src/engine/runtime/ui/title/creation.ts" );
const { decodeUiFont } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { createCreation } = await import( "../../src/engine/runtime/frontend/creation/creation.ts" );
const { readPublishedAssetJsonSync } = await import( "../../../../scripts/lib/publishedAsset.mjs" );

// A public line whose glyph break falls inside "review" at the board width.
const SPEECH = "Synthetic speech board for human review";
// Under the board's line height and over any glyph's bearing.
const ROW_GAP = 8;

/*
================
boardRows

Glyph counts of the rows drawn over the speaker, top to bottom: font
quads anchored to gid, a new row wherever a glyph starts at least
ROW_GAP below the first glyph of the row before (glyph bearings move
each quad's top within a row).
================
*/
function boardRows( f, gid ) {
	const tops = f.scenes.at( -1 ).quads
		.filter( q => q.characterAnchor === gid && q.texture === fontAtlas.image )
		.map( q => q.rect[1] )
		.sort( ( a, b ) => a - b );
	const rows = [];
	let first = -Infinity;
	for ( const top of tops ) {
		if ( top - first >= ROW_GAP ) {
			rows.push( 0 );
			first = top;
		}
		rows[rows.length - 1]++;
	}
	return rows;
}

/*
================
fixture

A world UI with the given Experimental rows, drawing a frame per call.
================
*/
function fixture( preferences ) {
	const f = uiFixture();
	let clock = 0, output;
	const draw = () => {
		for ( let i = 0; i < 4; i++ ) output = f.ui.step( f.state, clock += 100 ) ?? output;
		assert.equal( f.ui.stats().error, null );
		return output;
	};
	f.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: preferences }) );
	return { f, draw };
}

test("Correct English typos is on unless explicitly turned off", () => {
	for ( const value of [ undefined, null, {}, { correctTypos: 1 }, { correctTypos: "false" } ] ) {
		assert.equal( experimentalOptions( value ).correctTypos, true );
	}
	assert.equal( experimentalOptions( { correctTypos: false } ).correctTypos, false );
});

for ( const correct of [ false, true ] ) {
	test(`the empty hostile-guild row reads ${correct ? "corrected" : "the retail typo"}`, () => {
		const { f, draw } = fixture( { correctTypos: correct } );
		Object.assign( f.state.gameplay, {
			social: {
				...emptySocial( "Player" ),
				self: 1,
				guild: {
					id: 7,
					name: "Guild",
					level: 5,
					gp: 50,
					subject: "",
					contents: "",
					crest: 0,
					members: [ {
						id: 1,
						name: "Player",
						grade: 0,
						level: 40,
						permissions: 31,
						grant: "",
						donated: 0,
						model: 1907,
						role: 0,
						offline: 0
					} ]
				}
			}
		} );
		draw();
		for ( const id of [ "open-window:Guild", "guild-tab:2" ] ) {
			f.ui.event( { kind: "activate", id } );
			draw();
		}
		assert.equal( f.hasText( "<No hostile Guild>" ), correct );
		assert.equal( f.hasText( "<No hopstile Guild>" ), !correct );
	});
}

for ( const fit of [ false, true ] ) {
	test(`a speech board breaks ${fit ? "between words" : "at the glyph past its width"}`, () => {
		const { f, draw } = fixture( { fitWindowLabels: fit } );
		const speaker = f.state.entities.find( e => e.gid === f.state.gameplay.localGid );
		assert.ok( speaker, "the fixture has a local player" );
		Object.assign( f.state.gameplay, {
			chat: {
				pending: false,
				error: null,
				lines: [ { channel: 1, name: speaker.name, text: SPEECH, outgoing: false, sequence: 1 } ]
			}
		} );
		draw();
		// The board's first row ends "human r" retail (the glyph past the
		// width moves down) and "human" fitted ("review" moves down whole).
		const glyphs = ( value ) => value.replaceAll( " ", "" ).length;
		const rows = boardRows( f, speaker.gid );
		assert.equal(
			rows[0],
			glyphs( fit ? "Synthetic speech board for human" : "Synthetic speech board for human r" )
		);
	});
}

/*
================
creationCaptions

The Europe creation window drawn once from the published layout, font and
text: the semantic button labels and the drawn glyph count (a run quad
carries its glyphs).
================
*/
function creationCaptions( fitLabels ) {
	const layout = readPublishedAssetJsonSync( "/assets/cif/layouts/pscharactercreate_europe.json" ),
		font = decodeUiFont( readPublishedAssetJsonSync( "/assets/fonts/native-ui-font-atlas.json" ) ),
		catalog = readPublishedAssetJsonSync( "/assets/text/textuisystem.en.json" ).entries;
	const assets = /** @type {any} */ ({ available: () => 0, request: () => 0, take: () => null, cancel() {} });
	const owner = createCreation( assets, "http://localhost", () => {}, () => {} );
	owner.open( 0, 0 );
	const input = { hover: null, pressed: null, focus: null, draft: "", offset: 0, now: 0, message: "" };
	const output = drawCreation( layout, font, catalog, defined( owner.snapshot() ), input, {
		w: 1024,
		h: 768,
		interactive: true,
		fitLabels
	} );
	const check = output.controls.find( control => control.id === "create:check" );
	owner.dispose();
	return {
		check,
		glyphs: output.quads.filter( q => q.texture === font.image ).reduce(
			( n, q ) => n + (q.run?.glyphs.length ?? 1),
			0
		)
	};
}

test("the creation name-check caption fits its plate only under Fit window labels", () => {
	const retail = creationCaptions( false ), fitted = creationCaptions( true );
	// The control keeps the full caption for assistive tech either way.
	assert.equal( retail.check?.label, "Unique check" );
	assert.equal( fitted.check?.label, "Unique check" );
	// " check" (its space is a glyph too) is dropped from the drawn caption.
	assert.equal( retail.glyphs - fitted.glyphs, " check".length );
});
