/*
===========================================================================

alchemy-notices.test.mjs - refused alchemy answers name their reason

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { alchemyNotice } = await import( "../../src/engine/foundation/gameplay/alchemy-notices.ts" );

/*
================
astral refusal
================
*/
test("a refused Astral stone shows the Astral rule (0xB651 [2, 0x24])", () => {
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 2, 0x24 ) )?.key, "UIIT_MSG_STRGERR_ASTRAL" );
});

/*
================
elixir refusal
================
*/
test("a refused elixir reinforcement shows its category 0x12 reason", () => {
	assert.equal( alchemyNotice( 0xb373, Uint8Array.of( 2, 0x14 ) )?.key, "UIIT_MSG_REINFORCERR_INVALID_ITEM_SLOT" );
});

/*
================
compound refusal
================
*/
test("a refused stone compound shows its category 0x17 reason (0xB16F [2, 9])", () => {
	assert.equal( alchemyNotice( 0xb16f, Uint8Array.of( 2, 9 ) )?.key, "UIIT_MSG_ENCHANT_FAILED" );
});

/*
================
dissolve refusal
================
*/
test("a refused dissolve shows its category 0x17 reason (0xB549 [2, 8])", () => {
	assert.equal( alchemyNotice( 0xb549, Uint8Array.of( 2, 8 ) )?.key, "UIIT_MSG_ALCHEMY_NOT_ENOUGH_INVENTORY" );
});

/*
================
silent answers
================
*/
test("a failed magic roll, a success and other frames show no notice", () => {
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 2, 0x23 ) ), null );
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 1, 1, 13 ) ), null );
	assert.equal( alchemyNotice( 0xb06d, Uint8Array.of( 2, 0x24 ) ), null );
	assert.equal( alchemyNotice( 0xb16f, Uint8Array.of( 1, 1 ) ), null );
	assert.equal( alchemyNotice( 0xb16f, Uint8Array.of( 1, 0, 5, 0, 0, 0 ) ), null );
	assert.equal( alchemyNotice( 0xb549, Uint8Array.of( 1 ) ), null );
	assert.equal( alchemyNotice( 0xb16f, Uint8Array.of( 2, 0x40 ) ), null );
});
