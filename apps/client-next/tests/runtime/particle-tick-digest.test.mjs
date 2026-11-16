/*
===========================================================================

particle-tick-digest.test.mjs - the particle graph tick against its catalogue digest

Every effect of the built catalogue with a particle graph keeps the tick
digest the fixture holds (tests/helpers/particle-tick-digest.mjs): the
graph tick (src/engine/foundation/animation/particle-graph.ts) gives the
same results, bit for bit, in all three scenarios. A change that alters
results on purpose rewrites the fixture with
`node tools/particle-tick-digest.mjs --write` and names the effects.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { particleTickDigests, DIGEST_PATH } from "../helpers/particle-tick-digest.mjs";

test("every catalogue particle graph ticks to its recorded digest", () => {
	const expected = JSON.parse( readFileSync( DIGEST_PATH, "utf8" ) ).effects, actual = particleTickDigests();
	const changed = Object.keys( { ...expected, ...actual } ).filter( path => expected[path] !== actual[path] );
	assert.deepEqual(
		changed.slice( 0, 20 ).map( path =>
			`${path}: ${expected[path] ?? "(missing)"} -> ${actual[path] ?? "(missing)"}`
		),
		[],
		`${changed.length} effects differ`
	);
});
