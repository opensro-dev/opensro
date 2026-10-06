/*
===========================================================================

client.mjs - the dev client under measurement

Boots the scratch character into the dev client in an uncapped browser
(no vsync, no frame-rate limit), installs the frame probe the runtime
reads (src/engine/runtime/frame-probes.ts) and, on request, WebGPU command
counters, and measures frames while a scenario drives input. Captures
(CPU profile, sampled allocation profile, Chrome trace) wrap any span.

===========================================================================
*/
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import {
	startChromeTraceCapture,
	DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES
} from "../../../../../scripts/lib/chromeTraceCapture.mjs";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { bootPlayableSession } from "../../../tests/browser/helpers/playable-session.mjs";

export const CHARACTER = "asd2";
const VIEWPORT = { width: 1600, height: 900 };
const SETTLE_MS = 8000;
// Allocation sampling: one sample per 16 KiB allocated on average, keeping
// objects the collectors already freed, so the profile shows churn.
const HEAP_SAMPLING_BYTES = 16384;

/*
================
instrument

Page start-up hooks: the frame probe (frame and world time per frame) and,
with counts, wrappers that count WebGPU commands into the current frame.
With spans, the probe also times the runtime's own stage marks (as
"@stage" ms since the previous mark of the same owner) and detail spans
(as "stage" ms and "stage n" spans per frame): where a frame goes, without
a profiler's overhead.
================
*/
function instrument( { counts, spans } ) {
	const now = () => performance.now();
	let frameStart = 0, worldStart = 0, worldEnd = 0, frameMark = 0, renderMark = 0, characterMark = 0;
	let displayed;
	const tally = {}, opened = {};
	const add = ( key, value ) => {
		tally[key] = (tally[key] ?? 0) + value;
	};
	globalThis.__benchRows = [];
	globalThis.__benchMovement = [];
	globalThis.__benchInputs = [];
	globalThis.__benchTally = tally;
	globalThis.__worldProbeFrameProfiler = {
		/*
		================
		movement
		================
		*/
		movement( sample ) {
			displayed = sample;
			const rows = globalThis.__benchMovement;
			const root = globalThis.__benchRuntime, game = root?.gameplay();
			sample.path = game?.movementPath;
			const actor = root?.characterActors().find( actor => actor.gid === game?.localGid );
			if ( actor ) sample.body = { pose: { ...actor.pose }, clip: actor.clip, mountedOn: actor.mountedOn };
			rows.push( sample );
			if ( rows.length > 4096 ) rows.splice( 0, 1024 );
		},
		detailBegin( stage ) {
			if ( spans ) opened[stage] = now();
		},
		detailEnd( stage ) {
			if ( !spans || opened[stage] === undefined ) return;
			add( stage, now() - opened[stage] );
			add( stage + " n", 1 );
			opened[stage] = undefined;
		},
		renderBegin() {
			renderMark = now();
		},
		renderMark( stage ) {
			if ( !spans ) return;
			const at = now();
			add( "@" + stage, at - renderMark );
			renderMark = at;
		},
		characterBegin() {
			characterMark = now();
		},
		characterMark( stage ) {
			if ( !spans ) return;
			const at = now();
			add( "@" + stage, at - characterMark );
			characterMark = at;
		},
		characterCount( name, value = 1 ) {
			add( name, value );
		},
		sampleDetails: () => spans,
		worldBegin() {
			worldStart = now();
		},
		worldMark() {
			worldEnd = now();
		},
		begin() {
			displayed = undefined;
			frameStart = frameMark = now();
			worldStart = worldEnd = 0;
			for ( const key in tally ) tally[key] = 0;
		},
		mark( stage ) {
			if ( !spans ) return;
			const at = now();
			add( "@" + stage, at - frameMark );
			frameMark = at;
		},
		end() {
			if ( displayed ) displayed.presentedAtMs = now();
			globalThis.__benchRows.push( [ now() - frameStart, worldEnd - worldStart, { ...tally } ] );
			if ( globalThis.__benchRows.length > 16384 ) globalThis.__benchRows.splice( 0, 4096 );
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
export async function measure( page, name, ms, drive ) {
	await page.evaluate( () => {
		globalThis.__benchRows.length = 0;
		globalThis.__benchMovement.length = 0;
		globalThis.__benchInputs.length = 0;
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
	const [intervals, rows, movement, inputs] = await page.evaluate( () => {
		globalThis.__benchLoop = false;
		return [
			globalThis.__benchIntervals.slice( 2 ),
			globalThis.__benchRows.slice( 2 ),
			globalThis.__benchMovement,
			globalThis.__benchInputs
		];
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
		p95: at( .95 ),
		p99: at( .99 ),
		max: sorted.at( -1 ),
		main: mean( rows.map( r => r[0] ) ),
		world: mean( rows.map( r => r[1] ) ),
		callbacksOver50Ms: rows.filter( r => (r[2]["cpu-ms"] ?? r[0]) > 50 ).length,
		movement,
		inputs,
		counts: tally
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
export async function revive( page ) {
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
openClient

Resets the scratch character to fixture, boots the client at 1600x900,
revives the character if it died and lets the world settle. Returns the
browser and page; close the browser when done. counts and spans are
instrument's options.
================
*/
export async function openClient(
	fixture,
	{ counts = false, spans = false, uncapped = true, cpuRate = 1, beforeLogin = undefined } = {}
) {
	process.env.SRO_PROBE_UNLOCK_FPS = uncapped ? "1" : "0";
	await resetMissionMovementFixture( { characterName: CHARACTER, fixture, timeoutMs: 60000 } );
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.addInitScript( instrument, { counts, spans } );
		await bootPlayableSession( page, CHARACTER, beforeLogin );
		await page.evaluate( () => globalThis.__benchRuntime = globalThis.__playableRuntime );
		await page.setViewportSize( VIEWPORT );
		await revive( page );
		await page.waitForTimeout( SETTLE_MS );
		if ( cpuRate !== 1 ) {
			const cdp = await page.context().newCDPSession( page );
			await cdp.send( "Emulation.setCPUThrottlingRate", { rate: cpuRate } );
		}
	} catch ( error ) {
		console.error(
			"Client boot evidence:",
			await page.evaluate( () => ({
				status: document.querySelector( "output" )?.textContent,
				session: globalThis.__playableRuntime?.sessionState()?.phase
			}) ).catch( () => null )
		);
		await browser.close();
		throw error;
	}
	return { browser, page };
}

/*
================
closeClient
================
*/
export async function closeClient( { browser, page } ) {
	await page.evaluate( () => globalThis.__benchRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
	await browser.close();
}

/*
================
createCaptures

Optional captures around spans of a run: a CPU profile and a sampled
allocation profile per span (written to dir as NAME.cpuprofile and
NAME.heapprofile), and one Chrome trace of the whole run. stop() returns
the span's sampled allocation in bytes, or null without --heap.
================
*/
export async function createCaptures( page, { dir = null, cpu = false, heap = false, trace = null } = {} ) {
	const cdp = cpu || heap ? await page.context().newCDPSession( page ) : null;
	if ( dir ) await mkdir( dir, { recursive: true } );
	if ( cpu ) {
		await cdp.send( "Profiler.enable" );
		await cdp.send( "Profiler.setSamplingInterval", { interval: 50 } );
	}
	if ( heap ) await cdp.send( "HeapProfiler.enable" );
	const tracing = trace ?
		await startChromeTraceCapture( page, {
			categories: [ ...DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES, "disabled-by-default-v8.cpu_profiler" ]
		} ) :
		null;
	return {
		/*
		================
		start
		================
		*/
		async start() {
			if ( cpu ) await cdp.send( "Profiler.start" );
			if ( heap ) {
				await cdp.send( "HeapProfiler.startSampling", {
					samplingInterval: HEAP_SAMPLING_BYTES,
					includeObjectsCollectedByMajorGC: true,
					includeObjectsCollectedByMinorGC: true
				} );
			}
		},
		/*
		================
		stop
		================
		*/
		async stop( name ) {
			if ( cpu ) {
				const { profile } = await cdp.send( "Profiler.stop" );
				await writeFile( `${dir}/${name}.cpuprofile`, JSON.stringify( profile ) );
			}
			if ( !heap ) return null;
			const { profile } = await cdp.send( "HeapProfiler.stopSampling" );
			await writeFile( `${dir}/${name}.heapprofile`, JSON.stringify( profile ) );
			return profile.samples.reduce( ( sum, sample ) => sum + sample.size, 0 );
		},
		/*
		================
		finish
		================
		*/
		async finish() {
			if ( !tracing ) return;
			await tracing.stop( { rawOutputPath: trace } );
		}
	};
}
