/*
===========================================================================

brightnessText.test.mjs - the brightness page's text ships translated

The video page's brightness spin (#3908) labels its five steps and the
section from the later client's text tables; v1.150 ships none of the
rows, so the completion is the only source. The completion must fill a
missing or untranslated value and never overwrite shipped English.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { BRIGHTNESS_TEXT_COMPLETIONS, completeUiText } from "../../build/shared/textResources.mjs";

test("the completion supplies every brightness row verbatim", () => {
	const entries = {};
	completeUiText( entries );
	for ( const [key, english] of Object.entries( BRIGHTNESS_TEXT_COMPLETIONS ) ) {
		assert.equal( entries[key], english, key );
	}
	assert.equal( entries.UILM_TEXT_BRIGHT_FORTH, "Bright" );
	assert.equal( entries.UILM_TEXT_BRIGHT_THIRD, "Normal" );
	// The later client's own title reads "Set Brigntness"; the typo is fixed.
	assert.equal( entries.UILM_TEXT_BRIGHT, "Set Brightness" );
	assert.equal( entries.UIIT_STT_LUMINOSITY, "Set Brightness" );
});
test("shipped English wins over the completion", () => {
	const entries = { UILM_TEXT_BRIGHT_FIRST: "Very dark (retail)" };
	completeUiText( entries );
	assert.equal( entries.UILM_TEXT_BRIGHT_FIRST, "Very dark (retail)" );
});
test("the completion map is frozen", () => {
	assert.ok( Object.isFrozen( BRIGHTNESS_TEXT_COMPLETIONS ) );
});
