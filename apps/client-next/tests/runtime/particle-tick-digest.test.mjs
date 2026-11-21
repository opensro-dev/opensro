/*
===========================================================================

particle-tick-digest.test.mjs - the particle tick digest's rules and coverage

The catalogue check itself lives in particle-tick-digest-NN.test.mjs,
one effect-path range (shard) per file, so no file runs for minutes
(the release's package step measured the unsplit check at 280 s;
ops #436). What stays here is what needs no catalogue: the comparison
rules (digestDifferences) and the proof that the shard ranges cover
the fixture - every recorded effect in exactly one shard, and a file
on disk for every shard there is. A change that alters tick results
on purpose rewrites the fixture with
`node tools/particle-tick-digest.mjs --write` and names the effects.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import {
	DIGEST_PATH,
	PARTICLE_TICK_SHARD_COUNT,
	digestDifferences,
	particleTickShardExpected
} from "../helpers/particle-tick-digest.mjs";

test("an expanded build passes only additions; a changed or lost recorded effect fails on any build", () => {
	const expected = { "a.efp": "1", "b.efp": "2" }, added = { "a.efp": "1", "b.efp": "2", "c.efp": "3" };
	assert.deepEqual( digestDifferences( expected, added, true ), [] );
	assert.deepEqual( digestDifferences( expected, added, false ), [ "c.efp" ] );
	for ( const expanded of [ true, false ] ) {
		assert.deepEqual( digestDifferences( expected, { "a.efp": "1", "b.efp": "9" }, expanded ), [ "b.efp" ] );
		assert.deepEqual( digestDifferences( expected, { "a.efp": "1" }, expanded ), [ "b.efp" ] );
	}
});

test("the shard ranges cover every recorded effect exactly once, with a file per shard", () => {
	const recorded = JSON.parse( readFileSync( DIGEST_PATH, "utf8" ) ).effects, seen = new Set();
	for ( let shard = 0; shard < PARTICLE_TICK_SHARD_COUNT; shard++ ) {
		const expected = particleTickShardExpected( shard );
		assert.ok(
			Object.keys( expected ).length > 0,
			`shard ${shard + 1} of ${PARTICLE_TICK_SHARD_COUNT} records nothing`
		);
		for ( const path of Object.keys( expected ) ) {
			assert.ok( !seen.has( path ), `${path} is in two shards` );
			seen.add( path );
		}
	}
	assert.equal( seen.size, Object.keys( recorded ).length );
	const files = readdirSync( new URL( ".", import.meta.url ) )
		.filter( name => /^particle-tick-digest-\d\d\.test\.mjs$/.test( name ) ).sort();
	assert.deepEqual(
		files,
		Array.from(
			{ length: PARTICLE_TICK_SHARD_COUNT },
			( _, i ) => `particle-tick-digest-${String( i + 1 ).padStart( 2, "0" )}.test.mjs`
		)
	);
});
