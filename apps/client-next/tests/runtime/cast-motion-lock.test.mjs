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
const ICE_GUARD_ACTION_MS = 2000;
const UNCATALOGUED = 999;

/*
================
skill

A complete catalogue row with an optional action window.
================
*/
/** @param {number} id @param {number} [actionMs] @returns {import("../../src/engine/foundation/gameplay/skill-catalog.ts").SkillMetadata} */
function skill( id, actionMs ) {
	const none = { ID: 0, Level: 0 };
	return {
		id,
		group: 1,
		level: 1,
		name: "SKILL_" + id,
		spCost: 0,
		trainable: false,
		targetRequired: false,
		cooldownMs: 0,
		masteries: [ none, none ],
		prerequisites: [ none, none, none ],
		...(actionMs === undefined ? {} : { actionMs })
	};
}

/*
================
cast

A complete cast record opened at receivedAtMs.
================
*/
/** @param {Partial<import("../../src/engine/contracts/gameplay.ts").CastState>} fields @returns {import("../../src/engine/contracts/gameplay.ts").CastState} */
function cast( fields ) {
	return {
		token: 1,
		caster: LOCAL,
		skill: ICE_GUARD,
		target: 0,
		damage: 0,
		fatal: false,
		receivedAtMs: 0,
		...fields
	};
}

/*
================
lockWith
================
*/
function lockWith() {
	const lock = createCastMotionLock();
	lock.catalog( [ skill( ICE_GUARD, ICE_GUARD_ACTION_MS ), skill( 1 ) ] );
	return lock;
}

test("a self buff holds the caster for its whole action window, not the object-action count", () => {
	const lock = lockWith(), opened = cast( { receivedAtMs: 1000 } );
	// A self buff queues no object command: the old count gate let the walk through.
	assert.equal( lock.locked( [ opened ], LOCAL, 1000, false ), true );
	assert.equal( lock.locked( [ opened ], LOCAL, 2999, false ), true );
	assert.equal( lock.locked( [ opened ], LOCAL, 3000, false ), false, "the click walks when the action ends" );
});

test("a cancelled or another actor's cast never holds the local walk", () => {
	const lock = lockWith();
	assert.equal( lock.locked( [ cast( { cancelledAtMs: 10 } ) ], LOCAL, 20, true ), false );
	assert.equal( lock.locked( [ cast( { caster: 8 } ) ], LOCAL, 20, true ), false );
	assert.equal( lock.locked( [ cast( { resultOnly: true } ) ], LOCAL, 20, true ), false );
});

test("a skill without a known window keeps the committed-command fallback", () => {
	const lock = lockWith(), open = cast( { skill: UNCATALOGUED } );
	assert.equal( lock.locked( [ open ], LOCAL, 50000, true ), true );
	assert.equal( lock.locked( [ open ], LOCAL, 50000, false ), false );
	lock.clear();
	assert.equal( lock.locked( [ cast( {} ) ], LOCAL, 10, false ), false );
});

test("a Force wall's cast roots its caster until the wall retires", () => {
	// SKILL_CH_COLD_BINGBYEOK_A_01 (Crystal Wall): its WAIT is never released.
	const CRYSTAL_WALL = 99;
	const lock = createCastMotionLock();
	lock.catalog( [ { ...skill( CRYSTAL_WALL, 1500 ), holdsCaster: true } ] );
	const wall = cast( { skill: CRYSTAL_WALL, receivedAtMs: 0 } );
	assert.ok( lock.locked( [ wall ], LOCAL, 60_000, false ), "the wall's caster walked a minute in" );
	assert.ok(
		!lock.locked( [ { ...wall, cancelledAtMs: 60_000 } ], LOCAL, 60_001, false ),
		"the wall's retirement left the caster rooted"
	);
});
