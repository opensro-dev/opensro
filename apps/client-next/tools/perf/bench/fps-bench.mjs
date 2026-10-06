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
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { parseOptions } from "../core/report.mjs";
import { frameLimits } from "../../../src/engine/foundation/rendering/video-options.ts";
import { openClient, closeClient, createCaptures, measure, revive } from "../core/client.mjs";
import { keepGoing, drag, walk, approach, fight, cross, loadCombat, combat } from "./scenarios.mjs";

const GOAL_FPS = 500;
const CROSS_LIMIT_MS = 30000;
const SCENARIOS = [ "still", "drag", "move", "skill", "cross", "combat" ];
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
} ];
const USAGE = "fps-bench.mjs [--seconds N] [--at a,b] [--only a,b] [--counts] [--spans] [--cpu] [--heap] [--out DIR] " +
	"[--trace] [--json FILE] [--paced] [--cpu-rate N] [--frame-limit 0|60|120|240] [--shadow-detail 0|1|2] " +
	"[--combat codename:count:CHAMP|GIANT|NORMAL] [--vulnerable]";

// Units the character may stand from where a sample expects it (the boot
// fixture start, or its place before a revive) before the sample is rejected.
const REVIVE_TOLERANCE = 50;
// The local GameWorld's own view, for counting GM-loaded residue; its snapshot is cached.
const OBSERVATORY_URL = process.env.SRO_BENCH_OBSERVATORY ??
	"http://127.0.0.1:8791/internal/diagnostics/observatory";
const OBSERVATORY_CACHE_MS = 2000;

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
	const dead = new Set( await sceneDead( page, scene ) );
	const inView = new Set( await sceneAlive( page, scene ) );
	const server = await serverMonsters();
	const alive = [], unknown = [];
	for ( const gid of scene.gids ) {
		const row = server.monsters.get( gid );
		// The server's own hp outranks anything the client shows: a client
		// death sighting never clears a gid the GameWorld still lists alive.
		if ( row && row.hp > 0 || inView.has( gid ) ) alive.push( gid );
		else if ( dead.has( gid ) ) continue;
		// Absent from a truncated server list and never seen dying: not proven gone.
		else if ( !row && server.truncated ) unknown.push( gid );
	}
	// Requested monsters that never showed up are on the server but unknown here.
	const unaccounted = scene.count - scene.gids.length;
	const residue = { codename: scene.codename, loaded: scene.count, dead: dead.size, alive, unknown, unaccounted };
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
async function serverMonsters() {
	await new Promise( resolve => setTimeout( resolve, OBSERVATORY_CACHE_MS + 250 ) );
	const response = await fetch( OBSERVATORY_URL, { headers: { "X-SRO-Local-Diagnostics": "1" } } );
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
session

One location: open the client there and run its scenarios.
================
*/
async function session( options, location, results ) {
	console.log( `Opening ${location.name}: frame limit ${options.frameLimit || "uncapped"}, CPU ${options.cpuRate}x` );
	let scene = null;
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
			const [ms, input] = await drive( client.page, name, location, options.seconds * 1000, scene );
			await captures.start();
			const started = Date.now();
			const result = await measure( client.page, `${location.name}/${name}`, ms, input );
			result.frameLimit = options.frameLimit;
			result.cpuRate = options.cpuRate;
			result.shadowDetail = options.shadowDetail;
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
			result.allocatedMBs = allocated === null ? null : allocated / 1048576 / ((Date.now() - started) / 1000);
			results.push( result );
			if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
			console.log( row( result ) );
		}
		await captures.finish();
	} finally {
		// Residue is reported for rejected windows too: GM monsters outlive them.
		// A run that leaves any (or cannot tell) fails, and its artifact says so.
		if ( scene ) {
			// Never let the residue report replace the window's own exception.
			try {
				await recordResidue( client.page, scene, location, results, options );
			} catch ( error ) {
				process.exitCode = 1;
				console.log( `  combat residue: unknown (${error?.message ?? error})` );
			}
		}
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
	// combat runs only when named: it needs a GM character and an isolated
	// server, since its monsters stay until that server stops.
	at: LOCATIONS.map( l => l.name ).filter( name => name !== "combat" ),
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
	combat: "MOB_CH_TIGER:10:GIANT",
	vulnerable: false,
	out: "temp/artifacts/fps-bench",
	json: ""
}, USAGE );
if ( !frameLimits().includes( options.frameLimit ) ) throw Error( `unsupported frame limit ${options.frameLimit}` );
for ( const name of options.only ) if ( !SCENARIOS.includes( name ) ) throw Error( `unknown scenario ${name}` );
if ( ![ 0, 1, 2 ].includes( options.shadowDetail ) ) throw Error( `unsupported shadow detail ${options.shadowDetail}` );
await run( options );
