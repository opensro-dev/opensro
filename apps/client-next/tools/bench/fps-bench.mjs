/*
===========================================================================

fps-bench.mjs - frame rate of the real client in the scenarios that matter

Usage:
  node tools/bench/fps-bench.mjs [--seconds N] [--counts] [--trace FILE]
                                 [--profile DIR] [--only a,b] [--at a,b]
                                 [--json FILE]

For each location (--at field,jangan) resets the scratch character there
(scripts/lib/missionMovementFixture.mjs), boots the dev client uncapped
(no vsync, no frame-rate limit) at 1600x900, revives the character if it
died, and runs that location's scenarios:

  still   - nothing moves but the world itself;
  drag    - a right-button camera drag;
  move    - walking back and forth inside the region;
  skill   - skills and attacks on the nearest monster;
  cross   - walking across the east region boundary until arrival.

Each reports frames per second, frame-interval percentiles, and the main
thread's frame and world-preparation time (the runtime's frame probe,
frame-probes.ts). --counts also counts GPU commands per frame (draws,
bundle executions, buffer and texture writes, submits, bind groups and
encoders created) by wrapping the WebGPU prototypes; counting slows the
frame, so its timings are not comparable. --profile writes one CPU profile
per scenario (tools/trace/cpuprofile.mjs), --trace a Chrome trace of the
whole run (tools/trace/analyze.mjs).

The goal these measure: 500 frames a second in every scenario.

===========================================================================
*/
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import {
	startChromeTraceCapture,
	DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES
} from "../../../../scripts/lib/chromeTraceCapture.mjs";
import {
	MISSION_MOVEMENT_FIXTURES,
	resetMissionMovementFixture
} from "../../../../scripts/lib/missionMovementFixture.mjs";
import { bootPlayableSession } from "../../tests/browser/helpers/playable-session.mjs";

const CHARACTER = "asd2";
const SCENARIOS = [ "still", "drag", "move", "skill", "cross" ];
const VIEWPORT = MISSION_MOVEMENT_FIXTURES.region_cross.viewport;
// Two places: a quiet Europe field with a measured region crossing, and the
// water ghost field outside Jangan, a busier scene with live monsters.
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
	scenarios: [ "still", "drag", "move", "skill" ]
} ];
const SETTLE_MS = 8000;
const CROSS_TIMEOUT_MS = 30000;

/*
================
parseArgs
================
*/
function parseArgs( argv ) {
	const options = {
		seconds: 3,
		counts: false,
		trace: null,
		profile: null,
		only: SCENARIOS,
		at: LOCATIONS.map( l => l.name ),
		json: null
	};
	for ( let i = 0; i < argv.length; i++ ) {
		const arg = argv[i], value = () => argv[++i];
		if ( arg === "--seconds" ) options.seconds = Number( value() );
		else if ( arg === "--counts" ) options.counts = true;
		else if ( arg === "--trace" ) options.trace = value();
		else if ( arg === "--profile" ) options.profile = value();
		else if ( arg === "--only" ) options.only = value().split( "," );
		else if ( arg === "--at" ) options.at = value().split( "," );
		else if ( arg === "--json" ) options.json = value();
		else throw Error( `unknown option ${arg}` );
	}
	for ( const name of options.only ) if ( !SCENARIOS.includes( name ) ) throw Error( `unknown scenario ${name}` );
	return options;
}

/*
================
instrument

Page start-up hooks: the frame probe (frame and world time per frame) and,
with counts, wrappers that count WebGPU commands into the current frame.
================
*/
function instrument( counts ) {
	const now = () => performance.now();
	let frameStart = 0, worldStart = 0, worldEnd = 0;
	const tally = {};
	globalThis.__benchRows = [];
	globalThis.__benchTally = tally;
	globalThis.__worldProbeFrameProfiler = {
		detailBegin() {},
		detailEnd() {},
		renderBegin() {},
		renderMark() {},
		characterBegin() {},
		characterMark() {},
		characterCount() {},
		sampleDetails: () => false,
		worldBegin() {
			worldStart = now();
		},
		worldMark() {
			worldEnd = now();
		},
		begin() {
			frameStart = now();
			worldStart = worldEnd = 0;
			for ( const key in tally ) tally[key] = 0;
		},
		mark() {},
		end() {
			globalThis.__benchRows.push( [ now() - frameStart, worldEnd - worldStart, { ...tally } ] );
		}
	};
	if ( !counts ) return;
	const wrap = ( prototype, names, key ) => {
		for ( const name of names ) {
			const original = prototype?.[name];
			if ( typeof original !== "function" ) continue;
			prototype[name] = function( ...args ) {
				tally[key ?? name] = (tally[key ?? name] ?? 0) + 1;
				return original.apply( this, args );
			};
		}
	};
	wrap( globalThis.GPURenderPassEncoder?.prototype, [ "draw", "drawIndexed" ], "pass draws" );
	wrap( globalThis.GPURenderPassEncoder?.prototype, [ "executeBundles" ] );
	wrap( globalThis.GPURenderPassEncoder?.prototype, [ "setBindGroup" ], "pass bind groups" );
	wrap( globalThis.GPURenderBundleEncoder?.prototype, [ "draw", "drawIndexed" ], "bundle draws recorded" );
	wrap( globalThis.GPUDevice?.prototype, [ "createRenderBundleEncoder" ], "bundles recorded" );
	wrap( globalThis.GPUQueue?.prototype, [ "writeBuffer" ] );
	wrap( globalThis.GPUQueue?.prototype, [ "writeTexture" ] );
	wrap( globalThis.GPUQueue?.prototype, [ "submit" ] );
	wrap( globalThis.GPUDevice?.prototype, [ "createBindGroup", "createBuffer", "createCommandEncoder" ] );
	wrap( globalThis.GPUCommandEncoder?.prototype, [ "beginRenderPass", "beginComputePass" ], "passes" );
}

/*
================
measure

Runs one scenario for ms while drive does its input, and returns its frame
statistics. The rAF loop records frame intervals; the frame probe records
main-thread frame and world time.
================
*/
async function measure( page, name, ms, drive ) {
	await page.evaluate( () => {
		globalThis.__benchRows.length = 0;
		globalThis.__benchIntervals = [];
		globalThis.__benchLoop = true;
		let last = performance.now();
		const tick = now => {
			globalThis.__benchIntervals.push( now - last );
			last = now;
			if ( globalThis.__benchLoop ) requestAnimationFrame( tick );
		};
		requestAnimationFrame( tick );
	} );
	const started = Date.now();
	await drive( () => Date.now() - started < ms );
	const [intervals, rows] = await page.evaluate( () => {
		globalThis.__benchLoop = false;
		return [ globalThis.__benchIntervals.slice( 2 ), globalThis.__benchRows.slice( 2 ) ];
	} );
	const sorted = [ ...intervals ].sort( ( a, b ) => a - b ), at = q => sorted[Math.floor( (sorted.length - 1) * q )];
	const mean = list => list.reduce( ( a, b ) => a + b, 0 ) / Math.max( 1, list.length );
	const tally = {};
	for ( const [, , counts] of rows ) for ( const key in counts ) tally[key] = (tally[key] ?? 0) + counts[key];
	for ( const key in tally ) tally[key] = Number( (tally[key] / Math.max( 1, rows.length )).toFixed( 2 ) );
	return {
		name,
		frames: intervals.length,
		fps: 1000 / mean( intervals ),
		p50: at( .5 ),
		p99: at( .99 ),
		max: sorted.at( -1 ),
		main: mean( rows.map( r => r[0] ) ),
		world: mean( rows.map( r => r[1] ) ),
		counts: tally
	};
}

/*
================
keepGoing

Drives nothing but waits, in short steps, while more is true.
================
*/
async function keepGoing( page, more ) {
	while ( more() ) await page.waitForTimeout( 20 );
}

/*
================
drag
================
*/
async function drag( page, more ) {
	const box = await page.evaluate( () => {
		const r = document.querySelector( "canvas" ).getBoundingClientRect();
		return { x: r.x, y: r.y, width: r.width, height: r.height };
	} );
	const y = box.y + box.height * .45, left = box.x + box.width * .15, right = box.x + box.width * .85;
	let x = box.x + box.width / 2, step = 6;
	await page.mouse.move( x, y );
	await page.mouse.down( { button: "right" } );
	try {
		while ( more() ) {
			x += step;
			if ( x > right || x < left ) step = -step;
			await page.mouse.move( x, y );
			await page.waitForTimeout( 8 );
		}
	} finally {
		await page.mouse.up( { button: "right" } );
	}
}

/*
================
walk

Walks between the start and a point 140 units west, turning every second
and a half.
================
*/
async function walk( page, more ) {
	const start = await page.evaluate( () => globalThis.__benchRuntime.gameplay().pose );
	let travelled = 0, last = start;
	const ends = [ { ...start, x: start.x - 140 }, start ];
	for ( let leg = 0; more(); leg++ ) {
		await page.evaluate(
			destination =>
				globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "move", destination } } ),
			ends[leg % 2]
		);
		const legStart = Date.now();
		while ( more() && Date.now() - legStart < 1500 ) {
			await page.waitForTimeout( 100 );
			const pose = await page.evaluate( () => globalThis.__benchRuntime.gameplay().pose );
			travelled += Math.hypot( pose.x - last.x, pose.z - last.z );
			last = pose;
		}
	}
	if ( travelled < 50 ) throw Error( `the move scenario walked only ${travelled.toFixed( 0 )} units` );
}

/*
================
approach

Before the skill scenario is measured: walks to within reach of the
nearest live monster.
================
*/
async function approach( page ) {
	const target = await page.evaluate( () => {
		const root = globalThis.__benchRuntime, pose = root.gameplay().pose;
		const world = e => [ (e.regionId & 255) * 1920 + e.x, (e.regionId >>> 8) * 1920 + e.z ];
		const here = world( pose );
		return root.entities().filter( e => e.kind === "monster" && e.appearanceState?.[0] !== 2 ).map(
			e => ({ entity: e, d: Math.hypot( world( e )[0] - here[0], world( e )[1] - here[1] ) })
		).sort( ( a, b ) => a.d - b.d )[0]?.entity ?? null;
	} );
	if ( !target ) throw Error( "no live monster near the skill scenario" );
	await page.evaluate(
		destination =>
			globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "move", destination } } ),
		{ regionId: target.regionId, x: target.x, y: target.y, z: target.z }
	);
	await page.waitForFunction(
		gid => {
			const root = globalThis.__benchRuntime, p = root.gameplay().pose, e = root.entity( gid );
			if ( !e ) return true;
			const dx = (e.regionId & 255) * 1920 + e.x - ((p.regionId & 255) * 1920 + p.x),
				dz = (e.regionId >>> 8) * 1920 + e.z - ((p.regionId >>> 8) * 1920 + p.z);
			return Math.hypot( dx, dz ) < 60;
		},
		target.gid,
		{ timeout: 30000 }
	);
}

/*
================
fight

Every 700 ms, a learned skill (in turn) and an attack on the nearest live
monster within reach; skills that need no target still cast.
================
*/
async function fight( page, more ) {
	const tally = { skills: 0, targets: 0, turns: 0 };
	for ( let turn = 0; more(); turn++ ) {
		const done = await page.evaluate( turn => {
			const root = globalThis.__benchRuntime, game = root.gameplay(), pose = game.pose;
			const world = e => [ (e.regionId & 255) * 1920 + e.x, (e.regionId >>> 8) * 1920 + e.z ];
			const here = world( pose );
			const target = root.entities().filter( e => e.kind === "monster" && e.appearanceState?.[0] !== 2 ).map(
				e => ({ gid: e.gid, d: Math.hypot( world( e )[0] - here[0], world( e )[1] - here[1] ) })
			).filter( e => e.d < 400 ).sort( ( a, b ) => a.d - b.d )[0];
			const skills = (game.skills ?? []).map( s => s.id ?? s ).filter( id => Number.isInteger( id ) );
			if ( skills.length ) {
				const skillId = skills[turn % skills.length];
				root.session( {
					kind: "gameplay",
					command: target ? { kind: "skill", skillId, gid: target.gid } : { kind: "skill", skillId }
				} );
			}
			if ( target ) root.session( { kind: "gameplay", command: { kind: "attack", gid: target.gid } } );
			return { skill: skills.length > 0, target: !!target };
		}, turn );
		tally.turns++;
		tally.skills += Number( done.skill );
		tally.targets += Number( done.target );
		const turnStart = Date.now();
		while ( more() && Date.now() - turnStart < 700 ) await page.waitForTimeout( 20 );
	}
	console.log( `  skill turns ${tally.turns}, with a skill ${tally.skills}, with a target ${tally.targets}` );
	if ( !tally.skills || !tally.targets ) throw Error( "the skill scenario had no skill or no target" );
}

/*
================
cross

Walks to the fixture's destination across the east region boundary and
returns the run's statistics from the walk's start to its arrival.
================
*/
async function cross( page, fixture ) {
	// Leave any fight the skill scenario started before walking.
	await page.evaluate( () => globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "cancel" } } ) );
	await page.waitForTimeout( 500 );
	await page.evaluate(
		target => {
			// A movement destination is a whole pose; the walk keeps the rest of it.
			const destination = { ...globalThis.__benchRuntime.gameplay().pose, ...target };
			globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "move", destination } } );
		},
		fixture.destination
	);
	const started = Date.now();
	return async more => {
		while ( Date.now() - started < CROSS_TIMEOUT_MS ) {
			const arrived = await page.evaluate( destination => {
				const p = globalThis.__benchRuntime.gameplay().pose;
				return p.regionId === destination.regionId &&
					Math.hypot( p.x - destination.x, p.z - destination.z ) < 8;
			}, fixture.destination );
			if ( arrived ) return;
			await page.waitForTimeout( 50 );
			void more;
		}
		const pose = await page.evaluate( () => {
			const game = globalThis.__benchRuntime.gameplay();
			return { pose: game.pose, moving: game.moving, health: game.vitals?.find( v => v.gid === game.localGid ) };
		} );
		throw Error( `region crossing did not arrive: ${JSON.stringify( pose )}` );
	};
}

/*
================
revive

The scratch character may have died in an earlier run; a benchmark of a
corpse measures nothing. Revives it in place (rebirth choice 2) and waits
for health.
================
*/
async function revive( page ) {
	const alive = () =>
		page.evaluate( () => {
			const game = globalThis.__benchRuntime.gameplay();
			return (game.vitals?.find( v => v.gid === game.localGid )?.hp ?? 0) > 0;
		} );
	if ( await alive() ) return;
	console.log( "  reviving the scratch character" );
	for ( let attempt = 0; attempt < 20 && !await alive(); attempt++ ) {
		await page.evaluate( () =>
			globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "rebirth", choice: 2 } } )
		);
		await page.waitForTimeout( 1000 );
	}
	if ( !await alive() ) throw Error( "the scratch character could not be revived" );
}

/*
================
row
================
*/
function row( result ) {
	const counts = Object.entries( result.counts ).map( ( [k, v] ) => `${k} ${v}` ).join( ", " );
	return `${result.name.padEnd( 6 )} ${result.fps.toFixed( 0 ).padStart( 5 )} fps  frame p50 ${
		result.p50.toFixed( 2 )
	} p99 ${result.p99.toFixed( 2 )} max ${result.max.toFixed( 1 )} ms  main ${result.main.toFixed( 2 )} world ${
		result.world.toFixed( 2 )
	} ms/f${counts ? "  | " + counts : ""}`;
}

/*
================
session

One location: reset the character there, boot, revive, settle, and run
its scenarios. Names results location/scenario.
================
*/
async function session( options, location, results ) {
	await resetMissionMovementFixture( { characterName: CHARACTER, fixture: location.fixture, timeoutMs: 60000 } );
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.addInitScript( instrument, options.counts );
		await bootPlayableSession( page, CHARACTER );
		await page.evaluate( () => globalThis.__benchRuntime = globalThis.__playableRuntime );
		await page.setViewportSize( VIEWPORT );
		await revive( page );
		await page.waitForTimeout( SETTLE_MS );
		const capture = options.trace ?
			await startChromeTraceCapture( page, {
				categories: [ ...DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES, "disabled-by-default-v8.cpu_profiler" ]
			} ) :
			null;
		const cdp = options.profile ? await page.context().newCDPSession( page ) : null;
		if ( cdp ) {
			await mkdir( options.profile, { recursive: true } );
			await cdp.send( "Profiler.enable" );
			await cdp.send( "Profiler.setSamplingInterval", { interval: 50 } );
		}
		const ms = options.seconds * 1000;
		for ( const name of location.scenarios ) {
			if ( !options.only.includes( name ) ) continue;
			if ( name === "skill" ) await approach( page );
			if ( cdp ) await cdp.send( "Profiler.start" );
			let result;
			if ( name === "still" ) result = await measure( page, name, ms, more => keepGoing( page, more ) );
			else if ( name === "drag" ) result = await measure( page, name, ms, more => drag( page, more ) );
			else if ( name === "move" ) result = await measure( page, name, ms, more => walk( page, more ) );
			else if ( name === "skill" ) result = await measure( page, name, ms, more => fight( page, more ) );
			else result = await measure( page, name, CROSS_TIMEOUT_MS, await cross( page, location.fixture ) );
			result.name = `${location.name}/${name}`;
			if ( cdp ) {
				const { profile } = await cdp.send( "Profiler.stop" );
				await writeFile( `${options.profile}/${location.name}-${name}.cpuprofile`, JSON.stringify( profile ) );
			}
			results.push( result );
			console.log( row( result ) );
		}
		if ( capture ) {
			const trace = await capture.stop();
			await writeFile(
				options.trace.replace( /(\.json)?$/, `-${location.name}.json` ),
				JSON.stringify( { traceEvents: trace.traceEvents, metadata: trace.metadata } )
			);
		}
	} finally {
		await page.evaluate( () => globalThis.__benchRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		await browser.close();
	}
}

/*
================
run
================
*/
async function run( options ) {
	process.env.SRO_PROBE_UNLOCK_FPS = "1";
	const results = [];
	for ( const location of LOCATIONS ) {
		if ( options.at.includes( location.name ) ) await session( options, location, results );
	}
	if ( options.json ) await writeFile( options.json, JSON.stringify( results, null, 2 ) );
	const worst = Math.min( ...results.map( r => r.fps ) );
	console.log( `slowest scenario ${worst.toFixed( 0 )} fps; goal 500 ${worst >= 500 ? "MET" : "not met"}` );
}

await run( parseArgs( process.argv.slice( 2 ) ) );
