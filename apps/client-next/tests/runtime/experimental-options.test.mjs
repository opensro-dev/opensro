/*
===========================================================================

experimental-options.test.mjs - explicit opt-in and draft cancellation

Browser additions stay off until a valid saved preference or Confirm enables
them. Opening again discards changes that were never confirmed.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createExperimentalHud } = await import( "../../src/engine/runtime/ui/hud/experimental-hud.ts" );

test("only an explicit boolean enables chat timestamps", () => {
	for ( const value of [ undefined, null, [], {}, true, { chatTimestamps: "true" }, { chatTimestamps: 1 } ] ) {
		assert.equal( experimentalOptions( value ).chatTimestamps, false );
	}
	assert.equal( experimentalOptions( { chatTimestamps: true } ).chatTimestamps, true );
});

test("experimental drafts require Confirm and Default only changes the draft", () => {
	const hud = createExperimentalHud();
	assert.equal( hud.state().saved.chatTimestamps, false );
	hud.open();
	hud.toggleChatTimestamps();
	assert.equal( hud.state().draft.chatTimestamps, true );
	assert.equal( hud.state().saved.chatTimestamps, false );
	hud.open();
	assert.equal( hud.state().draft.chatTimestamps, false );
	hud.toggleChatTimestamps();
	assert.deepEqual( hud.confirm(), { chatTimestamps: true } );
	hud.reset();
	assert.equal( hud.state().draft.chatTimestamps, false );
	assert.equal( hud.state().saved.chatTimestamps, true );
	hud.open();
	assert.equal( hud.state().draft.chatTimestamps, true );
	hud.reset();
	assert.deepEqual( hud.confirm(), { chatTimestamps: false } );
	hud.restore( { chatTimestamps: true } );
	assert.deepEqual( hud.state(), { saved: { chatTimestamps: true }, draft: { chatTimestamps: true } } );
});
