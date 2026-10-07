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
import { execFileSync } from "node:child_process";
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import { assertCharacterAllowed } from "../../../../../scripts/lib/probeCharacter.mjs";
import {
	startChromeTraceCapture,
	DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES
} from "../../../../../scripts/lib/chromeTraceCapture.mjs";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { defaultVideoOptions, frameLimits } from "../../../src/engine/foundation/rendering/video-options.ts";
import { bootPlayableSession } from "../../../tests/browser/helpers/playable-session.mjs";

export const CHARACTER = assertCharacterAllowed( process.env.SRO_PROBE_CHARACTER ?? "asd2", {
	context: "frame benchmark"
} );
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
export function instrument( { counts, spans, target = globalThis } ) {
	const LONG_FRAME_MS = 50, MAX_LONG_FRAMES = 32, OPENING_WINDOW_MS = 10000;
	const now = () => target.performance.now();
	let frameStart = 0, worldStart = 0, worldEnd = 0, frameMark = 0, renderMark = 0, characterMark = 0;
	let measuring = null, intervalMs;
	let displayed;
	const tally = {}, opened = {};
	const add = ( key, value ) => {
		tally[key] = (tally[key] ?? 0) + value;
	};
	target.__benchRows = [];
	target.__benchMovement = [];
	target.__benchInputs = [];
	target.__benchTally = tally;
	target.__benchLongFrames = { callbacks: 0, intervals: 0, frames: [] };
	target.__worldProbeFrameProfiler = {
		/*
		================
		movement
		================
		*/
		movement( sample ) {
			displayed = sample;
			const rows = target.__benchMovement;
			const root = target.__benchRuntime, game = root?.gameplay();
			sample.path = game?.movementPath;
			const actor = root?.characterActors().find( actor => actor.gid === game?.localGid );
			if ( actor ) sample.body = { pose: { ...actor.pose }, clip: actor.clip, mountedOn: actor.mountedOn };
			if ( actor?.mountedOn ) {
				const mount = root.characterActors().find( row => row.gid === actor.mountedOn );
				if ( mount ) sample.mount = { pose: { ...mount.pose }, gid: mount.gid, height: mount.height };
			}
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
		/*
		================
		characterBatch

		Reasons overlap. The combination bucket is exclusive, while a fragment
		bucket answers how much submitted work carries that particular split.
		Never retain model IDs, actor IDs or animation revisions as tally keys.
		================
		*/
		characterBatch: counts ?
			( variant, actors, draws ) => {
				const fragments = variant.split( "\0" ).filter( Boolean ).map( value => value.split( ":" )[0] );
				const reasons = [ "cloth", "modifier", "glow", "fade", "tint", "light", "deferred" ].filter(
					value => fragments.includes( value )
				);
				if ( !reasons.length ) reasons.push( "plain" );
				for ( const key of [ "all", "combination:" + reasons.join( "+" ), ...reasons ] ) {
					add( "character-batch groups " + key, 1 );
					add( "character-batch actors " + key, actors );
					add( "character-batch draws " + key, draws );
				}
			} :
			undefined,
		sampleDetails: () => spans,
		worldBegin() {
			worldStart = now();
		},
		worldMark() {
			worldEnd = now();
		},
		/*
		================
		worldCount
		================
		*/
		worldCount( name, value ) {
			add( name, value );
		},
		begin() {
			displayed = undefined;
			frameStart = frameMark = now();
			measuring = target.__benchLoop === true ? target.__benchLongFrames : null;
			intervalMs = undefined;
			if ( measuring ) {
				if ( target.__benchLastFrame !== undefined ) {
					intervalMs = frameStart - target.__benchLastFrame;
					target.__benchIntervals.push( intervalMs );
				}
				target.__benchLastFrame = frameStart;
			}
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
			const endedAtMs = now(), elapsedMs = endedAtMs - frameStart, counts = { ...tally };
			if ( displayed ) displayed.presentedAtMs = endedAtMs;
			target.__benchRows.push( [ elapsedMs, worldEnd - worldStart, counts ] );
			if ( target.__benchRows.length > 16384 ) target.__benchRows.splice( 0, 4096 );
			if ( !measuring || target.__benchLoop !== true || measuring !== target.__benchLongFrames ) return;
			const cpuMs = counts["cpu-ms"] ?? elapsedMs, evidence = measuring;
			// The incident threshold alone cannot prove a sub-25 ms first turn.
			// Keep constant-space opening maxima even after the frame tail rolls.
			const opening = evidence.opening ??= {
				startAtMs: frameStart,
				endAtMs: frameStart,
				windowMs: OPENING_WINDOW_MS,
				frames: 0,
				maxCpuMs: 0,
				maxElapsedMs: 0,
				maxIntervalMs: 0,
				maxPoseCreated: 0,
				maxCpuAtMs: frameStart,
				maxPoseAtMs: frameStart
			};
			if ( frameStart - opening.startAtMs < OPENING_WINDOW_MS ) {
				opening.frames++;
				opening.endAtMs = endedAtMs;
				if ( cpuMs > opening.maxCpuMs ) {
					opening.maxCpuMs = cpuMs;
					opening.maxCpuAtMs = frameStart;
				}
				const created = counts["pose-created"] ?? 0;
				if ( created > opening.maxPoseCreated ) {
					opening.maxPoseCreated = created;
					opening.maxPoseAtMs = frameStart;
				}
				opening.maxElapsedMs = Math.max( opening.maxElapsedMs, elapsedMs );
				opening.maxIntervalMs = Math.max( opening.maxIntervalMs, intervalMs ?? 0 );
			}
			const slowCallback = cpuMs > LONG_FRAME_MS, slowInterval = intervalMs > LONG_FRAME_MS;
			if ( slowCallback ) evidence.callbacks++;
			if ( slowInterval ) evidence.intervals++;
			// Preserve opening incidents even when the ordinary frame tail rolls over.
			// CPU work and displayed interval differ when scheduling or readback stalls.
			if ( (slowCallback || slowInterval) && evidence.frames.length < MAX_LONG_FRAMES ) {
				evidence.frames.push( {
					atMs: frameStart,
					endedAtMs,
					elapsedMs,
					intervalMs,
					cpuMs,
					worldMs: worldEnd - worldStart,
					revision: displayed?.revision,
					workerAtMs: displayed?.workerAtMs,
					workerDebtMs: displayed?.workerDebtMs,
					counts
				} );
			}
		}
	};
	if ( !counts ) return;
	const encoderCommands = new WeakMap(), bundleCommands = new WeakMap(), shadowPasses = new WeakSet();
	/*
	================
	wrap

	Bundle recording and playback are distinct costs. Retain only command
	counts, never resource handles, and charge each execution in its own frame.
	================
	*/
	const wrap = ( prototype, names, key, bundleKey ) => {
		for ( const name of names ) {
			const original = prototype?.[name];
			if ( typeof original !== "function" ) continue;
			prototype[name] = function( ...args ) {
				const result = original.apply( this, args );
				tally[key ?? name] = (tally[key ?? name] ?? 0) + 1;
				if ( key === "pass draws" && shadowPasses.has( this ) ) add( "shadow silhouette draws", 1 );
				if ( name === "beginRenderPass" && args[0]?.label === "character-shadow-generate" ) {
					shadowPasses.add( result );
				}
				if ( bundleKey ) {
					const commands = encoderCommands.get( this ) ?? {};
					commands[bundleKey] = (commands[bundleKey] ?? 0) + 1;
					encoderCommands.set( this, commands );
				}
				if ( name === "writeBuffer" ) {
					const data = args[2], elementBytes = data.BYTES_PER_ELEMENT ?? 1;
					add(
						"writeBuffer bytes",
						args[4] === undefined ?
							data.byteLength - (args[3] ?? 0) * elementBytes :
							args[4] * elementBytes
					);
				}
				return result;
			};
		}
	};
	const drawMethods = [ "draw", "drawIndexed", "drawIndirect", "drawIndexedIndirect" ];
	wrap( target.GPURenderPassEncoder?.prototype, drawMethods, "pass draws" );
	wrap( target.GPURenderPassEncoder?.prototype, [ "setBindGroup" ], "pass bind groups" );
	wrap( target.GPURenderPassEncoder?.prototype, [ "setPipeline" ], "pass pipelines" );
	wrap( target.GPURenderBundleEncoder?.prototype, drawMethods, "bundle draws recorded", "draws" );
	wrap( target.GPURenderBundleEncoder?.prototype, [ "setPipeline" ], "bundle pipelines recorded", "pipelines" );
	wrap( target.GPURenderBundleEncoder?.prototype, [ "setBindGroup" ], "bundle bind groups recorded", "bind groups" );
	const bundlePrototype = target.GPURenderBundleEncoder?.prototype;
	const finishBundle = bundlePrototype?.finish;
	if ( finishBundle ) {
		bundlePrototype.finish = function( ...args ) {
			const bundle = finishBundle.apply( this, args );
			bundleCommands.set( bundle, { ...(encoderCommands.get( this ) ?? {}) } );
			encoderCommands.delete( this );
			return bundle;
		};
	}
	const passPrototype = target.GPURenderPassEncoder?.prototype;
	const executeBundles = passPrototype?.executeBundles;
	if ( executeBundles ) {
		passPrototype.executeBundles = function( bundles ) {
			// WebGPU accepts an iterable; materialize once so generators are not
			// consumed by counting before the actual API receives them.
			const submitted = Array.from( bundles );
			const result = executeBundles.call( this, submitted );
			add( "executeBundles", 1 );
			for ( const bundle of submitted ) {
				const commands = bundleCommands.get( bundle );
				add( "bundles executed", 1 );
				if ( !commands ) {
					add( "untracked bundles executed", 1 );
					continue;
				}
				for ( const key of [ "draws", "pipelines", "bind groups" ] ) {
					add( "bundle " + key + " executed", commands[key] ?? 0 );
				}
			}
			return result;
		};
	}
	wrap(
		target.GPUComputePassEncoder?.prototype,
		[ "dispatchWorkgroups", "dispatchWorkgroupsIndirect" ],
		"dispatches"
	);
	wrap( target.GPUComputePassEncoder?.prototype, [ "setBindGroup" ], "compute bind groups" );
	wrap( target.GPUDevice?.prototype, [ "createRenderBundleEncoder" ], "bundles recorded" );
	wrap( target.GPUQueue?.prototype, [ "writeBuffer" ] );
	wrap( target.GPUQueue?.prototype, [ "writeTexture" ] );
	wrap( target.GPUQueue?.prototype, [ "submit" ] );
	wrap( target.GPUDevice?.prototype, [ "createBindGroup", "createBuffer", "createCommandEncoder" ] );
	wrap( target.GPUCommandEncoder?.prototype, [ "beginRenderPass", "beginComputePass" ], "passes" );
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
		globalThis.__benchLongFrames = { callbacks: 0, intervals: 0, frames: [] };
		globalThis.__benchIntervals = [];
		globalThis.__benchLoop = true;
		globalThis.__benchLastFrame = undefined;
	} );
	const started = Date.now();
	await drive( () => Date.now() - started < ms );
	const [intervals, rows, movement, inputs, longFrames, timeOriginMs] = await page.evaluate( () => {
		globalThis.__benchLoop = false;
		return [
			globalThis.__benchIntervals.slice( 2 ),
			globalThis.__benchRows.slice( 2 ),
			globalThis.__benchMovement,
			globalThis.__benchInputs,
			globalThis.__benchLongFrames,
			performance.timeOrigin
		];
	} );
	const sorted = [ ...intervals ].sort( ( a, b ) => a - b ), at = q => sorted[Math.floor( (sorted.length - 1) * q )];
	const mean = list => list.reduce( ( a, b ) => a + b, 0 ) / Math.max( 1, list.length );
	const tally = {};
	for ( const [, , counts] of rows ) for ( const key in counts ) tally[key] = (tally[key] ?? 0) + counts[key];
	// Preserve rare incidents before the per-frame averages round them away.
	// These totals cover the retained rows, whose count is reported explicitly.
	const countTotals = { ...tally };
	for ( const key in tally ) tally[key] = Number( (tally[key] / Math.max( 1, rows.length )).toFixed( 2 ) );
	return {
		name,
		timeOriginMs,
		frames: intervals.length,
		fps: 1000 / mean( intervals ),
		p50: at( .5 ),
		p95: at( .95 ),
		p99: at( .99 ),
		max: sorted.at( -1 ),
		main: mean( rows.map( r => r[0] ) ),
		world: mean( rows.map( r => r[1] ) ),
		// Incidents cover the full window even when average timings use the frame tail.
		callbacksOver50Ms: longFrames.callbacks,
		intervalsOver50Ms: longFrames.intervals,
		longFrames: longFrames.frames,
		opening: longFrames.opening ?? null,
		movement,
		inputs,
		counts: tally,
		countTotals,
		countedFrames: rows.length
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
	{
		counts = false,
		spans = false,
		uncapped = true,
		cpuRate = 1,
		frameLimit = 0,
		shadowDetail = 0,
		videoOptions = undefined,
		headed = false,
		backgroundThrottling = false,
		beforeLogin = undefined
	} = {}
) {
	process.env.SRO_PROBE_UNLOCK_FPS = uncapped ? "1" : "0";
	await resetMissionMovementFixture( { characterName: CHARACTER, fixture, timeoutMs: 60000 } );
	const { browser, page } = await launchProbeBrowser( { headed, backgroundThrottling } );
	try {
		await page.addInitScript(
			options => {
				localStorage.setItem( "sro:v1150:video-options:1", JSON.stringify( options ) );
			},
			videoOptions ?? {
				...defaultVideoOptions(),
				frameLimit,
				records: defaultVideoOptions().records.map( row =>
					row.map( ( value, slot ) => slot === 1 ? shadowDetail : value )
				)
			}
		);
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
buildIdentity

What a measurement ran against, so a number cannot be read as the wrong
build: the client origin, whether it is the Vite dev server (which serves
unbundled modules with its client script) or a built bundle, the source
commit of this checkout (dirty when it has local changes), and the bug
report replay recorder, which costs frames while it runs.
================
*/
export async function buildIdentity( page ) {
	const client = await page.evaluate( () => ({
		origin: location.origin,
		dev: !!document.querySelector( 'script[src*="/@vite/client"]' ),
		replayPreference: localStorage.getItem( "sro:bug-report:replay:1" ),
		replayRecording: [ ...document.querySelectorAll( "video" ) ].some( video => !!video.srcObject )
	}) );
	let commit = "unknown";
	try {
		const git = ( ...args ) => execFileSync( "git", args, { encoding: "utf8" } ).trim();
		commit = git( "rev-parse", "--short", "HEAD" ) + (git( "status", "--porcelain" ) ? "+dirty" : "");
	} catch {
		// Not a checkout: the commit stays unknown rather than guessed.
	}
	return {
		origin: client.origin,
		build: client.dev ? "dev-server" : "bundle",
		commit,
		replay: client.replayRecording ? "recording" : "off",
		replayPreference: client.replayPreference
	};
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
selectFrameLimit

Use the real option draft and Apply path; leave the window open for inspection.
================
*/
export async function selectFrameLimit( page, limit ) {
	const index = frameLimits().indexOf( limit );
	if ( index < 0 ) throw Error( `Unsupported frame limit ${limit}` );
	await page.keyboard.press( "Escape" );
	await page.locator( '[data-ui-id="open-window:Option"]' ).click();
	for ( let i = 0; i < 10; i++ ) await page.locator( '[data-ui-id="option-video-down"]' ).click();
	await page.locator( '[data-ui-id="option-video-combo:-3"]' ).click();
	await page.locator( `[data-ui-id="option-video-choice:-3:${index}"]` ).click();
	await page.locator( '[data-ui-id="option-apply"]' ).click();
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
/**
 * @param {import("playwright-core").Page} page
 * @param {{ dir?: string | null, cpu?: boolean, heap?: boolean, trace?: string | null }} options
 */
export async function createCaptures( page, { dir = null, cpu = false, heap = false, trace = null } = {} ) {
	// A sampled trace already starts V8's internal CPU profiler. Keep each
	// capture to one sampler; separate runs also make their overhead explicit.
	if ( cpu && trace ) throw Error( "Choose --cpu or --trace per run; sampled traces already include CPU profiles" );
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
