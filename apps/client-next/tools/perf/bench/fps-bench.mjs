/*
===========================================================================

fps-bench.mjs - frame rate of the real client in the scenarios that matter

Usage:
  node tools/perf/bench/fps-bench.mjs [--seconds N] [--at a,b] [--only a,b]
       [--counts] [--spans] [--cpu] [--heap] [--out DIR] [--trace] [--gpu-process] [--json FILE]
       [--screenshot] [--snow] [--frames N] [--frame-ms MS] [--memory] [--gpu-time]
       [--hide ground,sky,objects,characters] [--render-scale 100|75|50]
       [--paced] [--cpu-rate N] [--frame-limit 0|60|120|240] [--shadow-detail 0|1|2]
       [--expect-commit SHA]

For each location (--at) resets the scratch character there, boots the dev
client uncapped at 1600x900 (core/client.mjs) and runs the location's
scenarios (scenarios.mjs):

  still   - nothing moves but the world itself;
  drag    - a right-button camera drag;
  move    - walking back and forth;
  skill   - skills and attacks on a live monster;
  cross   - walking across a region boundary until arrival.

The crowd location (--at crowd, run only when named) stands --crowd N real
peers in a grid in front of the character (core/crowd.mjs crowdLayout):
authenticated characters on real GameWorld sockets, admitted through the
local provisioning endpoint (--provisioning, --token; at most 32), or
attached from a crowd-host.mjs that holds a matching crowd (any size its
accounts file holds; docs/guides/PROFILING.md). --crowd-fixture FILE.json
stands the character and the crowd at that fixture instead of the Europe
field; a held crowd matches only at its own fixture, and --crowd-host URL
names the host to ask (default core/crowd.mjs CROWD_HOST_URL). It measures what
every drawn character costs (#3563); its rows record the crowd's size,
where it came from and its activity.
--act walk=0.5,chat=0.2 [--seed N] makes the crowd live (core/crowd-activity.mjs):
shares of it walk, chat, roam or stay busy (roam while changing clothes
and recasting an imbue) from a seeded script; each row records the
activity, its seed and the server's receipts. Without --act it stands.

Each row reports frames per second, frame-interval percentiles, and the
main thread's frame and world-preparation time per frame. Options add:

  --counts  WebGPU commands per frame (draws, bundles, writes, submits);
            counting slows the frame, so timings are not comparable;
  --spans   the runtime's stage marks ("@stage" ms per frame) and detail
            spans ("stage" ms and "stage n" per frame, such as ui-assembly:
            how often the HUD rebuilds and what a rebuild costs);
  --cpu     a CPU profile per scenario, OUT/<location>-<scenario>.cpuprofile
            (V8 detailed line info on, so profile.mjs --lines can name lines;
            line attribution also needs an unminified build);
  --heap    a sampled allocation profile per scenario (.heapprofile) and
            the allocation rate in the row;
  --trace   a Chrome trace per location, OUT/<location>.json.
  --screenshot  the page as each timed window ends, OUT/<location>-<scenario>.png
            (taken after the window, so it never touches the timed numbers):
            the before/after picture a performance PR carries.
  --snow    holds the client's weather at full snow for the whole run
            (core/scene-evidence.mjs holdSnow; the observer must be a GM,
            SRO_GM_CHARACTERS on the scratch stack).
  --rain    holds full rain instead (weather mode 2), the same way.
  --frames N  after each timed window, a verify window of N frames (one
            every --frame-ms, default 500) with the scenario's input still
            running: OUT/<location>-<scenario>-NN.png beside a record of
            what the client held (camera, each peer's position, chest
            garment and imbue, weather, heap), and a verdict in the row
            that fails the run when the scene was not what it claims
            (the camera turned in a drag, the actors moved, dressed and
            imbued in a busy crowd, it snowed with --snow).
  --memory  the JS heap's start, peak and end MB per timed window, and the
            MB collections freed in it (sampled every 250 ms); with --heap
            the sampled allocation profile names who allocated.
  --gpu-process  CPU time in Chrome's GPU process per frame: after each
            timed window, a second, traced window of the same scenario
            (reduced as it arrives; no trace file); the row gains the
            GPU main thread's CPU and wall ms per frame, every GPU-process
            thread above 2% busy and the frames they divide by, and the
            CPU ms per frame of every page thread above 2% busy: its main
            thread, the simulation, asset and particle workers and the
            compositor, so no thread's cost goes unread. Tracing
            slows frames, so this never touches the timed numbers. The
            cross and combat scenarios run once and get no traced window.
  --hide    switch native graphic options off (ground, sky, objects,
            characters) before measuring: with --gpu-time, what each costs.
  --render-scale  the Experimental render scale (port-only): the scene at
            75% or 50% resolution; with --gpu-time, whether the pass is fill-bound.
  --gpu-time  the GPU's own time per frame: the page opens with the
            renderer's pass timestamps on (gpu-timing=1) and each row gains
            the frames' GPU ms (mean, p50, p99) and the costliest passes
            (core/gpu-time.mjs). The timestamps ride the measured frames, so
            compare a --gpu-time run only with another.
  --paced   retain display pacing; the default remains uncapped throughput.
  --cpu-rate N  apply Chrome CPU throttling after warm-up (for example 4).
  --frame-limit N  use the player's presentation limit; default 0 preserves
                  the uncapped throughput benchmark.
  --expect-commit SHA  fail the run unless the served bundle's stamped
            revision is this commit (core/build-identity.mjs): an A/B side
            served by a stale preview is refused, not measured.
  --expect-assets HASH  fail the run unless the served /assets/manifest.json
            manifestHash starts with HASH: one machine's stale generated
            tree is refused, not measured.
A busy crowd (--act busy=...) is a combat window: the run fails unless at
least COMBAT_FRAME_SHARE of its frames drew effect actors and particles
(core/client.mjs verifyCombatWorkload). Every row records the per-second
workload timeline (workload in --json).

Read the captures with tools/perf/analyze (profile.mjs, trace.mjs).

The goal these measure: 500 frames a second in every scenario, on the
built bundle. The dev server serves unbundled modules and is slower to
start, so its numbers are development readings. A release bundle cannot be
measured (it keeps neither the runtime export nor the frame probe); the
bench bundle is the release build with exactly those two kept:

  pnpm --filter @sro/client-next build:bench
  pnpm --filter @sro/client-next preview:bench
  SRO_PROBE_CLIENT_NEXT_BASE_URL=http://127.0.0.1:4181 node tools/perf/bench/fps-bench.mjs

Every row records what it ran against (identity: dev server or bundle,
origin, served entry URL, separate harness revision, replay capture state).
Both boundaries are recorded; changed or unknown state invalidates the row.

===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { parseOptions } from "../core/report.mjs";
import { frameLimits } from "../../../src/engine/foundation/rendering/video-options.ts";
import { openClient, closeClient, createCaptures, measure, revive, verifyCombatWorkload } from "../core/client.mjs";
import {
	buildIdentity,
	verifyMeasuredIdentity,
	verifyServedAssets,
	verifyServedCommit
} from "../core/build-identity.mjs";
import { keepGoing, drag, walk, approach, fight, cross, loadCombat, combat, strike, localAlive } from "./scenarios.mjs";
import { CROWD_HOST_URL, DEFAULT_ACTIVITY_SEED, obtainCrowd } from "../core/crowd.mjs";
import { BUSY_IMBUE_SKILL, parseActivity } from "../core/crowd-activity.mjs";
import { gpuTimingCursor, gpuTimingSince, summarizeGpuTiming } from "../core/gpu-time.mjs";
import { fpsRow } from "./fps-rows.mjs";
import { assertDrawn, captureFrames, holdSnow, judgeFrames, RAIN_MODE, SNOW_MODE } from "../core/scene-evidence.mjs";
import { cleanupCombat, combatResidue } from "../core/combat-cleanup.mjs";
import { processCosts } from "../core/trace.mjs";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../../.." );
const GOAL_FPS = 500;
const CROSS_LIMIT_MS = 30000;
const SCENARIOS = [ "still", "drag", "move", "skill", "cross", "combat" ];
// A quiet Europe field with a measured region crossing, the water ghost
// field outside Jangan (a busier scene), and a field with a live monster:
// the scratch character's skills all need a target.
// --hide names: the native graphic options a bench may switch off
// (foundation/rendering/graphic-options.ts GRAPHIC_OPTION_*).
const HIDE_MODES = Object.freeze( { ground: 0x13, sky: 0x14, objects: 0x15, characters: 0x16 } );
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
}, {
	// A repeatable heavy fight: the native GM /LOADMONSTER scene (--combat)
	// around the character at the hunt field, measured only while every
	// loaded monster is alive and the server accepts damaging casts.
	name: "combat",
	fixture: {
		id: "fps-bench-combat-field",
		movementMode: 3,
		start: { regionId: 0x62a6, x: 863, y: 20, z: 1746 },
		startYawRadians: 0
	},
	scenarios: [ "combat" ]
}, {
	// Donwhang Stone Cave 4F (0x8001, floor y 420): a room block over three
	// lower floors, where the dungeon room and floor culling decide the frame.
	name: "cave",
	fixture: {
		id: "fps-bench-donwhang-cave-4f",
		movementMode: 3,
		start: { regionId: 0x8001, x: 1299.7, y: 420, z: 202.4 },
		startYawRadians: 0
	},
	scenarios: [ "still", "drag", "move" ]
}, {
	// Qin-Shi Tomb floor 3 (0x8005, the 59 MB floor): a Snake Woman nest
	// field, for the cave-lag profile (#2788). The pose is a shipped nest
	// point, so the character stands where the floor's monsters spawn.
	name: "tomb",
	fixture: {
		id: "fps-bench-qinshi-tomb-f3",
		movementMode: 3,
		start: { regionId: 0x8005, x: 14122.1, y: 667.43, z: -174.86 },
		startYawRadians: 0
	},
	scenarios: [ "still", "drag", "move" ]
}, {
	// A crowd of real peers (--crowd) on the acceptance bench's Europe field:
	// the per-character cost of the renderer, the labels and the actors.
	name: "crowd",
	fixture: MISSION_MOVEMENT_FIXTURES.movement,
	crowd: true,
	scenarios: [ "still", "drag" ]
} ];
const USAGE = "fps-bench.mjs [--seconds N] [--at a,b] [--only a,b] [--counts] [--spans] [--cpu] [--heap] [--out DIR] " +
	"[--trace] [--screenshot] [--gpu-process] [--json FILE] [--paced] [--cpu-rate N] [--frame-limit 0|60|120|240] [--shadow-detail 0|1|2] " +
	"[--combat codename:count:CHAMP|GIANT|NORMAL] [--vulnerable] [--crowd N] [--crowd-fixture FILE.json] " +
	"[--crowd-host URL] [--act walk=SHARE,chat=SHARE,roam=SHARE,busy=SHARE] [--seed N] [--provisioning URL] " +
	"[--token FILE] [--snow] [--rain] [--frames N] [--frame-ms MS] [--memory] [--gpu-time] [--expect-commit SHA] [--expect-assets HASH]";
// --gpu-process trace categories, the fewest that serve processCosts:
// toplevel gives every thread's top-level tasks as complete events with
// their thread time (tdur); blink.user_timing carries the window marks.
// Process and thread names are metadata, recorded whatever the categories.
const GPU_PROCESS_TRACE_CATEGORIES = [ "toplevel", "blink.user_timing" ];
const GPU_WINDOW_START = "fps-bench gpu-process window start";
const GPU_WINDOW_END = "fps-bench gpu-process window end";
// --gpu-time: how long after a window its last timestamp queries resolve.
const GPU_TIMING_SETTLE_MS = 500;
// A GPU-process thread busier than this share of the window is reported.
const GPU_THREAD_BUSY_SHARE = 0.02;
// Scenarios whose input can run a second window: cross ends at its
// destination, and combat's monsters may be dead after the first.
const REPEATABLE_SCENARIOS = [ "still", "drag", "move", "skill" ];

// How long the client may take to publish every admitted peer.
const CROWD_PUBLISH_MS = 30000;

// Units the character may stand from where a sample expects it (the boot
// fixture start, or its place before a revive) before the sample is rejected.
const REVIVE_TOLERANCE = 50;
// The local GameWorld's own view, for counting GM-loaded residue; its snapshot is cached.
const OBSERVATORY_URL = process.env.SRO_BENCH_OBSERVATORY ??
	"http://127.0.0.1:8791/internal/diagnostics/observatory";
const OBSERVATORY_CACHE_MS = 2000;
const OBSERVATORY_TIMEOUT_MS = 10000;

// The GameWorld's transport metrics; its tick count dates the server's start
// (artifacts record minutes since start: a fresh server measures boot state).
const METRICS_URL = process.env.SRO_BENCH_GAMEWORLD_METRICS ?? "http://127.0.0.1:8788/transport/metrics";
const TICKS_PER_MINUTE = 600;

/*
================
serverUptimeMinutes
================
*/
async function serverUptimeMinutes() {
	try {
		const metrics = await (await fetch( METRICS_URL )).json();
		return Math.round( metrics.tick_count / TICKS_PER_MINUTE * 10 ) / 10;
	} catch {
		return null;
	}
}

/*
================
sceneAlive

The scene's monsters still alive anywhere in view (gids). The residue
check: wider than the fight radius, so one that wandered off still counts.
================
*/
function sceneAlive( page, scene ) {
	return page.evaluate( gids => {
		const ids = new Set( gids );
		return globalThis.__benchRuntime.entities().filter( e => ids.has( e.gid ) && e.appearanceState?.[0] !== 2 )
			.map( e => e.gid );
	}, scene.gids );
}

/*
================
sceneDead

The scene gids the client shows dead (life state 2): positive evidence of
a kill, which a monster that only left view range never gives.
================
*/
function sceneDead( page, scene ) {
	return page.evaluate( gids => {
		const ids = new Set( gids );
		return globalThis.__benchRuntime.entities().filter( e => ids.has( e.gid ) && e.appearanceState?.[0] === 2 )
			.map( e => e.gid );
	}, scene.gids );
}

/*
================
recordResidue

Decides what the session left from three sources: the deaths the client
shows (native life state 2), the GameWorld's monster list, and the
client's view. GM-loaded monsters have no nest, so anything alive stays
until a restart. A gid alive in either, or absent from a truncated server
list without a seen death, is residue and fails the run. The record goes
to its own artifact (a rejected window has no result row) and onto each of
the location's results.
================
*/
async function recordResidue( page, scene, location, results, options ) {
	const dead = new Set( [ ...await sceneDead( page, scene ), ...(scene.cleanup?.dead ?? []) ] );
	const inView = new Set( await sceneAlive( page, scene ) );
	const server = await serverMonsters();
	const classified = combatResidue( scene, server );
	// Positive server HP outranks all earlier death observations. A live
	// client observation also prevents a clean report until it reconciles.
	const alive = [ ...new Set( [ ...classified.alive, ...inView ] ) ];
	const unknown = classified.unknown.filter( gid => !dead.has( gid ) && !inView.has( gid ) );
	const unaccounted = classified.unaccounted;
	const residue = {
		codename: scene.codename,
		loaded: scene.count,
		dead: dead.size,
		alive,
		unknown,
		unaccounted,
		cleanup: scene.cleanup
	};
	if ( alive.length || unknown.length || unaccounted ) process.exitCode = 1;
	await mkdir( options.out, { recursive: true } );
	await writeFile( `${options.out}/${location.name}-residue.json`, JSON.stringify( residue, null, 2 ) );
	for ( const result of results ) {
		if ( result.name.startsWith( `${location.name}/` ) ) result.residue = residue;
	}
	if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
	console.log(
		!alive.length && !unknown.length && !unaccounted ?
			`  combat residue: none; all ${scene.count} GM-loaded ${scene.codename} are dead or gone from the server` :
			`  combat residue: ${alive.length} alive, ${unknown.length} unknown, ${unaccounted} unaccounted of ` +
			`${scene.count} GM-loaded ` +
			`${scene.codename}; restart the GameWorld (announce it first) before other measurements`
	);
}

/*
================
serverMonsters

The GameWorld's own monster list (the local observatory), read after its
snapshot cache has turned over so it postdates the window.
================
*/
async function serverMonsters( timeoutMs = OBSERVATORY_TIMEOUT_MS ) {
	const started = Date.now();
	await new Promise( resolve => setTimeout( resolve, Math.min( OBSERVATORY_CACHE_MS + 250, timeoutMs ) ) );
	const remaining = timeoutMs - (Date.now() - started);
	if ( remaining <= 0 ) throw Error( "observatory deadline exceeded before cache expiry" );
	const response = await fetch( OBSERVATORY_URL, {
		headers: { "X-SRO-Local-Diagnostics": "1" },
		signal: AbortSignal.timeout( Math.max( 1, Math.min( OBSERVATORY_TIMEOUT_MS, Math.floor( remaining ) ) ) )
	} );
	if ( !response.ok ) throw Error( `observatory ${response.status}` );
	const population = (await response.json()).population;
	return {
		truncated: !!population.truncated,
		monsters: new Map( population.monsters.map( row => [ row.gid, row ] ) )
	};
}

/*
================
combatScene

--combat codename:count:type (type CHAMP, GIANT or NORMAL); --vulnerable
keeps the character mortal so incoming damage is part of the load.
================
*/
function combatScene( options ) {
	const [codename, count, type] = options.combat.split( ":" );
	if ( !/^MOB_[A-Z0-9_]+$/.test( codename ?? "" ) ) throw Error( `invalid --combat codename ${codename}` );
	const n = Number( count );
	if ( !Number.isInteger( n ) || n < 1 || n > 250 ) throw Error( `invalid --combat count ${count}` );
	if ( ![ "CHAMP", "GIANT", "NORMAL" ].includes( type ) ) throw Error( `invalid --combat type ${type}` );
	return { codename, count: n, type, vulnerable: options.vulnerable };
}

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
crowdInView

Waits until the client publishes every peer of the crowd.
================
*/
async function crowdInView( page, peers ) {
	await page.waitForFunction(
		names => {
			const entities = globalThis.__benchRuntime.entities();
			return names.every( name => entities.some( entity => entity.name === name ) );
		},
		peers.map( peer => peer.character ),
		{ timeout: CROWD_PUBLISH_MS }
	);
}

/*
================
markWindow

The trace µs of the window marks gpuProcessPass placed.
================
*/
function markWindow( events ) {
	const at = name => events.find( e => e.name === name )?.ts;
	const start = at( GPU_WINDOW_START ), end = at( GPU_WINDOW_END );
	if ( !Number.isFinite( start ) || !Number.isFinite( end ) ) {
		throw Error( "gpu-process: the trace lacks its window marks" );
	}
	return { start, end };
}

/*
================
recordProcessEvents

Runs run() under a Chrome trace of GPU_PROCESS_TRACE_CATEGORIES and keeps
only what processCosts and markWindow read, as the events arrive: an
uncapped client's GPU process posts hundreds of thousands of tasks a
second (700 MB of trace JSON in 3 s on an M-series Mac), more than one
string or file read can hold. Returns run()'s result and the events.
================
*/
async function recordProcessEvents( page, run ) {
	const cdp = await page.context().newCDPSession( page );
	const events = [];
	cdp.on( "Tracing.dataCollected", ( { value } ) => {
		for ( const e of value ) {
			if ( e.ph === "M" ) events.push( { ph: e.ph, name: e.name, pid: e.pid, tid: e.tid, args: e.args } );
			else if ( e.ph === "X" ) {
				events.push( { ph: e.ph, pid: e.pid, tid: e.tid, ts: e.ts, dur: e.dur, tdur: e.tdur } );
			} else if ( e.name === GPU_WINDOW_START || e.name === GPU_WINDOW_END ) {
				events.push( { name: e.name, ts: e.ts } );
			}
		}
	} );
	const complete = new Promise( resolve => cdp.once( "Tracing.tracingComplete", resolve ) );
	await cdp.send( "Tracing.start", {
		transferMode: "ReportEvents",
		traceConfig: {
			recordMode: "recordAsMuchAsPossible",
			includedCategories: GPU_PROCESS_TRACE_CATEGORIES,
			excludedCategories: [ "*" ]
		}
	} );
	try {
		return { result: await run(), events };
	} finally {
		await cdp.send( "Tracing.end" );
		await complete;
		await cdp.detach().catch( () => {} );
	}
}

/*
================
drive

The scenario's input, and how long its measured span may last.
================
*/
async function drive( page, name, location, seconds, scene ) {
	if ( name === "combat" ) {
		// measure() keeps no return value; the window's evidence rides the scene.
		return [ seconds, async more => {
			scene.evidence = await combat( page, more, scene );
		} ];
	}
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
verifyWindow

After the timed window, the scenario again under frame capture
(core/scene-evidence.mjs), judged against what this run promises.
================
*/
async function verifyWindow( page, name, location, options, scene, crowd ) {
	const [, input] = await drive( page, name, location, options.seconds * 1000, scene );
	const activity = parseActivity( options.act );
	const frames = await captureFrames( {
		page,
		input,
		count: options.frames,
		intervalMs: options.frameMs,
		dir: options.out,
		prefix: `${location.name}-${name}`,
		imbue: BUSY_IMBUE_SKILL
	} );
	const verdict = judgeFrames( frames, {
		camera: name === "drag",
		actors: !!crowd && activity.walk + activity.roam + activity.busy > 0,
		dress: !!crowd && activity.busy > 0,
		imbue: !!crowd && activity.busy > 0,
		snow: options.snow
	} );
	console.log( `  verify ${location.name}/${name}: ${JSON.stringify( verdict )}` );
	return verdict;
}

/*
================
gpuProcessPass

A second window of the scenario under a minimal trace, kept apart from the
timed window because tracing slows frames. The page marks the span its
frame probe counted (performance.mark at those page times), so the trace
window and the frame count come from the same source as the fps.
================
*/
async function gpuProcessPass( page, name, location, options, scene ) {
	if ( !REPEATABLE_SCENARIOS.includes( name ) ) {
		console.log( `  gpu-process: ${location.name}/${name} runs once; no traced window` );
		return null;
	}
	const [ms, input] = await drive( page, name, location, options.seconds * 1000, scene );
	const { result: traced, events } = await recordProcessEvents( page, async () => {
		const window = await measure( page, `${location.name}/${name} gpu-process`, ms, input );
		await page.evaluate( ( [span, startName, endName] ) => {
			performance.mark( startName, { startTime: span.start } );
			performance.mark( endName, { startTime: span.end } );
		}, [ window.windowMs, GPU_WINDOW_START, GPU_WINDOW_END ] );
		return window;
	} );
	if ( traced.frames < 2 ) throw Error( `${location.name}/${name}: no frame samples in the traced window` );
	const costs = processCosts( events, markWindow( events ), traced.frames );
	const pick = row =>
		row && {
			name: row.name,
			cpuMsPerFrame: row.cpuMsPerFrame,
			wallMsPerFrame: row.wallMsPerFrame,
			busy: row.busy
		};
	return {
		categories: GPU_PROCESS_TRACE_CATEGORIES,
		frames: traced.frames,
		fps: traced.fps,
		spanMs: costs.spanMs,
		main: pick( costs.gpuMain ),
		threads: costs.gpu.filter( row => row.busy > GPU_THREAD_BUSY_SHARE ).map( pick ),
		renderer: pick( costs.renderer ),
		pageThreads: costs.rendererThreads.filter( row => row.busy > GPU_THREAD_BUSY_SHARE ).map( pick )
	};
}

/*
================
session

One location: open the client there and run its scenarios.
================
*/
async function session( options, location, results ) {
	console.log( `Opening ${location.name}: frame limit ${options.frameLimit || "uncapped"}, CPU ${options.cpuRate}x` );
	let scene = null, captures = null, capturing = false, crowd = null;
	const client = await openClient( location.fixture, {
		counts: options.counts,
		spans: options.spans,
		uncapped: !options.paced,
		cpuRate: options.cpuRate,
		frameLimit: options.frameLimit,
		shadowDetail: options.shadowDetail,
		// A profile is attribution: line positions matter, its timing is perturbed anyway.
		lineInfo: options.cpu,
		gpuTiming: options.gpuTime,
		experimental: options.renderScale === 100 ? undefined : { renderScale: options.renderScale }
	} );
	try {
		// A stale or replaced session can boot outside the scene; every
		// scenario of this location would then measure the wrong place.
		const booted = await localPose( client.page ), start = location.fixture.start;
		if (
			!booted || booted.regionId !== start.regionId ||
			Math.hypot( booted.x - start.x, booted.z - start.z ) > REVIVE_TOLERANCE
		) throw Error( `${location.name}: the character booted outside the scene (${JSON.stringify( booted )})` );
		// The combat scene loads once per session, on a fresh isolated server;
		// GM-loaded monsters have no nest and would accumulate across runs.
		if ( location.name === "combat" ) {
			// From here the GM command may have run: until the load returns its
			// gids, a failure of any kind leaves the whole request unaccounted.
			const requested = combatScene( options );
			scene = { ...requested, refObjId: null, gids: [], ambient: null };
			scene = await loadCombat( client.page, requested ).catch( error => {
				if ( error.scene ) scene = error.scene;
				throw error;
			} );
		}
		if ( location.crowd ) {
			// The crowd journals its accounts there before the first admission.
			await mkdir( options.out, { recursive: true } );
			crowd = await obtainCrowd( {
				count: options.crowd,
				fixture: location.fixture,
				act: options.act,
				seed: options.seed,
				hostUrl: options.crowdHost,
				provisioningUrl: options.provisioning,
				tokenPath: options.token,
				journalPath: `${options.out}/crowd-cleanup.json`
			} );
			await crowdInView( client.page, crowd.peers );
		}
		if ( options.snow || options.rain ) await holdSnow( client.page, options.rain ? RAIN_MODE : SNOW_MODE );
		if ( options.hide.length ) {
			// An ablation: the native graphic options' switches turned off
			// (GRAPHIC_OPTION_*), so --gpu-time weighs what they draw.
			await client.page.evaluate( modes => {
				for ( const mode of modes ) globalThis.__benchRuntime.setRenderMode( mode, false );
			}, options.hide.map( name => HIDE_MODES[name] ) );
		}
		// Before any window: a page that presents nothing measures nothing.
		await assertDrawn( client.page, location.name );
		captures = await createCaptures( client.page, {
			dir: options.out,
			cpu: options.cpu,
			heap: options.heap,
			memory: options.memory,
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
			const [ms, input] = await drive( client.page, name, location, options.seconds * 1000, scene );
			const identity = await buildIdentity( client.page );
			console.log(
				`  measuring ${identity.build} ${identity.entry ?? identity.origin}, replay ${identity.replay}; ` +
					`harness ${identity.harnessCommit}, served commit ${identity.servedCommit}, ` +
					`assets ${identity.servedAssets?.manifestHash.slice( 0, 12 ) ?? "none"} ` +
					`(${identity.servedAssets?.generatedAt ?? "unknown"})`
			);
			verifyServedCommit( identity, options.expectCommit );
			verifyServedAssets( identity, options.expectAssets );
			const gpuCursor = options.gpuTime ? await gpuTimingCursor( client.page ) : null;
			await captures.start();
			capturing = true;
			const started = Date.now();
			const result = await measure( client.page, `${location.name}/${name}`, ms, input );
			if ( gpuCursor !== null ) {
				// Late samples resolve a few frames after the window: wait for them.
				await client.page.waitForTimeout( GPU_TIMING_SETTLE_MS );
				const timed = await gpuTimingSince( client.page, gpuCursor );
				result.gpuTime = summarizeGpuTiming( timed.samples );
				result.gpuTimeFailures = {
					failed: timed.failed,
					skipped: timed.skipped,
					omittedPasses: timed.omittedPasses
				};
			}
			if ( options.screenshot ) {
				await mkdir( options.out, { recursive: true } );
				await client.page.screenshot( { path: `${options.out}/${location.name}-${name}.png` } );
			}
			// The page's frame probe fills the intervals; none means the client
			// never called it, as in a release bundle, which compiles it away.
			if ( result.frames < 2 ) {
				throw Error(
					`${location.name}/${name}: no frame samples; benchmark the dev server or a bench bundle (pnpm build:bench)`
				);
			}
			if ( options.counts ) {
				// Which poses ran on the CPU as the window ended, and why: the
				// skeleton work the GPU path did not take (characters.ts stats).
				result.poseEligibility = await client.page.evaluate( () =>
					globalThis.__benchRuntime.characterStats().poseEligibility ?? null
				);
			}
			result.frameLimit = options.frameLimit;
			result.cpuRate = options.cpuRate;
			result.shadowDetail = options.shadowDetail;
			result.identity = identity;
			// The activity's evidence as the window ended: what ran, its seed and
			// what the server answered, so a standing row never reads as a living one.
			if ( crowd ) {
				result.crowd = {
					peers: crowd.peers.length,
					held: !!crowd.held,
					source: crowd.source ?? { kind: "fresh", accounts: crowd.peers.length },
					activity: { ...crowd.activity }
				};
			}
			if ( scene ) {
				result.serverUptimeMinutes = await serverUptimeMinutes();
				result.scene = {
					codename: scene.codename,
					count: scene.count,
					type: scene.type,
					vulnerable: scene.vulnerable,
					refObjId: scene.refObjId,
					gids: scene.gids,
					ambient: scene.ambient
				};
				result.combat = scene.evidence;
			}
			const allocated = await captures.stop( `${location.name}-${name}` );
			capturing = false;
			result.identityAfter = await buildIdentity( client.page, identity.harnessCommit );
			try {
				verifyMeasuredIdentity( identity, result.identityAfter );
			} catch ( error ) {
				// Keep refused evidence separate from valid FPS rows.
				const evidence = {
					valid: false,
					error: String( error ),
					before: identity,
					after: result.identityAfter
				};
				console.error( "Rejected measurement identity:", JSON.stringify( evidence ) );
				await mkdir( options.out, { recursive: true } );
				await writeFile(
					`${options.out}/${location.name}-${name}-identity-rejected.json`,
					JSON.stringify( evidence, null, 2 )
				);
				throw error;
			}
			result.allocatedMBs = allocated === null ? null : allocated / 1048576 / ((Date.now() - started) / 1000);
			result.memory = captures.memorySpan();
			if ( options.frames > 0 ) {
				result.verify = await verifyWindow( client.page, name, location, options, scene, crowd );
				if ( !result.verify.pass ) process.exitCode = 1;
			}
			if ( options.gpuProcess ) {
				result.gpuProcess = await gpuProcessPass( client.page, name, location, options, scene );
			}
			results.push( result );
			if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
			console.log( fpsRow( result ) );
			// A busy crowd is a combat window: it fails unless its frames drew
			// the effects (the row and JSON above keep the evidence).
			if ( crowd && parseActivity( options.act ).busy > 0 ) {
				verifyCombatWorkload( result.workload, `${location.name}/${name}` );
			}
			// The world's resident and visible counts after the window (#4692):
			// static groups (GPU-culled, never walked), walked and walk-visible
			// groups, and the frame's bundle recordings so far. A bundle without
			// the stats prints null.
			const world = await client.page.evaluate( () => {
				const stats = globalThis.__benchRuntime.worldStats?.();
				return stats ?
					{
						residentGroups: stats.residentGroups,
						staticGroups: stats.staticGroups,
						walkedGroups: stats.walkedGroups,
						visibleGroups: stats.visibleGroups,
						bundleRecords: stats.bundleRecords
					} :
					null;
			} );
			console.log( `  verify world ${name}: ${JSON.stringify( world )}` );
			if ( options.snow || options.rain ) {
				// The held mode and the weather owner's drop counters after
				// the window: an A/B's two sides must hold the same weather.
				const held = await client.page.evaluate( () => ({
					mode: globalThis.__benchRuntime.gameplay()?.weather?.mode ?? null,
					drops: globalThis.__benchRuntime.worldStats?.()?.weather ?? null
				}) );
				console.log( `  verify weather ${name}: ${JSON.stringify( held )}` );
			}
		}
	} finally {
		// Rejected windows keep their profiles too. Cleanup is outside every
		// measured window and trace, so its falling monster count cannot score.
		try {
			if ( capturing ) await captures.stop( `${location.name}-rejected` );
			await captures?.finish();
		} catch ( error ) {
			process.exitCode = 1;
			console.log( `  capture finalization failed: ${error?.message ?? error}` );
		}
		// Residue is reported for rejected windows too: GM monsters outlive them.
		// A run that leaves any (or cannot tell) fails, and its artifact says so.
		if ( scene ) {
			await mkdir( options.out, { recursive: true } ).catch( () => {} );
			await writeFile( `${options.out}/${location.name}-combat.json`, JSON.stringify( scene, null, 2 ) )
				.catch( error => {
					process.exitCode = 1;
					console.log( `  combat evidence write failed: ${error?.message ?? error}` );
				} );
			try {
				scene.cleanup = await cleanupCombat( {
					scene,
					read: serverMonsters,
					attack: ( gids, turn ) => strike( client.page, gids, turn ),
					isAlive: () => localAlive( client.page ),
					pause: ms => client.page.waitForTimeout( ms )
				} );
				if ( scene.cleanup.status !== "clean" ) process.exitCode = 1;
			} catch ( error ) {
				process.exitCode = 1;
				scene.cleanup = { status: "error", error: String( error?.message ?? error ) };
			}
			// Never let the residue report replace the window's own exception.
			try {
				await recordResidue( client.page, scene, location, results, options );
			} catch ( error ) {
				// The check itself failed: every loaded gid is unknown, and the
				// record still lands so the restart decision has an artifact.
				process.exitCode = 1;
				console.log( `  combat residue: unknown (${error?.message ?? error})` );
				const residue = {
					codename: scene.codename,
					loaded: scene.count,
					alive: [],
					unknown: scene.gids,
					unaccounted: scene.count - scene.gids.length,
					cleanup: scene.cleanup,
					error: String( error?.message ?? error )
				};
				await mkdir( options.out, { recursive: true } ).catch( () => {} );
				await writeFile( `${options.out}/${location.name}-residue.json`, JSON.stringify( residue, null, 2 ) )
					.catch( () => {} );
			}
		}
		await closeClient( client );
		// A crowd this session admitted is closed (its accounts disabled); a
		// held one is only detached (its poll stops, or the bench never exits)
		// and stays with its host.
		await crowd?.close();
	}
}

/*
================
run
================
*/
async function run( options ) {
	for ( const name of options.at ) {
		if ( !LOCATIONS.some( location => location.name === name ) ) throw Error( `unknown location ${name}` );
	}
	const crowdFixture = options.crowdFixture ? JSON.parse( await readFile( options.crowdFixture, "utf8" ) ) : null;
	const selected = LOCATIONS.filter( location =>
		options.at.includes( location.name ) && location.scenarios.some( name => options.only.includes( name ) )
	).map( location => location.crowd && crowdFixture ? { ...location, fixture: crowdFixture } : location );
	if ( !selected.length ) throw Error( "no scenarios match --at and --only" );
	const results = [];
	for ( const location of selected ) {
		await session( options, location, results );
	}
	if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
	if ( !results.length || results.some( result => !Number.isFinite( result.fps ) || result.fps <= 0 ) ) {
		throw Error( "no valid frame-rate verdict: missing or invalid measurements" );
	}
	const worst = Math.min( ...results.map( r => r.fps ) );
	// Report the measured capture configuration without assuming player defaults.
	const builds = new Set( results.map( r => `${r.identity.build}, replay ${r.identity.replay}` ) );
	const verdict = worst >= GOAL_FPS ? "MET" : "not met";
	console.log(
		options.paced || options.frameLimit ?
			`slowest paced scenario ${worst.toFixed( 0 )} fps; see frame-interval percentiles` :
			`slowest scenario ${worst.toFixed( 0 )} fps; goal ${GOAL_FPS} ${verdict} on ${
				[ ...builds ].join( " / " )
			}` +
			(results.some( r => r.identity.build === "dev-server" ) ? " (dev server: not a release verdict)" : "")
	);
}

const options = parseOptions( process.argv.slice( 2 ), {
	seconds: 3,
	// combat runs only when named: it needs a GM character and an isolated
	// server, since its monsters stay until that server stops.
	// crowd runs only when named too: it needs the provisioning endpoint.
	at: LOCATIONS.map( l => l.name ).filter( name => name !== "combat" && name !== "crowd" ),
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
	screenshot: false,
	snow: false,
	rain: false,
	frames: 0,
	frameMs: 500,
	memory: false,
	gpuProcess: false,
	gpuTime: false,
	hide: [],
	renderScale: 100,
	combat: "MOB_CH_TIGER:10:GIANT",
	vulnerable: false,
	crowd: 32,
	crowdFixture: "",
	crowdHost: CROWD_HOST_URL,
	act: "",
	seed: DEFAULT_ACTIVITY_SEED,
	provisioning: "http://127.0.0.1:8789",
	token: path.join( REPO_ROOT, "apps/server/.state/cluster/agent-provisioning-token" ),
	out: "temp/artifacts/fps-bench",
	// The commit the served bundle must name (build-identity.mjs verifyServedCommit).
	expectCommit: "",
	// The asset manifest hash the page must serve (verifyServedAssets).
	expectAssets: "",
	json: ""
}, USAGE );
if ( !frameLimits().includes( options.frameLimit ) ) throw Error( `unsupported frame limit ${options.frameLimit}` );
for ( const name of options.only ) if ( !SCENARIOS.includes( name ) ) throw Error( `unknown scenario ${name}` );
if ( ![ 100, 75, 50 ].includes( options.renderScale ) ) throw Error( "--render-scale must be 100, 75 or 50" );
for ( const name of options.hide ) if ( !(name in HIDE_MODES) ) throw Error( `unknown --hide ${name}` );
// Chrome runs one trace at a time.
if ( options.trace && options.gpuProcess ) {
	throw Error( "choose --trace or --gpu-process per run; each records a trace" );
}
if ( ![ 0, 1, 2 ].includes( options.shadowDetail ) ) throw Error( `unsupported shadow detail ${options.shadowDetail}` );
if ( !Number.isInteger( options.seed ) || options.seed < 0 ) throw Error( `--seed must be a whole number` );
parseActivity( options.act );
if ( !Number.isInteger( options.frames ) || options.frames < 0 ) throw Error( "--frames must be a whole number" );
await run( options );
