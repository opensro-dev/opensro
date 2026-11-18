/*
===========================================================================

windup-trade.test.mjs - a windup object waits for the mover that trades it

Strong Bow (SKILL_CH_BOW_POWER_A, record 87) holds an arrow in the right
hand in READY (command slot 1) and its SHOT mover trades slot 1. Released
at the server's release time, the hand arrow was gone before the mover's
SHOT callback and the shot cut and reappeared.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { windupAwaitsTrade } = await import( "../../src/engine/runtime/characters/effects/windup-trade.ts" );

const hand = { phase: "READY", action: "AT_LOOP", count: 1, native: { slot: 1, trade: 0 } };
const mover = { phase: "SHOT", action: "AT_MOV_1TAR", count: 1, native: { slot: 0, trade: 1 } };

test("a windup a SHOT mover trades is held for the trade", () => {
	assert.equal( windupAwaitsTrade( /** @type {any} */ ([ hand, mover ]), 1 ), true );
});

test("a windup no mover trades is released as before", () => {
	const fresh = { ...mover, native: { slot: 0, trade: 0 } };
	assert.equal( windupAwaitsTrade( /** @type {any} */ ([ hand, fresh ]), 1 ), false );
	assert.equal( windupAwaitsTrade( /** @type {any} */ ([ hand, mover ]), 2 ), false, "another slot" );
	assert.equal( windupAwaitsTrade( /** @type {any} */ ([ hand, mover ]), 0 ), false, "slot zero names no command" );
	assert.equal( windupAwaitsTrade( undefined, 1 ), false, "no record" );
});

test("only a single distributed SHOT mover trades", () => {
	for (
		const stage of [
			{ ...mover, count: 3 },
			{ ...mover, action: "AT_LOOP" },
			{ ...mover, phase: "WAIT" }
		]
	) {
		assert.equal( windupAwaitsTrade( /** @type {any} */ ([ hand, stage ]), 1 ), false, JSON.stringify( stage ) );
	}
	assert.equal( windupAwaitsTrade( /** @type {any} */ ([ hand, { ...mover, action: "AT_MOV_SPLASH" } ]), 1 ), true );
	assert.equal(
		windupAwaitsTrade( /** @type {any} */ ([ hand, { ...mover, phase: undefined } ]), 1 ),
		true,
		"SHOT is the default phase"
	);
});
