/*
===========================================================================

presentation-due-scene.test.mjs - skipped resolves and decides change nothing, on an invented scene

presentation-due.test.mjs proves this on the replay tool's settle scenario,
which reads the built game assets. This file proves it with none: the
scene of tests/helpers/presentation-scene.mjs (invented bodies, items and
the smallest GLBs the decoder admits) is served to the replay harness
through its source hook. A standing crowd stays settled to the end; one
peer each walks, changes its equipment, sits and stands, dies, rides a
horse, takes a freeze and a slow, leaves view for one frame, attacks a
monster, emotes, picks up, hosts an attached motion, or wears what an
arena team or a job-suit rule changes (sceneSession lists them). The
scene runs with the production rules (presentation-due.ts)
and with always-due rules: every frame's outputs must be the same, the
forced run must skip nothing and the production run must skip most of
both rules' answers.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../.." );
const { sceneSession, sceneSources } = await import( "../helpers/presentation-scene.mjs" );
const { createPresentationHarness } = await import( "../../tools/perf/presentation/harness.mjs" );
const { PRESENTATION_DUE } = await import( "../../src/engine/runtime/characters/presentation-due.ts" );
const SEED = 1;
// The share of rule answers the production run must skip: the crowd
// stands still for most of the scene.
const SKIPPED_SHARE = .5;

/*
================
replay

Every frame's outputs of one run of the scene, encoded; how often each
rule was asked and answered not due; and the presentation errors seen.
================
*/
async function replay( rules ) {
	const asked = { appearance: 0, motion: 0 }, skipped = { appearance: 0, motion: 0 };
	const counted = kind => ( retained, stamp ) => {
		asked[kind]++;
		const due = rules[kind]( retained, stamp );
		if ( !due ) skipped[kind]++;
		return due;
	};
	const sources = sceneSources();
	const harness = await createPresentationHarness( ROOT, sceneSession( SEED ), {
		due: { appearance: counted( "appearance" ), motion: counted( "motion" ) },
		source: pathname => sources.get( pathname ) ?? null
	} );
	const frames = [], errors = new Set();
	try {
		for ( let i = 0; i < harness.frames; i++ ) {
			const frame = harness.step( i );
			if ( frame.reads.error !== null ) errors.add( frame.reads.error );
			frames.push( JSON.stringify( frame ) );
		}
	} finally {
		harness.dispose();
	}
	return { frames, asked, skipped, errors: [ ...errors ] };
}

/*
================
TestSceneSkippedWorkMatchesForced
================
*/
test("on the invented scene, skipped resolves and decides present every frame as doing them all does", async () => {
	const always = () => true;
	const forced = await replay( { appearance: always, motion: always } ),
		production = await replay( PRESENTATION_DUE );
	assert.deepEqual( forced.errors, [] );
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
