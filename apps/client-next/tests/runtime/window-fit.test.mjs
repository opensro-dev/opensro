/*
===========================================================================

window-fit.test.mjs - the F9 visual gate's four fits

The letter read and write windows are centred msgbox2 popups (607830 /
607A10), not a translucent sheet over the letter page; their long button
captions fit the 76-pixel plate under "Fit window labels"; every
Experimental description fits its 304-pixel line; and a tooltip never
leaves a narrow screen.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { captionWords } = await import( "../../src/engine/foundation/ui/text-lines.ts" );
const { tooltipBubble } = await import( "../../src/engine/foundation/ui/helper-bubble.ts" );
const { EXPERIMENTAL_TABS } = await import( "../../src/engine/runtime/ui/hud/experimental-hud.ts" );

const FRAME = "interface/messagebox/msgbox2_window_";
// ifletter.txt ids 51 and 55: Rect 0,0,439,271.
const POPUP = [ 439, 271 ];
// The Experimental description static: 39..343 on the window.
const DESCRIPTION_WIDTH = 304;

/*
================
advance

Font 0's advance for a string, as the HUD's text run measures it.
================
*/
function advance( value ) {
	const glyphs = fontAtlas.fonts["0"].glyphs;
	return Array.from( value ).reduce( ( sum, c ) => sum + (glyphs[c.codePointAt( 0 )] ?? glyphs["63"]).advanceX, 0 );
}

/*
================
openLetter

The Community window's Letter page with one letter, then the read window
(reading 0) or the write window (the Send message command).
================
*/
function openLetter( { fit, write } ) {
	const fixture = uiFixture();
	let time = 0, published;
	const draw = () => {
		for ( let i = 0; i < 4; i++ ) published = fixture.ui.step( fixture.state, ++time * 100 ) ?? published;
		assert.equal( fixture.ui.stats().error, null );
		return defined( published );
	};
	fixture.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: { fitWindowLabels: fit } }) );
	const row = { sender: "Berk", modelRefId: 1907, received: 0, read: false, body: "Hello" };
	Object.assign( fixture.state.gameplay, {
		social: emptySocial( "Player" ),
		letters: { rows: [ row ], reading: -1 }
	} );
	draw();
	fixture.ui.event( { kind: "activate", id: "open-window:Guild" } );
	draw();
	fixture.ui.event( { kind: "activate", id: "guild-tab:5" } );
	draw();
	if ( write ) fixture.ui.event( { kind: "activate", id: "letter-write" } );
	else Object.assign( fixture.state.gameplay, { letters: { rows: [ row ], reading: 0 } } );
	return { fixture, output: draw() };
}

/*
================
glyphsBeside

Font glyphs on a control's line that start within a plate's width of it:
what a caption drew, including any overflow past the plate.
================
*/
function glyphsBeside( fixture, rect ) {
	const scene = defined( fixture.scenes.at( -1 ) );
	return scene.quads.filter( q =>
		q.texture === fontAtlas.image && q.rect[1] >= rect[1] && q.rect[1] < rect[1] + rect[3] &&
		q.rect[0] + q.rect[2] > rect[0] - 20 && q.rect[0] < rect[0] + rect[2] + 20
	);
}

for ( const write of [ false, true ] ) {
	test(`the letter ${write ? "write" : "read"} window is a centred msgbox2 popup`, () => {
		const { fixture, output } = openLetter( { fit: true, write } );
		try {
			const { width, height } = fixture.state;
			const left = Math.floor( (width - POPUP[0]) / 2 ), top = Math.floor( (height - POPUP[1]) / 2 );
			const area = defined( output.controls.find( c => c.id === "letter-child" ) );
			assert.deepEqual( area.rect, [ left, top, ...POPUP ], "CGInterface_CenterWindowOnScreen" );
			const frame = defined( fixture.scenes.at( -1 ) ).quads.filter( q => q.texture.includes( FRAME ) );
			assert.ok( frame.length >= 8, "the frame's eight parts are drawn" );
			assert.equal( Math.min( ...frame.map( q => q.rect[0] ) ), left );
			assert.equal( Math.min( ...frame.map( q => q.rect[1] ) ), top );
			assert.equal( Math.max( ...frame.map( q => q.rect[0] + q.rect[2] ) ), left + POPUP[0] );
			assert.equal( Math.max( ...frame.map( q => q.rect[1] + q.rect[3] ) ), top + POPUP[1] );
			const sheet = defined( fixture.scenes.at( -1 ) ).quads.filter( q =>
				q.texture === "" && q.color[3] > .9 && q.color[3] < .93 && q.rect[2] === POPUP[0]
			);
			assert.deepEqual( sheet, [], "no translucent sheet stands in for the frame" );
		} finally {
			fixture.dispose();
		}
	});
}

for ( const fit of [ true, false ] ) {
	test(`the letter popups' long captions ${fit ? "fit their plates" : "stay native with the row off"}`, () => {
		for (
			const { write, id, full } of [
				{ write: false, id: "letter-read-delete", full: "Delete message" },
				{ write: true, id: "letter-write-send", full: "Send message" }
			]
		) {
			const { fixture, output } = openLetter( { fit, write } );
			try {
				const button = defined( output.controls.find( c => c.id === id ) );
				assert.equal( button.label, fit ? full.split( " " )[0] : full );
				const ink = glyphsBeside( fixture, button.rect );
				assert.ok( ink.length > 0, id );
				const inside = ink.every( q =>
					q.rect[0] >= button.rect[0] && q.rect[0] + q.rect[2] <= button.rect[0] + button.rect[2]
				);
				assert.equal( inside, fit, `${id}: caption inside its plate` );
			} finally {
				fixture.dispose();
			}
		}
	});
}

test("captionWords keeps the leading words that fit", () => {
	const measure = value => value.length * 7;
	assert.equal( captionWords( "Close", 68, measure ), "Close", "a caption that fits is unchanged" );
	assert.equal( captionWords( "Delete message", 68, measure ), "Delete" );
	assert.equal( captionWords( "Send a new message", 68, measure ), "Send a" );
	assert.equal( captionWords( "Unbreakable", 20, measure ), "Unbreakable", "a single word is never cut" );
});

test("every Experimental description fits its line", () => {
	for ( const tab of EXPERIMENTAL_TABS ) {
		for ( const row of tab.rows ) {
			assert.ok(
				advance( row.description ) <= DESCRIPTION_WIDTH,
				`${row.label}: ${advance( row.description )} > ${DESCRIPTION_WIDTH}`
			);
		}
	}
});

test("a tooltip stays inside a narrow screen with Fit window labels", () => {
	const measure = value => value.length * 7;
	const rows = [ { value: "A long item name that fills the bubble", color: 0xffffffff } ];
	// A phone portrait: the anchor near the left, too far right for the right side.
	const phone = tooltipBubble( rows, [ 100, 300, 260, 32 ], [ 0, 0, 390, 844 ], measure, true );
	const fill = phone.quads[0].rect;
	assert.ok( fill[0] - 8 >= 0, `left border at ${fill[0] - 8}` );
	assert.ok( fill[0] + fill[2] + 8 <= 390, `right border at ${fill[0] + fill[2] + 8}` );
	assert.ok( phone.lines.every( line => line.rect[0] === fill[0] ), "the text moves with the bubble" );
	// Where a native side fits, the bubble keeps 6791C0's placement.
	for ( const fit of [ true, false ] ) {
		const right = tooltipBubble( rows, [ 100, 300, 32, 32 ], [ 0, 0, 1200, 900 ], measure, fit );
		assert.equal( right.quads[0].rect[0], 140 );
		const left = tooltipBubble( rows, [ 1100, 300, 32, 32 ], [ 0, 0, 1200, 900 ], measure, fit );
		assert.equal( left.quads[0].rect[0], 1100 - left.quads[0].rect[2] - 8 );
	}
});

test("without Fit window labels a tooltip keeps 6791C0's placement exactly", () => {
	const measure = value => value.length * 7;
	const rows = [ { value: "A long item name that fills the bubble", color: 0xffffffff } ];
	const phone = tooltipBubble( rows, [ 100, 300, 260, 32 ], [ 0, 0, 390, 844 ], measure );
	const width = phone.quads[0].rect[2];
	// The right side does not fit (100 + 260 + width + 20 > 390): left side -8.
	assert.equal( phone.quads[0].rect[0], 100 - width - 8 );
	assert.ok( phone.quads[0].rect[0] < 0, "natively the bubble leaves a narrow screen" );
});
