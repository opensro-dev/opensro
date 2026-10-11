/*
===========================================================================

alchemy-notices.test.mjs - refused reinforcements name their reason

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
silent answers
================
*/
test("a failed magic roll, a success and other frames show no notice", () => {
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 2, 0x23 ) ), null );
	assert.equal( alchemyNotice( 0xb651, Uint8Array.of( 1, 1, 13 ) ), null );
	assert.equal( alchemyNotice( 0xb06d, Uint8Array.of( 2, 0x24 ) ), null );
});
