/*
===========================================================================

presentation-due.test.mjs - a settled entity's skipped decide changes nothing

The presentation skips decide (actor-motion.ts) for an entity whose last
steady decision still holds (presentation-due.ts). The settle scenario
(tools/perf/presentation/scenarios.mjs) stands a crowd still, then hands
each entity one kind of input. It runs through the presentation twice in
one process, with the production rule and with an always-due rule that
decides for every entity every frame: every frame's outputs must be the
same, and the production run must have skipped.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../.." );
const { SCENARIOS } = await import( "../../tools/perf/presentation/scenarios.mjs" );
const { createPresentationHarness } = await import( "../../tools/perf/presentation/harness.mjs" );
const { motionDue } = await import( "../../src/engine/runtime/characters/presentation-due.ts" );
// The settle scenario's seed, and the share of presented entities the
// production rule must skip: its crowd stands still most of the run.
const SEED = 1;
const SKIPPED_SHARE = .5;

/*
================
replay

Every frame's outputs of one run of the session, encoded, and how often
the rule was asked and answered not due.
================
*/
async function replay( session, rule ) {
	let asked = 0, skipped = 0;
	const harness = await createPresentationHarness( ROOT, session, {
		motionDue: ( retained, stamp ) => {
			asked++;
			const due = rule( retained, stamp );
			if ( !due ) skipped++;
			return due;
		}
	} );
	const frames = [];
	try {
		for ( let i = 0; i < harness.frames; i++ ) frames.push( JSON.stringify( harness.step( i ) ) );
	} finally {
		harness.dispose();
	}
	return { frames, asked, skipped };
}

/*
================
TestSkippedDecideMatchesForced
================
*/
test("a skipped decide presents every frame as deciding every entity does", async () => {
	const session = SCENARIOS.settle( SEED );
	const forced = await replay( session, () => true ), production = await replay( session, motionDue );
	assert.equal( forced.skipped, 0 );
	assert.equal( production.frames.length, forced.frames.length );
	for ( let i = 0; i < forced.frames.length; i++ ) {
		assert.equal( production.frames[i], forced.frames[i], `frame ${i}` );
	}
	assert.equal( production.asked, forced.asked );
	assert.ok(
		production.skipped > production.asked * SKIPPED_SHARE,
		`${production.skipped} of ${production.asked} presentations skipped decide`
	);
});
