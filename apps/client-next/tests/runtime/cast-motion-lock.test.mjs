/*
===========================================================================

cast-motion-lock.test.mjs - the caster cannot walk out of a skill's action

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCastMotionLock } = await import( "../../src/engine/foundation/gameplay/cast-motion-lock.ts" );

const LOCAL = 7;
// SKILL_CH_COLD_GANGGI_A_01 (Weak guard of ice): 1000 ms cast + 1000 ms action.
const ICE_GUARD = 240;
const UNCATALOGUED = 999;

/*
================
lockWith
================
*/
function lockWith() {
	const lock = createCastMotionLock();
	lock.catalog( [ { id: ICE_GUARD, actionMs: 2000 }, { id: 1, actionMs: undefined } ] );
	return lock;
}

test("a self buff holds the caster for its whole action window, not the object-action count", () => {
	const lock = lockWith(), cast = { token: 1, caster: LOCAL, skill: ICE_GUARD, receivedAtMs: 1000 };
	// A self buff queues no object command: the old count gate let the walk through.
	assert.equal( lock.locked( [ cast ], LOCAL, 1000, false ), true );
	assert.equal( lock.locked( [ cast ], LOCAL, 2999, false ), true );
	assert.equal( lock.locked( [ cast ], LOCAL, 3000, false ), false, "the click walks when the action ends" );
});

test("a cancelled or another actor's cast never holds the local walk", () => {
	const lock = lockWith();
	assert.equal(
		lock.locked( [ { caster: LOCAL, skill: ICE_GUARD, receivedAtMs: 0, cancelledAtMs: 10 } ], LOCAL, 20, true ),
		false
	);
	assert.equal( lock.locked( [ { caster: 8, skill: ICE_GUARD, receivedAtMs: 0 } ], LOCAL, 20, true ), false );
	assert.equal(
		lock.locked( [ { caster: LOCAL, skill: ICE_GUARD, receivedAtMs: 0, resultOnly: true } ], LOCAL, 20, true ),
		false
	);
});

test("a skill without a known window keeps the committed-command fallback", () => {
	const lock = lockWith(), cast = { caster: LOCAL, skill: UNCATALOGUED, receivedAtMs: 0 };
	assert.equal( lock.locked( [ cast ], LOCAL, 50000, true ), true );
	assert.equal( lock.locked( [ cast ], LOCAL, 50000, false ), false );
	lock.clear();
	assert.equal( lock.locked( [ { caster: LOCAL, skill: ICE_GUARD, receivedAtMs: 0 } ], LOCAL, 10, false ), false );
});
