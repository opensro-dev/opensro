/*
===========================================================================

fps-bench.mjs - frame rate of the real client in the scenarios that matter

Usage:
  node tools/perf/bench/fps-bench.mjs [--seconds N] [--at a,b] [--only a,b]
       [--counts] [--spans] [--cpu] [--heap] [--out DIR] [--trace] [--json FILE]
       [--paced] [--cpu-rate N] [--frame-limit 0|60|120|240] [--shadow-detail 0|1|2]

For each location (--at) resets the scratch character there, boots the dev
client uncapped at 1600x900 (core/client.mjs) and runs the location's
scenarios (scenarios.mjs):

  still   - nothing moves but the world itself;
  drag    - a right-button camera drag;
  move    - walking back and forth;
  skill   - skills and attacks on a live monster;
  cross   - walking across a region boundary until arrival.

Each row reports frames per second, frame-interval percentiles, and the
main thread's frame and world-preparation time per frame. Options add:

  --counts  WebGPU commands per frame (draws, bundles, writes, submits);
            counting slows the frame, so timings are not comparable;
  --spans   the runtime's stage marks ("@stage" ms per frame) and detail
            spans ("stage" ms and "stage n" per frame, such as ui-assembly:
            how often the HUD rebuilds and what a rebuild costs);
  --cpu     a CPU profile per scenario, OUT/<location>-<scenario>.cpuprofile;
  --heap    a sampled allocation profile per scenario (.heapprofile) and
            the allocation rate in the row;
  --trace   a Chrome trace per location, OUT/<location>.json.
  --paced   retain display pacing; the default remains uncapped throughput.
  --cpu-rate N  apply Chrome CPU throttling after warm-up (for example 4).
  --frame-limit N  use the player's presentation limit; default 0 preserves
                  the uncapped throughput benchmark.

Read the captures with tools/perf/analyze (profile.mjs, trace.mjs).

The goal these measure: 500 frames a second in every scenario.

===========================================================================
*/
import { writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { parseOptions } from "../core/report.mjs";
import { frameLimits } from "../../../src/engine/foundation/rendering/video-options.ts";
import { openClient, closeClient, createCaptures, measure, revive } from "../core/client.mjs";
import { keepGoing, drag, walk, approach, fight, cross } from "./scenarios.mjs";

const GOAL_FPS = 500;
const CROSS_LIMIT_MS = 30000;
const SCENARIOS = [ "still", "drag", "move", "skill", "cross" ];
// A quiet Europe field with a measured region crossing, the water ghost
// field outside Jangan (a busier scene), and a field with a live monster:
// the scratch character's skills all need a target.
const LOCATIONS = [ {
	name: "field",
	fixture: MISSION_MOVEMENT_FIXTURES.region_cross,
	scenarios: [ "still", "drag", "move", "cross" ]
}, {
	name: "jangan",
	fixture: {
		id: "fps-bench-jangan-water-ghost-field",
		movementMode: 3,
		start: { regionId: 25511, x: 1110, y: 64, z: 1800 },
		startYawRadians: 0
	},
	scenarios: [ "still", "drag", "move" ]
}, {
	name: "hunt",
	fixture: {
		id: "fps-bench-hunt-field",
		movementMode: 3,
		start: { regionId: 0x62a6, x: 863, y: 20, z: 1746 },
		startYawRadians: 0
	},
	scenarios: [ "skill" ]
} ];
const USAGE = "fps-bench.mjs [--seconds N] [--at a,b] [--only a,b] [--counts] [--spans] [--cpu] [--heap] [--out DIR] " +
	"[--trace] [--json FILE] [--paced] [--cpu-rate N] [--frame-limit 0|60|120|240] [--shadow-detail 0|1|2]";

// Units the character may stand from where a sample expects it (the boot
// fixture start, or its place before a revive) before the sample is rejected.
const REVIVE_TOLERANCE = 50;

/*
================
localPose

The local character's region and position, or null before it is placed.
================
*/
function localPose( page ) {
	return page.evaluate( () => {
		const pose = globalThis.__benchRuntime.gameplay().pose;
		return pose ? { regionId: pose.regionId, x: pose.x, z: pose.z } : null;
	} );
}

/*
================
row
================
*/
function row( result ) {
	const counts = Object.entries( result.counts ).map( ( [k, v] ) => `${k} ${v}` ).join( ", " );
	const allocated = result.allocatedMBs === null ?
		"" :
		`  alloc ${result.allocatedMBs.toFixed( 1 )} MB/s ${
			(result.allocatedMBs * 1024 / result.fps).toFixed( 0 )
		} KB/f`;
	return `${result.name.padEnd( 13 )} ${result.fps.toFixed( 0 ).padStart( 5 )} fps  frame p50 ${
		result.p50.toFixed( 2 )
	} p99 ${result.p99.toFixed( 2 )} max ${result.max.toFixed( 1 )} ms  main ${result.main.toFixed( 2 )} world ${
		result.world.toFixed( 2 )
	} ms/f${allocated}${counts ? "  | " + counts : ""}`;
}

/*
================
drive

The scenario's input, and how long its measured span may last.
================
*/
async function drive( page, name, location, seconds ) {
	if ( name === "still" ) return [ seconds, more => keepGoing( page, more ) ];
	if ( name === "drag" ) return [ seconds, more => drag( page, more ) ];
	if ( name === "move" ) return [ seconds, more => walk( page, more ) ];
	if ( name === "skill" ) {
		await approach( page );
		return [ seconds, more => fight( page, more ) ];
	}
	return [ CROSS_LIMIT_MS, await cross( page, location.fixture ) ];
}

/*
================
session

One location: open the client there and run its scenarios.
================
*/
async function session( options, location, results ) {
	console.log( `Opening ${location.name}: frame limit ${options.frameLimit || "uncapped"}, CPU ${options.cpuRate}x` );
	const client = await openClient( location.fixture, {
		counts: options.counts,
		spans: options.spans,
		uncapped: !options.paced,
		cpuRate: options.cpuRate,
		frameLimit: options.frameLimit,
		shadowDetail: options.shadowDetail
	} );
	try {
		// A stale or replaced session can boot outside the scene; every
		// scenario of this location would then measure the wrong place.
		const booted = await localPose( client.page ), start = location.fixture.start;
		if (
			!booted || booted.regionId !== start.regionId ||
			Math.hypot( booted.x - start.x, booted.z - start.z ) > REVIVE_TOLERANCE
		) throw Error( `${location.name}: the character booted outside the scene (${JSON.stringify( booted )})` );
		const captures = await createCaptures( client.page, {
			dir: options.out,
			cpu: options.cpu,
			heap: options.heap,
			trace: options.trace ? `${options.out}/${location.name}.json` : null
		} );
		for ( const name of location.scenarios ) {
			if ( !options.only.includes( name ) ) continue;
			const before = await localPose( client.page );
			await revive( client.page );
			const after = await localPose( client.page );
			// A revive at the resurrection point (choice 2) can move the
			// character out of the scene; such a sample measures elsewhere.
			if (
				!before || !after || before.regionId !== after.regionId ||
				Math.hypot( before.x - after.x, before.z - after.z ) > REVIVE_TOLERANCE
			) throw Error( `${location.name}/${name}: the character is not where the scene expects after revive` );
			const [ms, input] = await drive( client.page, name, location, options.seconds * 1000 );
			await captures.start();
			const started = Date.now();
			const result = await measure( client.page, `${location.name}/${name}`, ms, input );
			result.frameLimit = options.frameLimit;
			result.cpuRate = options.cpuRate;
			result.shadowDetail = options.shadowDetail;
			const allocated = await captures.stop( `${location.name}-${name}` );
			result.allocatedMBs = allocated === null ? null : allocated / 1048576 / ((Date.now() - started) / 1000);
			results.push( result );
			if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
			console.log( row( result ) );
		}
		await captures.finish();
	} finally {
		await closeClient( client );
	}
}

/*
================
run
================
*/
async function run( options ) {
	const results = [];
	for ( const location of LOCATIONS ) {
		if ( options.at.includes( location.name ) ) await session( options, location, results );
	}
	if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
	const worst = Math.min( ...results.map( r => r.fps ) );
	console.log(
		options.paced || options.frameLimit ?
			`slowest paced scenario ${worst.toFixed( 0 )} fps; see frame-interval percentiles` :
			`slowest scenario ${worst.toFixed( 0 )} fps; goal ${GOAL_FPS} ${worst >= GOAL_FPS ? "MET" : "not met"}`
	);
}

const options = parseOptions( process.argv.slice( 2 ), {
	seconds: 3,
	at: LOCATIONS.map( l => l.name ),
	only: SCENARIOS,
	counts: false,
	paced: false,
	cpuRate: 1,
	frameLimit: 0,
	shadowDetail: 0,
	spans: false,
	cpu: false,
	heap: false,
	trace: false,
	out: "temp/artifacts/fps-bench",
	json: ""
}, USAGE );
if ( !frameLimits().includes( options.frameLimit ) ) throw Error( `unsupported frame limit ${options.frameLimit}` );
for ( const name of options.only ) if ( !SCENARIOS.includes( name ) ) throw Error( `unknown scenario ${name}` );
if ( ![ 0, 1, 2 ].includes( options.shadowDetail ) ) throw Error( `unsupported shadow detail ${options.shadowDetail}` );
await run( options );
