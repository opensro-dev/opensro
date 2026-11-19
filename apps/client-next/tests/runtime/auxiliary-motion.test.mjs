/*
===========================================================================

auxiliary-motion.test.mjs - tests for auxiliary-motion.ts (#3564)

A wing dress's wings follow the owner's gait. A cast held them on "stand"
by mere membership in gameplay.casts, which keeps a self buff's cast for
the buff's whole duration and a completed cast until its teardown, so the
wings stopped while the body ran on. Only a cast whose action layers still
play holds them.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { castAnimating, auxiliaryLocomotion } = await import(
	"../../src/engine/foundation/animation/auxiliary-motion.ts"
);

/** @type {import("../../src/engine/foundation/animation/auxiliary-motion.ts").AuxiliaryMotionInput} */
const RUNNING = { mounted: false, dead: false, casting: false, moving: true, gait: "run" };
// A cast row as gameplay.casts holds it; castAnimating reads only cancelledAtMs.
const CAST = { token: 7, cancelledAtMs: undefined };
const CANCELLED = { token: 7, cancelledAtMs: 1200 };

test("a retained cast with no playing action layers no longer counts as casting", () => {
	// The self buff's cast after its animation: still in gameplay.casts,
	// uncancelled, no action layers.
	assert.equal( castAnimating( CAST, 0 ), false );
	assert.equal( castAnimating( CAST, 1 ), true );
	assert.equal( castAnimating( CANCELLED, 1 ), false );
	assert.equal( castAnimating( undefined, 1 ), false );
});

test("the wings run with a running owner whose buff cast is only retained", () => {
	const retained = castAnimating( CAST, 0 );
	assert.equal( auxiliaryLocomotion( { ...RUNNING, casting: retained } ), "run" );
});

test("a playing cast, a rider and a corpse stand; an idle owner stands; a walker asks for walk", () => {
	assert.equal( auxiliaryLocomotion( { ...RUNNING, casting: true } ), "stand" );
	assert.equal( auxiliaryLocomotion( { ...RUNNING, mounted: true } ), "stand" );
	assert.equal( auxiliaryLocomotion( { ...RUNNING, dead: true } ), "stand" );
	assert.equal( auxiliaryLocomotion( { ...RUNNING, moving: false } ), "stand" );
	assert.equal( auxiliaryLocomotion( { ...RUNNING, gait: "walk" } ), "walk" );
});
