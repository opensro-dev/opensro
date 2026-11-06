/*
===========================================================================

wholechat-edit.test.mjs - the Chat to all edit renders one native text line

The authored CIFEdit box is 36 px tall, but native CIFEdit aligns a single
text line inside it (520AA0 by the authored VAlign) and draws the caret as
a line of the font's height (522B50), never the control's height (#2615).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

const GLOBAL_CHAT_TYPE = 0x29ec;
const BAG_SLOT = 13;

test("wholechat caret spans one text line at the top of the tall edit box", () => {
	const f = uiFixture();
	let now = 0, presentation = /** @type {any} */ ( null );
	const step = () => {
		for ( let i = 0; i < 16; i++ ) presentation = f.ui.step( f.state, now += 100 ) ?? presentation;
	};
	step();
	Object.assign( f.state.gameplay, {
		inventorySlotCount: 45,
		inventory: [ { slot: BAG_SLOT, refObjId: 24600, typeFlags: GLOBAL_CHAT_TYPE, quantity: 3 } ]
	} );
	f.ui.event( { kind: "double-activate", id: "slot:" + BAG_SLOT } );
	step();
	f.ui.event( { kind: "edit", id: "wholechat-text", value: "Hello", start: 5, end: 5, composing: false } );
	step();
	assert.ok( presentation, "no presentation" );
	const edit = presentation.controls.find( c => c.id === "wholechat-text" );
	assert.ok( edit, "wholechat edit not published" );
	assert.equal( edit.rect[3], 36, "the authored edit box is the tall one" );
	const font = fontAtlas.fonts["0"];
	const lineHeight = font.ascent + font.descent;
	const scene = f.scenes.at( -1 );
	const inEdit = q =>
		q.rect[0] >= edit.rect[0] && q.rect[0] < edit.rect[0] + edit.rect[2] &&
		q.rect[1] >= edit.rect[1] - 1 && q.rect[1] < edit.rect[1] + edit.rect[3];
	const carets = scene.quads.filter( q => q.texture === "" && q.rect[2] === 1 && inEdit( q ) );
	assert.equal( carets.length, 1, "exactly one caret quad in the edit box" );
	assert.equal( carets[0].rect[3], lineHeight, "caret is one text line tall, not the box height" );
	assert.equal( carets[0].rect[1], edit.rect[1], "caret starts at the top of the box (authored VAlign 0)" );
	const glyphs = scene.quads.filter( q => q.texture === fontAtlas.image && inEdit( q ) );
	assert.ok( glyphs.length >= 5, "typed text rendered" );
	assert.ok(
		Math.min( ...glyphs.map( q => q.rect[1] ) ) <= edit.rect[1] + 4,
		"typed text hugs the top of the box like the native line, not the vertical centre"
	);
	f.dispose();
});
