/*
===========================================================================

english-text-fixes.test.mjs - retail English text defects, fixed

A misspelled retail string is corrected in the reviewed English table
(scripts/build/shared/englishCorrections.json), which the asset build
applies; the hostile-guild row is drawn here from the built catalog through
that same policy. Port-only, not native (owner 2026-11-21: the default, no
row): a speech board breaks between words, where the original breaks at
whichever glyph passes its width (CGFontTexture_UpdateExtent 7824F0), and
the creation screen's "Unique check" caption is trimmed to its plate.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { drawCreation } = await import( "../../src/engine/runtime/ui/title/creation.ts" );
const { decodeUiFont } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { createCreation } = await import( "../../src/engine/runtime/frontend/creation/creation.ts" );
const { readPublishedAssetJsonSync } = await import( "../../../../scripts/lib/publishedAsset.mjs" );
const { correctedEnglish } = await import( "../../../../scripts/build/shared/englishCorrections.mjs" );

const UI_SYSTEM_CATALOG = "/assets/text/textuisystem.en.json";
const HOSTILE_GUILD_KEY = "UIIT_MSG_GUILDWAR_NOTENEMYGUILD";

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
function fixture( preferences, published = {} ) {
	const f = uiFixture(
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{},
		published
	);
	let clock = 0, output;
	const draw = () => {
		for ( let i = 0; i < 4; i++ ) output = f.ui.step( f.state, clock += 100 ) ?? output;
		assert.equal( f.ui.stats().error, null );
		return output;
	};
	f.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: preferences }) );
	return { f, draw };
}

/*
================
correctedCatalog

The built UI system catalog with the hostile-guild cell corrected through
the build's own policy, which also accepts an already-corrected cell, so it
reads the same before and after the next asset build.
================
*/
function correctedCatalog() {
	const catalog = readPublishedAssetJsonSync( UI_SYSTEM_CATALOG );
	const entries = {
		...catalog.entries,
		[HOSTILE_GUILD_KEY]: correctedEnglish(
			"textuisystem.txt",
			HOSTILE_GUILD_KEY,
			catalog.entries[HOSTILE_GUILD_KEY]
		)
	};
	return { [UI_SYSTEM_CATALOG]: Buffer.from( JSON.stringify( { ...catalog, entries } ) ) };
}

test("the empty hostile-guild row reads corrected", () => {
	const { f, draw } = fixture( {}, correctedCatalog() );
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
	assert.ok( f.hasText( "<No hostile Guild>" ) );
	assert.ok( !f.hasText( "<No hopstile Guild>" ) );
});

test("a speech board breaks between words", () => {
	const { f, draw } = fixture( {} );
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
	// The board's first row ends "human" ("review" moves down whole);
	// retail ends it "human r", the glyph past the width moving down.
	const glyphs = ( value ) => value.replaceAll( " ", "" ).length;
	const rows = boardRows( f, speaker.gid );
	assert.equal( rows[0], glyphs( "Synthetic speech board for human" ) );
});

/*
================
creationCaptions

The Europe creation window drawn once from the published layout, font and
text: the name-check button and the glyphs drawn on its plate (a run quad
carries its glyphs).
================
*/
function creationCaptions() {
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
		interactive: true
	} );
	const check = defined( output.controls.find( control => control.id === "create:check" ) );
	const [x, y, w, h] = check.rect;
	owner.dispose();
	return {
		check,
		glyphs: output.quads.filter( q =>
			q.texture === font.image && q.rect[0] < x + w && q.rect[0] + q.rect[2] > x && q.rect[1] < y + h &&
			q.rect[1] + q.rect[3] > y
		).reduce( ( n, q ) => n + (q.run?.glyphs.length ?? 1), 0 )
	};
}

test("the creation name-check caption fits its plate", () => {
	const fitted = creationCaptions();
	// The control keeps the full caption for assistive tech.
	assert.equal( fitted.check.label, "Unique check" );
	// Only "Unique" is drawn on the plate; " check" is dropped.
	assert.equal( fitted.glyphs, "Unique".length );
});
