/*
===========================================================================

presentation-due.test.mjs - a settled entity's skipped resolve and decide change nothing

The presentation skips resolve (actor-appearance.ts) and decide
(actor-motion.ts) for an entity whose last steady result still holds
(presentation-due.ts). The settle scenario (tools/perf/presentation/
scenarios.mjs) stands a crowd still, then hands each entity one kind of
input. It runs through the presentation twice in one process, with the
production rules and with always-due rules that resolve and decide for
every entity every frame: every frame's outputs must be the same, and the
production run must have skipped both.

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
const { PRESENTATION_DUE } = await import( "../../src/engine/runtime/characters/presentation-due.ts" );
// The settle scenario's seed, and the share of presented entities the
// production rule must skip: its crowd stands still most of the run.
const SEED = 1;
const SKIPPED_SHARE = .5;

/*
================
replay

Every frame's outputs of one run of the session, encoded, and how often
each rule was asked and answered not due.
================
*/
async function replay( session, rules ) {
	const asked = { appearance: 0, motion: 0 }, skipped = { appearance: 0, motion: 0 };
	const counted = kind => ( retained, stamp ) => {
		asked[kind]++;
		const due = rules[kind]( retained, stamp );
		if ( !due ) skipped[kind]++;
		return due;
	};
	const harness = await createPresentationHarness( ROOT, session, {
		due: { appearance: counted( "appearance" ), motion: counted( "motion" ) }
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
TestSkippedWorkMatchesForced
================
*/
test("skipped resolves and decides present every frame as doing them for every entity does", async () => {
	const session = SCENARIOS.settle( SEED );
	const always = () => true;
	const forced = await replay( session, { appearance: always, motion: always } ),
		production = await replay( session, PRESENTATION_DUE );
	assert.deepEqual( forced.skipped, { appearance: 0, motion: 0 } );
	assert.equal( production.frames.length, forced.frames.length );
	for ( let i = 0; i < forced.frames.length; i++ ) {
		assert.equal( production.frames[i], forced.frames[i], `frame ${i}` );
	}
	for ( const kind of [ "appearance", "motion" ] ) {
		assert.ok(
			production.skipped[kind] > production.asked[kind] * SKIPPED_SHARE,
			`${production.skipped[kind]} of ${production.asked[kind]} ${kind} rule answers skipped`
		);
	}
});
