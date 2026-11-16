/*
===========================================================================

presentation-due.test.mjs - a settled entity's skipped work changes nothing

The presentation skips resolve (actor-appearance.ts) and decide
(actor-motion.ts) for an entity whose last steady result still holds, and
publishes its kept actor again with only its clocks advanced
(actor-advance.ts; the rules in presentation-due.ts). The settle scenario
(tools/perf/presentation/scenarios.mjs) stands a crowd still, then hands
each entity one kind of input. It runs through the presentation twice in
one process, with the production rules and with always-due rules that
resolve, decide and build the actor for every entity every frame: every
frame's outputs must be the same, and the production run must have
skipped all three.

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
const { createActorSnapshots } = await import( "../../src/engine/runtime/renderer/characters/actor-snapshots.ts" );
const { encodeCrowdValue } = await import( "../../tools/perf/core/crowd-capture.mjs" );
// The settle scenario's seed, and the share of presented entities the
// production rule must skip: its crowd stands still most of the run.
const SEED = 1;
const SKIPPED_SHARE = .5;
// The share of renderer copies that must copy only clocks: fading copies
// and actors whose opacity is set after publication are not frozen, and
// are copied in full on every frame.
const CLOCK_COPY_SHARE = .6;
// An own key holding undefined reads as an absent one (no renderer reader
// enumerates a row's keys): the one difference the two owners' attachment
// paths may leave, an in-place copy writing every optional field.
const UNDEFINED = JSON.stringify( encodeCrowdValue( undefined ) );
const rowText = rows =>
	JSON.stringify(
		encodeCrowdValue( rows ),
		( key, value ) => key && JSON.stringify( value ) === UNDEFINED ? undefined : value
	);

/*
================
freezeDeep

The publication contract a frozen actor makes (actor-advance.ts): nothing
it references changes after it is published but its clocks (layers and
the model animation frame, which the renderer copies every frame). Every
object a frozen published actor reaches is frozen here, so a producer that
changes one later throws, and the presentation reports the frame's error.
================
*/
function freezeDeep( actor ) {
	if ( !Object.isFrozen( actor ) ) return;
	const walk = value => {
		if ( value === null || typeof value !== "object" || ArrayBuffer.isView( value ) || Object.isFrozen( value ) ) {
			return;
		}
		Object.freeze( value );
		for ( const nested of Object.values( value ) ) walk( nested );
	};
	for ( const [key, value] of Object.entries( actor ) ) {
		if ( key !== "layers" && key !== "modelAnimation" ) walk( value );
	}
}

/*
================
replay

Every frame's outputs of one run of the session, encoded, and how often
each rule was asked and answered not due. The published actors also enter
two of the renderer's snapshot owners, one that copies only the clocks of
a frozen actor whose other fields did not change and one that copies every
actor in full: every frame's rows must be the same (rowText).
================
*/
async function replay( session, rules ) {
	const reused = createActorSnapshots(), full = createActorSnapshots( false );
	let rowsDiffer = -1, frame = 0;
	const asked = { appearance: 0, motion: 0, record: 0 }, skipped = { appearance: 0, motion: 0, record: 0 };
	const counted = kind => ( retained, stamp ) => {
		asked[kind]++;
		const due = rules[kind]( retained, stamp );
		if ( !due ) skipped[kind]++;
		return due;
	};
	const harness = await createPresentationHarness( ROOT, session, {
		due: { appearance: counted( "appearance" ), motion: counted( "motion" ), record: counted( "record" ) },
		actors: actors => {
			for ( const actor of actors ) freezeDeep( actor );
			const a = rowText( reused.update( actors ) ), b = rowText( full.update( actors ) );
			if ( a !== b && rowsDiffer < 0 ) rowsDiffer = frame;
		}
	} );
	const frames = [];
	try {
		for ( ; frame < harness.frames; frame++ ) frames.push( JSON.stringify( harness.step( frame ) ) );
	} finally {
		harness.dispose();
	}
	return { frames, asked, skipped, rowsDiffer, copies: reused.stats() };
}

/*
================
TestSkippedWorkMatchesForced
================
*/
test("skipped resolves, decides and actor builds present every frame as doing them all does", async () => {
	const session = SCENARIOS.settle( SEED );
	const always = () => true;
	const forced = await replay( session, { appearance: always, motion: always, record: always } ),
		production = await replay( session, PRESENTATION_DUE );
	assert.deepEqual( forced.skipped, { appearance: 0, motion: 0, record: 0 } );
	assert.equal( production.frames.length, forced.frames.length );
	for ( let i = 0; i < forced.frames.length; i++ ) {
		assert.equal( production.frames[i], forced.frames[i], `frame ${i}` );
		assert.equal( JSON.parse( production.frames[i] ).reads.error, null, `frame ${i} error` );
	}
	assert.equal( production.rowsDiffer, -1, `renderer rows differ from frame ${production.rowsDiffer}` );
	assert.equal( forced.rowsDiffer, -1, `renderer rows differ from frame ${forced.rowsDiffer}` );
	const { copies, clockCopies } = production.copies;
	assert.ok(
		clockCopies > (copies + clockCopies) * CLOCK_COPY_SHARE,
		`${clockCopies} of ${copies + clockCopies} copied clocks only`
	);
	for ( const kind of [ "appearance", "motion", "record" ] ) {
		assert.ok(
			production.skipped[kind] > production.asked[kind] * SKIPPED_SHARE,
			`${production.skipped[kind]} of ${production.asked[kind]} ${kind} rule answers skipped`
		);
	}
});
