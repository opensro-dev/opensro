/*
===========================================================================

perf-frame-evidence.test.mjs - bounded whole-window long-frame evidence

Exercise the benchmark's actual serialized installer and measurement boundary.
Early incidents must survive the rolling ordinary frame tail. CPU work, GPU
wait and scheduling gaps remain distinct observations.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { createContext, runInContext } from "node:vm";
import { instrument, measure } from "../../tools/perf/core/client.mjs";

/*
================
fixture

The fake page executes the same self-contained functions Playwright serializes.
Only the clock and browser globals are supplied by the test.
================
*/
function fixture() {
	let clock = 0;
	const target = /** @type {any} */ ({ performance: { now: () => clock, timeOrigin: 12345 } });
	const context = createContext( target );
	runInContext( `(${instrument.toString()})({ counts: false, spans: true })`, context );
	const probe = target.__worldProbeFrameProfiler;
	return {
		target,
		page: { evaluate: async fn => runInContext( `(${fn.toString()})()`, context ) },
		/*
		================
		frame
		================
		*/
		frame( atMs, cpuMs, elapsedMs = cpuMs ) {
			clock = atMs;
			probe.begin();
			probe.worldBegin();
			clock += 1;
			probe.worldMark();
			probe.mark( "world" );
			probe.movement( { atMs, revision: 7, workerAtMs: atMs - 5, workerDebtMs: 0 } );
			probe.characterCount( "cpu-ms", cpuMs );
			clock = atMs + elapsedMs;
			probe.end();
		}
	};
}

test("an opening long callback survives more frames than the ordinary retained tail", async () => {
	const f = fixture();
	const result = await measure( f.page, "opening", 1, async () => {
		f.frame( 0, 64 );
		for ( let index = 0; index < 20000; index++ ) f.frame( 70 + index * 3, 2 );
	} );
	assert.equal( result.callbacksOver50Ms, 1 );
	assert.equal( result.timeOriginMs, 12345 );
	assert.equal( result.intervalsOver50Ms, 1 );
	assert.equal( result.longFrames[0].atMs, 0 );
	assert.equal( result.longFrames[0].cpuMs, 64 );
	assert.equal( result.longFrames[0].revision, 7 );
	assert.equal( result.longFrames[0].workerAtMs, -5 );
	assert.equal( result.longFrames[0].counts["@world"], 1 );
	assert.equal( result.longFrames[0].counts["cpu-ms"], 64, "later tallies cannot overwrite the witness" );
	assert.ok( f.target.__benchRows.every( row => row[0] === 2 ), "opening frame has left the ordinary tail" );
});

test("GPU wait and scheduling gaps do not become CPU-heavy callbacks", async () => {
	const f = fixture();
	const result = await measure( f.page, "wait", 1, async () => {
		f.frame( 0, 2, 80 );
		f.frame( 100, 2 );
	} );
	assert.equal( result.callbacksOver50Ms, 0 );
	assert.equal( result.intervalsOver50Ms, 1 );
	assert.equal( result.longFrames.length, 1 );
	assert.equal( result.longFrames[0].atMs, 100 );
	assert.equal( result.longFrames[0].intervalMs, 100 );
	assert.equal( result.longFrames[0].cpuMs, 2 );
});

test("witness storage stays bounded while totals cover every long callback", async () => {
	const f = fixture();
	const result = await measure( f.page, "overload", 1, async () => {
		for ( let index = 0; index < 100; index++ ) f.frame( index * 80, 60 );
	} );
	assert.equal( result.callbacksOver50Ms, 100 );
	assert.equal( result.intervalsOver50Ms, 99 );
	assert.equal( result.longFrames.length, 32 );
	assert.equal( result.longFrames[0].atMs, 0 );
	assert.equal( result.longFrames.at( -1 ).atMs, 31 * 80 );
});

test("warmup and previous measurement windows do not contaminate the next window", async () => {
	const f = fixture();
	f.frame( 0, 80 );
	const first = await measure( f.page, "first", 1, async () => f.frame( 100, 70 ) );
	f.frame( 1000, 80 );
	const second = await measure( f.page, "second", 1, async () => {
		f.frame( 2000, 2 );
		f.frame( 2004, 2 );
	} );
	assert.equal( first.callbacksOver50Ms, 1 );
	assert.equal( second.callbacksOver50Ms, 0 );
	assert.equal( second.intervalsOver50Ms, 0 );
	assert.equal( second.longFrames.length, 0 );
	assert.equal( f.target.__benchLoop, false );
});

test("a frame in flight from the previous window cannot become the next windows incident", async () => {
	const f = fixture(), probe = f.target.__worldProbeFrameProfiler;
	await measure( f.page, "previous", 1, async () => {
		probe.begin();
		probe.characterCount( "cpu-ms", 80 );
	} );
	const next = await measure( f.page, "next", 1, async () => {
		probe.end();
		f.frame( 10, 2 );
	} );
	assert.equal( next.callbacksOver50Ms, 0 );
	assert.equal( next.longFrames.length, 0 );
});
