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
import { COMBAT_FRAME_SHARE, instrument, measure, verifyCombatWorkload } from "../../tools/perf/core/client.mjs";

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
		frame( atMs, cpuMs, elapsedMs = cpuMs, created = 0, combat = { effects: 0, particles: 0 } ) {
			clock = atMs;
			probe.begin();
			probe.worldBegin();
			clock += 1;
			probe.worldMark();
			probe.mark( "world" );
			probe.movement( { atMs, revision: 7, workerAtMs: atMs - 5, workerDebtMs: 0 } );
			probe.characterCount( "cpu-ms", cpuMs );
			probe.characterCount( "pose-created", created );
			probe.characterCount( "effect-actors", combat.effects );
			probe.characterCount( "particles", combat.particles );
			clock = atMs + elapsedMs;
			probe.end();
		}
	};
}

test("rare command incidents survive rounding of per-frame averages", async () => {
	const f = fixture();
	const result = await measure( f.page, "rare-command", 1, async () => {
		// measure excludes two opening frames from its ordinary averages.
		for ( let index = 0; index < 1002; index++ ) f.frame( index * 3, 1, 1, index === 3 ? 1 : 0 );
	} );
	assert.equal( result.counts["pose-created"], 0 );
	assert.equal( result.countTotals["pose-created"], 1 );
	assert.equal( result.countedFrames, 1000 );
});

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
	assert.equal( result.opening.maxCpuMs, 2 );
	assert.equal( result.opening.maxElapsedMs, 80 );
	assert.equal( result.opening.maxIntervalMs, 100 );
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
	assert.equal( second.opening.maxCpuMs, 2 );
	assert.equal( second.opening.startAtMs, 2000 );
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
	assert.equal( next.opening.frames, 1 );
	assert.equal( next.opening.maxCpuMs, 2 );
});

test("opening maxima retain sub-threshold callbacks and pose bursts through the rolling tail", async () => {
	const f = fixture();
	const result = await measure( f.page, "first-turn", 1, async () => {
		f.frame( 100, 3 );
		f.frame( 104, 24, 25, 33 );
		for ( let index = 0; index < 20000; index++ ) f.frame( 130 + index * 3, 2 );
		f.frame( 70000, 40, 40, 60 );
	} );
	assert.equal( result.callbacksOver50Ms, 0 );
	assert.equal( result.opening.startAtMs, 100 );
	assert.equal( result.opening.windowMs, 10000 );
	assert.equal( result.opening.maxCpuMs, 24 );
	assert.equal( result.opening.maxCpuAtMs, 104 );
	assert.equal( result.opening.maxElapsedMs, 25 );
	assert.equal( result.opening.maxPoseCreated, 33 );
	assert.equal( result.opening.maxPoseAtMs, 104 );
	assert.ok( result.opening.endAtMs < 10104 );
	assert.ok( f.target.__benchRows.every( row => row[0] !== 25 ), "opening evidence survives tail rollover" );
});

test("a combat window records when its effects were drawn and fails without them", async () => {
	const f = fixture(), combat = { effects: 3, particles: 40 };
	// Effects for the first second of a three-second window only.
	const result = await measure( f.page, "combat", 1, async () => {
		for ( let index = 0; index < 1002; index++ ) {
			f.frame( index * 3, 1, 1, 0, index * 3 < 1006 ? combat : { effects: 0, particles: 0 } );
		}
	} );
	const { workload } = result;
	assert.equal( workload.frames, 1000 );
	assert.equal( workload.startPageMs, 6, "the window opens on the first counted frame" );
	assert.equal( workload.startWallMs, 12345 + 6 );
	assert.deepEqual( workload.timeline.map( s => s.effectFrames ), [ 334, 0, 0 ] );
	assert.deepEqual(
		workload.timeline.map( s => s.frames ),
		[ 334, 333, 333, 0 ].slice( 0, workload.timeline.length )
	);
	assert.equal( workload.particleFrames, 334 );
	assert.throws(
		() => verifyCombatWorkload( workload, "crowd/drag" ),
		/combat workload missing: effect actors in 334/
	);
	const full = await measure( f.page, "combat-full", 1, async () => {
		for ( let index = 0; index < 1002; index++ ) f.frame( 4000 + index * 3, 1, 1, 0, combat );
	} );
	verifyCombatWorkload( full.workload, "crowd/drag" );
	const short = { ...full.workload, particleFrames: Math.ceil( 1000 * COMBAT_FRAME_SHARE ) - 1 };
	assert.throws( () => verifyCombatWorkload( short, "crowd/drag" ), /particles in 899 of 1000/ );
});
