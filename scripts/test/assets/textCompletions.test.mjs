/*
===========================================================================

textCompletions.test.mjs - the text table's authored completions

Rows the v1.150 textuisystem does not ship are injected only when
blank, so a shipped row always wins. The NASRUN worn line is the
v1.696 client's own row (textuisystem_4200.txt id 4174) for the
Devil's Spirit extension gear refused on the worn Spirit (#3909).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { completeUiText, LATER_UI_TEXT_COMPLETIONS } from "../../build/shared/textResources.mjs";

test("the later client's NASRUN worn line completes the v1.150 table", () => {
	assert.deepEqual( { ...LATER_UI_TEXT_COMPLETIONS }, {
		UIIT_MSG_NASRUN_WEAR_EXTENSION_ITEM_NOT_USE: "You cannot use this while you are wearing it."
	} );
	const entries = {};
	completeUiText( entries );
	assert.equal(
		entries.UIIT_MSG_NASRUN_WEAR_EXTENSION_ITEM_NOT_USE,
		"You cannot use this while you are wearing it."
	);
	// Fill-if-blank only: a shipped row would win over the completion.
	const shipped = { UIIT_MSG_NASRUN_WEAR_EXTENSION_ITEM_NOT_USE: "Shipped" };
	completeUiText( shipped );
	assert.equal( shipped.UIIT_MSG_NASRUN_WEAR_EXTENSION_ITEM_NOT_USE, "Shipped" );
});
