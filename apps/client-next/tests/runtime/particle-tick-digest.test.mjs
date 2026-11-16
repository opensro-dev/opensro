/*
===========================================================================

particle-tick-digest.test.mjs - the particle graph tick against its catalogue digest

Every effect of the built catalogue with a particle graph keeps the tick
digest the fixture holds (tests/helpers/particle-tick-digest.mjs): the
graph tick (src/engine/foundation/animation/particle-graph.ts) gives the
same results, bit for bit, in all three scenarios. A change that alters
results on purpose rewrites the fixture with
`node tools/particle-tick-digest.mjs --write` and names the effects.

The fixture is the native catalogue's. A native build matches it exactly;
an expanded build (the Alexandria variant) must keep every recorded
effect's digest, and its added effects pass (digestDifferences says why).
So does a build that read the operator's optional later-client glow tables
(scripts/import_vsro_item_glow.py): they name enchant sparks the native
catalogue never loads (system_enchant_b_01.efp), so the catalogue gains
effects while every recorded one must still match.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { digestDifferences, particleTickDigests, DIGEST_PATH } from "../helpers/particle-tick-digest.mjs";
import { EXPANDED_BUILD } from "../helpers/expanded-build.mjs";
import { vsroItemGlowRoot } from "../../../../scripts/build/char/equipmentGlowMetadata.mjs";

// How many differing effects a failure names.
const NAMED_DIFFERENCES = 20;
// A build that adds effects to the native catalogue: the variant, or the
// optional glow import the asset build reads when it is present.
const ADDS_EFFECTS = EXPANDED_BUILD || existsSync( vsroItemGlowRoot );

test("every catalogue particle graph ticks to its recorded digest", () => {
	const expected = JSON.parse( readFileSync( DIGEST_PATH, "utf8" ) ).effects, actual = particleTickDigests();
	const changed = digestDifferences( expected, actual, ADDS_EFFECTS );
	assert.deepEqual(
		changed.slice( 0, NAMED_DIFFERENCES ).map( path =>
			`${path}: ${expected[path] ?? "(missing)"} -> ${actual[path] ?? "(missing)"}`
		),
		[],
		`${changed.length} effects differ`
	);
});

test("an expanded build passes only additions; a changed or lost recorded effect fails on any build", () => {
	const expected = { "a.efp": "1", "b.efp": "2" }, added = { "a.efp": "1", "b.efp": "2", "c.efp": "3" };
	assert.deepEqual( digestDifferences( expected, added, true ), [] );
	assert.deepEqual( digestDifferences( expected, added, false ), [ "c.efp" ] );
	for ( const expanded of [ true, false ] ) {
		assert.deepEqual( digestDifferences( expected, { "a.efp": "1", "b.efp": "9" }, expanded ), [ "b.efp" ] );
		assert.deepEqual( digestDifferences( expected, { "a.efp": "1" }, expanded ), [ "b.efp" ] );
	}
});
