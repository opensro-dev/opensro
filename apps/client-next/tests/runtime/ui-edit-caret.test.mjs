/*
===========================================================================

ui-edit-caret.test.mjs - a tall single-line edit draws a one-line caret

#2615: in the Chat to all window the caret spanned the whole 36 px edit
box and the typed line sat vertically centred. Native CIFEdit (522B50)
lays its one line out in the client rect by the authored VAlign and
draws the caret 2 px wide over that line box alone. The name-change
window reuses the same frame and edit, so both are exercised here.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";

// A type word isGlobalChatItem accepts (global-chat.ts): TID 3/3/5/2.
const GLOBAL_CHAT_TYPE = 0x29ec;
// The name-change scroll (name-change.ts isNameChangeScrollItem), known
// by its RefObjID; its shipped type word (3/3/13/11) is not isGlobalChatItem's.
const NAME_CHANGE_SCROLL = 3789;
const NAME_CHANGE_TYPE = (3 << 2) | (3 << 5) | (13 << 7) | (11 << 11);
const BAG_SLOT = 13;
// The glyph layout's line box: raw font height + 5 (ui-glyphs.ts).
const LINE_HEIGHT = fontAtlas.fonts["0"].recordHeight + 5;

/*
================
typeInTallEdit

Open the window its item opens, type into its edit and return the last
settled presentation plus the first scene (from the end) whose caret is
in its visible blink phase.
================
*/
/**
 * @param {import("node:test").TestContext} t
 * @param {{ refObjId: number; typeFlags: number }} item
 * @param {string} editId
 */
function typeInTallEdit( t, item, editId ) {
	const f = uiFixture();
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		inventorySlotCount: 45,
		inventory: [ { slot: BAG_SLOT, quantity: 3, ...item } ]
	} );
	let now = 0;
	// A property, not a local: the closure assigns it, and a narrowed local
	// would read as never at the assertions (ui-panel-sweep's note).
	const seen = {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
	};
	const step = () => {
		for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
	};
	step();
	f.ui.event( { kind: "double-activate", id: "slot:" + BAG_SLOT } );
	step();
	f.ui.event( { kind: "edit", id: editId, value: "Hello", start: 5, end: 5, composing: false } );
	step();
	const edit = seen.presentation?.controls.find( control => control.id === editId );
	assert.ok( edit, `${editId} was admitted` );
	assert.ok( edit.rect[3] > LINE_HEIGHT * 2, `${editId} is a tall edit box` );
	const inside = q =>
		q.rect[0] >= edit.rect[0] && q.rect[0] < edit.rect[0] + edit.rect[2] &&
		q.rect[1] >= edit.rect[1] - 1 && q.rect[1] < edit.rect[1] + edit.rect[3];
	const scene = [ ...f.scenes ].reverse().find( s =>
		s.quads.some( q =>
			!q.texture && q.color[0] === 1 && q.color[1] === 1 && q.color[2] === 1 &&
			q.rect[2] >= 1 && q.rect[2] <= 3 && q.rect[3] >= 8 && inside( q )
		)
	);
	assert.ok( scene, `${editId} drew a caret` );
	const caret = scene.quads.find( q =>
		!q.texture && q.color[0] === 1 && q.color[1] === 1 && q.color[2] === 1 &&
		q.rect[2] >= 1 && q.rect[2] <= 3 && q.rect[3] >= 8 && inside( q )
	);
	const glyphs = scene.quads.filter( q => q.texture === fontAtlas.image && inside( q ) );
	assert.ok( glyphs.length > 0, `${editId} drew its typed line` );
	return { edit, caret, glyphs: glyphs.sort( ( a, b ) => a.rect[0] - b.rect[0] ) };
}

/*
================
assertOneLineCaret

The caret is 2 px wide and exactly one line tall at the top of the
authored (VAlign 0) line box, and the typed glyphs sit on that line -
not centred in the tall control.
================
*/
/**
 * @param {string} editId
 * @param {{ rect: readonly number[] }} edit
 * @param {{ rect: readonly number[] }} caret
 * @param {{ rect: readonly number[] }[]} glyphs
 */
function assertOneLineCaret( editId, edit, caret, glyphs ) {
	assert.equal( caret.rect[2], 2, `${editId} caret is 2 px wide` );
	assert.equal( caret.rect[3], LINE_HEIGHT, `${editId} caret is one line tall` );
	assert.equal( caret.rect[1], Math.round( edit.rect[1] ), `${editId} caret tops the line box` );
	const first = glyphs[0];
	assert.ok(
		first.rect[1] - edit.rect[1] <= 4,
		`${editId} typed line sits at the top of its box, not centred`
	);
	assert.ok(
		first.rect[1] + first.rect[3] / 2 >= caret.rect[1] - 2 &&
			first.rect[1] + first.rect[3] / 2 <= caret.rect[1] + LINE_HEIGHT + 2,
		`${editId} typed line and caret share one line box`
	);
}

test("Chat to all: the tall edit's caret is one line tall (#2615)", t => {
	const { edit, caret, glyphs } = typeInTallEdit(
		t,
		{ refObjId: 24600, typeFlags: GLOBAL_CHAT_TYPE },
		"wholechat-text"
	);
	assertOneLineCaret( "wholechat-text", edit, caret, glyphs );
});

test("Name change: the reused frame's caret is one line tall (#2615)", t => {
	const { edit, caret, glyphs } = typeInTallEdit(
		t,
		{ refObjId: NAME_CHANGE_SCROLL, typeFlags: NAME_CHANGE_TYPE },
		"rename-text"
	);
	assertOneLineCaret( "rename-text", edit, caret, glyphs );
});
