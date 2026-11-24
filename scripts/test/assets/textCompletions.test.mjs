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
	assert.equal(
		LATER_UI_TEXT_COMPLETIONS.UIIT_MSG_NASRUN_WEAR_EXTENSION_ITEM_NOT_USE,
		"You cannot use this while you are wearing it."
	);
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

test("the later client's reverse scroll destinations complete the v1.150 table", () => {
	// textuisystem ids 4200..4221 in the v1.696 client (#3903); the
	// v1.150 table ships none of these keys.
	const entries = {};
	completeUiText( entries );
	assert.equal( entries.UIIT_CLT_REVERSE_RETRUN_SCROLL_PARTY_MOVE, "Move to party members." );
	assert.equal( entries.UIIT_CLT_REVERSE_RETRUN_SCROLL_USER_APPOINT, "Move to the user's set location." );
	assert.equal(
		entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE,
		"Cannot move because the user has left the party."
	);
	assert.equal(
		entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE_1,
		"A party member is in a location you cannot move to."
	);
	assert.equal(
		entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_APPOINT_SELECTION_INTRODUCTION,
		"Setting Location: Register current location as user location.\nMovement: Move to registered location."
	);
	assert.equal( entries.UIIT_CLT_REVERSE_RETRUN_SCROLL_USER_APPOINT_BUTTON, "Set location" );
	assert.equal(
		entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_APPOINT_SELECTION_RE_CHECK,
		"Do you want to delete the existing location and register a new location?"
	);
	assert.equal( entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_1, "Failed to save user location." );
	assert.equal( entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_2, "Failed to move to set user location." );
	assert.equal( entries.UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_3, "Cannot set location." );
	// Fill-if-blank only: a shipped row would win over the completion.
	const shipped = { UIIT_CLT_REVERSE_RETRUN_SCROLL_PARTY_MOVE: "Shipped" };
	completeUiText( shipped );
	assert.equal( shipped.UIIT_CLT_REVERSE_RETRUN_SCROLL_PARTY_MOVE, "Shipped" );
});
