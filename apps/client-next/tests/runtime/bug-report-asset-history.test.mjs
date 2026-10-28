/*
===========================================================================

bug-report-asset-history.test.mjs - the asset queue's history in a report

Drives asset-history.ts with a scripted asset owner and clock, the way a
minutes-long loading stall (#1975) would feed it: one sample per interval
however often the UI frame calls, the oldest samples dropped past the cap,
a bounded job list, and the three stall shapes readable from assets.json.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createAssetHistory, ASSET_SAMPLE_MS, MAX_ASSET_SAMPLES, MAX_ASSET_JOBS } = await import(
	sourceFileUrl( "src/engine/runtime/bug-report/asset-history.ts" ).href
);

/*
================
job
================
*/
function job( id, path, ageMs, state = "loading" ) {
	return { id, path, decode: "bytes", ageMs, state, result: null };
}

/*
================
harness

A scripted asset owner: the test sets the queue and the session phase,
then calls sample() as the UI frame would.
================
*/
function harness() {
	const host = { snapshot: undefined, phase: "world" };
	const history = createAssetHistory( { read: () => host.snapshot, phase: () => host.phase } );
	return { host, history };
}

test("one sample per interval, however often the frame calls", () => {
	const { host, history } = harness();
	host.snapshot = { phase: "running", error: null, available: 8, jobs: [] };
	for ( let nowMs = 0; nowMs < ASSET_SAMPLE_MS * 3; nowMs += 16 ) history.sample( nowMs );
	const doc = history.document( 0, ASSET_SAMPLE_MS * 3 );
	assert.equal( doc.samples.length, 3 );
	// Frames are discrete: each sample lands on the first frame at or past
	// the interval, never earlier and never a whole frame later.
	const gaps = doc.samples.slice( 1 ).map( ( s, i ) => (s.t - doc.samples[i].t) * 1000 );
	for ( const gap of gaps ) assert.ok( gap >= ASSET_SAMPLE_MS && gap < ASSET_SAMPLE_MS + 16, String( gap ) );
});

test("the history keeps the newest MAX_ASSET_SAMPLES and reaches past the journal window", () => {
	const { host, history } = harness();
	host.snapshot = { phase: "running", error: null, available: 8, jobs: [] };
	const count = MAX_ASSET_SAMPLES + 5;
	for ( let i = 0; i < count; i++ ) history.sample( i * ASSET_SAMPLE_MS );
	const doc = history.document( 0, count * ASSET_SAMPLE_MS );
	assert.equal( doc.samples.length, MAX_ASSET_SAMPLES );
	assert.equal( doc.samples[0].t, (5 * ASSET_SAMPLE_MS) / 1000 );
	// The four-minute stall of #1975 fits, with room on either side.
	assert.ok( MAX_ASSET_SAMPLES * ASSET_SAMPLE_MS >= 300000 );
});

test("a long queue is cut to MAX_ASSET_JOBS and says how many were left out", () => {
	const { host, history } = harness();
	const jobs = Array.from( { length: MAX_ASSET_JOBS + 3 }, ( _, i ) => job( i, `/a/${i}.bin`, 1000.4 ) );
	host.snapshot = { phase: "running", error: null, available: 0, jobs };
	history.sample( 0 );
	const queue = history.document( 0, 0 ).samples[0].queue;
	assert.equal( queue.jobs.length, MAX_ASSET_JOBS );
	assert.equal( queue.omittedJobs, 3 );
	assert.equal( queue.jobs[0].ageMs, 1000 );
});

test("the three stall shapes of #1975 are readable from the samples", () => {
	// Slot retention: Restart left the world's loads cancelling in every slot.
	const retained = harness();
	retained.host.phase = "character-select";
	retained.host.snapshot = {
		phase: "running",
		error: null,
		available: 0,
		jobs: [ job( 1, "/world/region-a.bundle", 30000, "cancelling" ) ]
	};
	retained.history.sample( 0 );
	const r = retained.history.document( 0, 0 ).samples[0];
	assert.equal( r.phase, "character-select" );
	assert.equal( r.queue.available, 0 );
	assert.equal( r.queue.jobs[0].state, "cancelling" );

	// A stalled download: the next scene's load ages in place across samples.
	const stalled = harness();
	stalled.host.phase = "character-select";
	for ( let i = 0; i < 3; i++ ) {
		stalled.host.snapshot = {
			phase: "running",
			error: null,
			available: 7,
			jobs: [ job( 9, "/frontend/select.manifest", i * ASSET_SAMPLE_MS ) ]
		};
		stalled.history.sample( i * ASSET_SAMPLE_MS );
	}
	const ages = stalled.history.document( 0, 0 ).samples.map( s => s.queue.jobs[0].ageMs );
	assert.deepEqual( ages, [ 0, ASSET_SAMPLE_MS, ASSET_SAMPLE_MS * 2 ] );

	// The scene never asked: free slots and nothing listed.
	const idle = harness();
	idle.host.phase = "character-select";
	idle.host.snapshot = { phase: "running", error: null, available: 8, jobs: [] };
	idle.history.sample( 0 );
	assert.deepEqual( idle.history.document( 0, 0 ).samples[0].queue.jobs, [] );
});

test("no asset owner yet reads as a null queue, not a failure", () => {
	const { history } = harness();
	history.sample( 0 );
	const doc = history.document( 1000, 0 );
	assert.equal( doc.samples[0].queue, null );
	assert.equal( doc.current.queue, null );
	assert.equal( doc.samples[0].t, -1 );
});
