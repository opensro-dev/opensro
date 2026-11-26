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
	// The v1.696 band (textuisystem_4300.txt ids 4200..4221) ships 20 of
	// these keys (#3903); ids 4207 and 4214 are holes there, so the
	// introduction and overwrite-question rows are port-written. The
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

test("the Settings tab titles resolve to each tab's own name (#4512)", () => {
	// The Option window's five tabs, in native order: the routine at
	// 0x5c9eb5..0x5c9fa0 resolves these keys into tab slots 0..4. The
	// v1.150 English cell for the camera tab ships "Set time"; the built
	// catalogue takes the later client's own correction (v1.696
	// textuisystem_800.txt row 796, "View") over it.
	const entries = {
		UIIT_CTL_MENU_VIDEOSET: "Video",
		UIIT_CTL_MENU_AUDIOSET: "Audio",
		UIIT_CTL_MENU_CAMERASET: "Set time",
		UIIT_CTL_MENU_INPUTSET: "Set input",
		UIIT_CTL_MENU_GAMESET: "Set game"
	};
	completeUiText( entries );
	assert.equal( entries.UIIT_CTL_MENU_VIDEOSET, "Video" );
	assert.equal( entries.UIIT_CTL_MENU_AUDIOSET, "Audio" );
	assert.equal( entries.UIIT_CTL_MENU_CAMERASET, "View" );
	assert.equal( entries.UIIT_CTL_MENU_INPUTSET, "Set input" );
	assert.equal( entries.UIIT_CTL_MENU_GAMESET, "Set game" );
	// A catalogue without the row gains none: the correction is not a completion.
	const other = { UIIT_CTL_MENU_VIDEOSET: "Video" };
	completeUiText( other );
	assert.equal( "UIIT_CTL_MENU_CAMERASET" in other, false );
});

test("the later client's party summon lines complete the v1.150 table", () => {
	// The v1.696 band (textuisystem_4600.txt ids 4528-4537, 4552-4563,
	// 4579; textuisystem_5500.txt id 5404) for the party summon window
	// (#3918). Native key spellings (POSSILBE, IMPOSSILBE, MGS) kept.
	const entries = {};
	completeUiText( entries );
	assert.equal( entries.UIIT_PAG_PARTY_SUMMON, "Summon Party member" );
	assert.equal( entries.UIIT_STT_PARTY_SUMMON, "Number of Players Allowed: 1-%d" );
	assert.equal( entries.UIIT_STT_PARTY_SUMMON_PERSONNEL, "Current summoned number : %d" );
	assert.equal( entries.UIIT_STT_PARTY_SUMMON_MEMBER_NAME, "Party Member Name" );
	assert.equal( entries.UIIT_STT_PARTY_SUMMON_ENTRANCE, "Eligibility" );
	assert.equal( entries.UIIT_STT_PARTY_SUMMON_LIMIT_TIME, "Time Limit for Entry" );
	assert.equal( entries.UIIT_CTL_PARTY_SUMMON, "Summon" );
	assert.equal( entries.UIIT_CTL_PARTY_SUMMON_POSSILBE, "Possible" );
	assert.equal( entries.UIIT_CTL_PARTY_SUMMON_IMPOSSILBE, "Impossible" );
	assert.equal(
		entries.UIIT_STT_PARTY_SUMMON_CONFIRM_BOX,
		"[%s] invites you to the world unknown. Would you like to comply?"
	);
	assert.equal(
		entries.UIIT_STT_PARTY_SUMMON_IMPOSSILBE_1,
		"The target character cannot be summoned for entry if the character is outside of a town."
	);
	assert.equal(
		entries.UIIT_STT_PARTY_SUMMON_IMPOSSILBE_8,
		"The target character is dead."
	);
	assert.equal( entries.UIIT_STT_PARTY_SUMMON_WAIT_ERR, "The summoning request is currently pending." );
	assert.equal(
		entries.UIIT_STT_PARTY_SUMMON_BUTTON,
		"You must wait for 5 seconds before summoning again."
	);
	assert.equal( entries.UIIT_MGS_ERR_PARTY_SUMMON_NOT, "You must be a party leader to use it." );
	assert.equal( entries.UIIT_STT_PARTY_SUMMON_LIMIT_COUNT, "Entry Remaining" );
	assert.match( entries.UIIT_STT_STORAGE_EXPANSION_INVENTORY_ADDITION_ITEM_USE, /^Inventory .* 32 rooms/ );
	assert.match( entries.UIIT_STT_STORAGE_EXPANSION_WAREHOUSE_ADDITION_ITEM_USE, /^Personal Storage .* 30 rooms/ );
	// Fill-if-blank only: a shipped row would win over the completion.
	const shipped = { UIIT_PAG_PARTY_SUMMON: "Shipped" };
	completeUiText( shipped );
	assert.equal( shipped.UIIT_PAG_PARTY_SUMMON, "Shipped" );
});
