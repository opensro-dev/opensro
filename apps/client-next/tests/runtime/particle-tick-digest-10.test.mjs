/*
===========================================================================

particle-tick-digest-10.test.mjs - catalogue particle tick digests, shard 10 of 20

One path range of the particle tick digest: the effects whose paths
sort from skill/china/water_resurrection_effect_b.efp up to
skill/europe/cleric_saint_holyword_a.efp. The whole check
(tests/helpers/particle-tick-digest.mjs) is split by effect path so
each file runs in seconds; the release's package step measured the
unsplit file at 280 s (ops #436). particle-tick-digest.test.mjs holds
the comparison rules and proves the ranges cover the fixture.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	digestDifferences,
	particleTickAddsEffects,
	particleTickShardDigests,
	particleTickShardExpected
} from "../helpers/particle-tick-digest.mjs";

// This file's range of the 20 (0-based, particleTickShardIndex).
const SHARD = 9;
// How many differing effects a failure names.
const NAMED_DIFFERENCES = 20;

test("catalogue particle graphs tick to their recorded digests (shard 10 of 20)", () => {
	const expected = particleTickShardExpected( SHARD ), actual = particleTickShardDigests( SHARD );
	const changed = digestDifferences( expected, actual, particleTickAddsEffects() );
	assert.deepEqual(
		changed.slice( 0, NAMED_DIFFERENCES ).map( path =>
			`${path}: ${expected[path] ?? "(missing)"} -> ${actual[path] ?? "(missing)"}`
		),
		[],
		`${changed.length} effects differ`
	);
});
