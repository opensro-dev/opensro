/*
===========================================================================

emote-routing.test.mjs - tests for emote.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { emoteRoute: route, emoteAttachments: attachments, stallAttachments: stall } = await import(
	sourceFileUrl( "src/engine/foundation/animation/emote.ts" ).href
);
test("native COS discriminator requires runtime class, action and complete family/subtype", () => {
	for ( let band = 0; band < 32; band++ ) {
		for ( let action = 0; action < 7; action++ ) {
			const word = (band << 11) | 0x1c6;
			assert.equal( route( false, true, word, action ), action === 1 && (band === 3 || band === 4) ? 0 : null );
			assert.equal( route( false, false, word, action ), null );
			assert.equal( route( true, false, word, action ), action );
		}
	}
	for ( let bit = 1; bit < 11; bit++ ) assert.equal( route( false, true, 0x19c6 ^ (1 << bit), 1 ), null );
});
test("emote hand flags preserve action 2, restore on interruption and honor mounted exit", () => {
	assert.equal( attachments( false, undefined, "emote0", true, false ), true );
	assert.equal( attachments( false, undefined, "emote2", true, false ), false );
	assert.equal( attachments( true, undefined, "emote2", true, true ), true );
	assert.equal( attachments( true, "emote0", undefined, true, false ), false );
	assert.equal( attachments( true, "emote0", undefined, true, true ), true );
	assert.equal( attachments( true, "emote0", "emote2", true, false ), false );
	assert.equal( attachments( false, undefined, "emote0", false, false ), false );
});
test("a living player keeping a stall has empty hands until the stall closes (#3443)", () => {
	assert.equal( stall( true, true, false ), true, "the keeper's greeting waves empty hands" );
	assert.equal( stall( true, false, false ), false, "a closed stall returns the weapon" );
	assert.equal( stall( true, true, true ), false, "a dead keeper is not posed at the stall" );
	assert.equal( stall( false, true, false ), false, "only players keep stalls" );
});
