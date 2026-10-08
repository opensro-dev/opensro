/*
===========================================================================

release-smoke.mjs - exercise a staged release through the public HTTPS edge.

Use a dedicated, single-character probe account and real player controls. The
probe observes published worker messages without changing packets, responses,
application sources or runtime state. Evidence belongs to one immutable entry.

===========================================================================
*/

import { writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import {
	assertCandidateEntry,
	createProbeResult,
	HTTP_OK,
	PROBE_VIEWPORT,
	readProbeInputs,
	recordPhase,
	routeCandidateAssets,
	watchRuntimeErrors
} from "./release-probe.mjs";

// Scene admission is spread over frames. Measured software rendering needed
// 366 seconds for the dock; reload also recreates GPU, UI and audio resources.
// This is a bounded functional probe. Keep frame timings as evidence and apply
// the shorter control deadline only after initialization has completed.
const SCENE_BUDGET_MS = 600_000;
const CONTROL_BUDGET_MS = 30_000;
const MAX_NETWORK_ROWS = 4096;
// The scratch roster contains one actor at this authored dock hit position.
const DOCK_PICK = { x: 505, y: 430 };
// Opt-in performance traces (RELEASE_TRACE_DIR): the phase that just ended ->
// the phase traced next. These are the two scene constructions that dominate
// the probe's duration under software rendering.
const TRACED_PHASES = { login: "roster", gameplay: "resume" };
const TRACE_CATEGORIES = [
	"devtools.timeline",
	"disabled-by-default-devtools.timeline",
	"disabled-by-default-devtools.timeline.frame",
	"disabled-by-default-v8.cpu_profiler",
	"v8.execute",
	"blink.user_timing",
	"gpu"
];

/*
================
fillProbeCredentials

Playwright includes fill arguments in timeout errors. Replace credential-entry
failures at this boundary so reports and uploaded logs cannot contain a password.
================
*/
export async function fillProbeCredentials( page, credentials ) {
	for ( const [id, value] of [ [ "account", credentials.username ], [ "password", credentials.password ] ] ) {
		try {
			await page.locator( `[data-ui-id="${id}"]` ).fill( value );
		} catch {
			throw Error( `Unable to fill release probe ${id} field` );
		}
	}
}

/*
================
observeWorld

Attach an observer to the existing Worker surface before application startup.
Keep only the latest entity states, sufficient to prove world activity without
recording account tokens, chat history or other players' inventories.
================
*/
function observeWorld() {
	const OriginalWorker = window.Worker;
	window.__releaseWorld = { entities: {}, batches: 0 };
	window.Worker = class extends OriginalWorker {
		constructor( ...arguments_ ) {
			super( ...arguments_ );
			this.addEventListener( "message", ( { data } ) => {
				if ( data.kind !== "world" || !data.batch ) return;
				const world = window.__releaseWorld;
				world.batches++;
				for ( const event of data.batch.events ) {
					if ( event.kind === "state" || event.kind === "spawn" ) {
						world.entities[event.entity.gid] = event.entity;
					}
					if ( event.kind === "despawn" ) delete world.entities[event.gid];
				}
			} );
		}
	};
}

/*
================
exercise

The roster must contain exactly the configured scratch character before any
dock click. Opening and closing inventory proves keyboard, HUD and frame
updates while keeping production game state unchanged.
================
*/
async function exercise( page, result, credentials, tracer ) {
	const control = id => page.locator( `[data-ui-id="${id}"]` );
	await control( "frontend:reveal" ).click( { timeout: SCENE_BUDGET_MS } );
	await passPhase( result, tracer, "title" );
	await control( "native:servers" ).click();
	await control( `server:${credentials.shard}` ).click();
	await control( "native:server-accept" ).click();
	await fillProbeCredentials( page, credentials );
	const response = page.waitForResponse( row => new URL( row.url() ).pathname.endsWith( "/character/list" ) );
	result.navigation = "login";
	await control( "password" ).press( "Enter" );
	const rosterResponse = await response;
	if ( rosterResponse.status() !== HTTP_OK ) throw Error( "Roster request failed" );
	await passPhase( result, tracer, "login" );
	const document = await rosterResponse.json();
	const characters = Array.isArray( document ) ? document : document.characters;
	if ( !Array.isArray( characters ) || characters.length !== 1 || characters[0].name !== credentials.character ) {
		throw Error( "Release probe requires its dedicated single-character roster" );
	}
	await control( "frontend:create" ).waitFor( { timeout: SCENE_BUDGET_MS } );
	await passPhase( result, tracer, "roster" );
	await page.mouse.click( DOCK_PICK.x, DOCK_PICK.y );
	await control( "enter" ).click();
	await page.waitForFunction(
		() => document.querySelector( "output" )?.textContent?.includes( "Frontend: world\n" ),
		null,
		{ timeout: SCENE_BUDGET_MS }
	);
	await page.waitForFunction(
		name => Object.values( window.__releaseWorld.entities ).some( row => row.name === name ),
		credentials.character,
		{ timeout: CONTROL_BUDGET_MS }
	);
	await page.locator( "#startup-loading" ).waitFor( { state: "hidden" } );
	await passPhase( result, tracer, "world" );
	result.navigation = "world";
	await page.keyboard.press( "i" );
	await control( "inventory-gold" ).waitFor();
	await page.keyboard.press( "i" );
	await control( "inventory-gold" ).waitFor( { state: "detached" } );
	await passPhase( result, tracer, "gameplay" );
	result.workerResources = await collectWorkerResources( page );
	result.navigation = "resume";
	await page.reload( { waitUntil: "commit" } );
	result.navigation = "world";
	await page.waitForFunction(
		() => document.querySelector( "output" )?.textContent?.includes( "Frontend: world\n" ),
		null,
		{ timeout: SCENE_BUDGET_MS }
	);
	await page.waitForFunction(
		name => Object.values( window.__releaseWorld.entities ).some( row => row.name === name ),
		credentials.character,
		{ timeout: CONTROL_BUDGET_MS }
	);
	await page.locator( "#startup-loading" ).waitFor( { state: "hidden" } );
	await page.keyboard.press( "i" );
	await control( "inventory-gold" ).waitFor();
	await page.keyboard.press( "i" );
	await control( "inventory-gold" ).waitFor( { state: "detached" } );
	await passPhase( result, tracer, "resume" );
}

/*
================
createPhaseTracer

Chrome performance traces of the slow phases, one file each, when a trace
directory is configured; otherwise every call is a no-op. Tracing observes
the page's threads and V8 samples without changing application behavior.
================
*/
function createPhaseTracer( browser, page, directory ) {
	let active = false;
	return {
		async phaseEnded( phase ) {
			if ( !directory ) return;
			if ( active ) {
				await browser.stopTracing();
				active = false;
			}
			const next = TRACED_PHASES[phase];
			if ( !next ) return;
			await browser.startTracing( page, {
				path: path.join( directory, `trace-${next}.json` ),
				categories: TRACE_CATEGORIES
			} );
			active = true;
		},
		async close() {
			if ( active ) await browser.stopTracing().catch( () => {} );
			active = false;
		}
	};
}

/*
================
passPhase

Record a passed phase, then let the tracer close or open its window.
================
*/
async function passPhase( result, tracer, phase ) {
	recordPhase( result, phase );
	await tracer.phaseEnded( phase );
}

/*
================
collectWorkerResources

Read transport and byte timing before a reload disposes the asset worker. The
observation does not change fetch credentials, headers, caching or responses.
================
*/
async function collectWorkerResources( page ) {
	return Promise.all(
		page.workers().map( worker =>
			worker.evaluate( () =>
				performance.getEntriesByType( "resource" ).map( entry => ({
					path: new URL( entry.name ).pathname,
					protocol: entry.nextHopProtocol,
					transferBytes: entry.transferSize,
					duration: entry.duration
				}) )
			).catch( () => [] )
		)
	);
}

/*
================
classifyRequestFailures

The session owner aborts the previous title request when starting a new request
or entering the world. Require the successful replacement phase before admitting
an obsolete restore or login abort; transport and unrelated asset errors fail.
================
*/
export function classifyRequestFailures( failures, phases ) {
	const errors = [], cancelled = [];
	for ( const failure of failures ) {
		const completed = failure.duringReload ||
			(failure.path === "/api/title/servers" && phases.title === "PASS") ||
			(failure.path === "/api/title/session" && phases.login === "PASS") ||
			(failure.path === "/api/title/login" && phases.login === "PASS");
		if ( completed && failure.reason === "net::ERR_ABORTED" ) cancelled.push( failure );
		else errors.push( `Request failed: ${failure.path}: ${failure.reason}` );
	}
	return { errors, cancelled };
}

/*
================
main

Each process starts with a cold browser profile. Persist phase timing, network
durations and a screenshot; credentials and authenticated response bodies are
never written to the public report or uploaded diagnostic artifacts.
================
*/
async function main() {
	const { candidate, credentials, origin, destination, entryUrl: url } = await readProbeInputs( "release-smoke.mjs" );
	const { browser, page } = await launchProbeBrowser( {
		// Linux software WebGPU needs a real display compositor for visible
		// canvas evidence. CI supplies Xvfb; application behavior is unchanged.
		headed: process.env.RELEASE_HEADED === "1",
		viewport: PROBE_VIEWPORT,
		...(process.env.RELEASE_CHROME ? { executablePath: process.env.RELEASE_CHROME } : {})
	} );
	page.setDefaultTimeout( CONTROL_BUDGET_MS );
	const tracer = createPhaseTracer( browser, page, process.env.RELEASE_TRACE_DIR );
	const result = createProbeResult( candidate, { network: [] } );
	watchRuntimeErrors( page, result.errors );
	await routeCandidateAssets( page.context(), url );
	page.on( "requestfinished", request => {
		if ( result.network.length >= MAX_NETWORK_ROWS ) return;
		const endpoint = new URL( request.url() );
		if ( endpoint.origin !== origin || endpoint.pathname.startsWith( "/api/" ) ) return;
		result.network.push( { path: endpoint.pathname, range: request.headers().range, timing: request.timing() } );
	} );
	const requestFailures = [];
	page.on( "requestfailed", request => {
		requestFailures.push( {
			path: new URL( request.url() ).pathname,
			reason: request.failure()?.errorText,
			duringReload: result.navigation === "resume"
		} );
	} );
	const progress = setInterval( async () => {
		const status = await page.locator( "output" ).textContent().catch( () => "loading" );
		// The static button exists before its listener. Wait for a real runtime
		// report before using the player's control, including after document reload.
		if ( status?.startsWith( "Replacement runtime:" ) ) {
			const toggle = page.locator( "#fps-toggle" );
			if ( await toggle.getAttribute( "aria-expanded" ).catch( () => null ) === "false" ) {
				await toggle.click().catch( () => {} );
			}
		}
		console.log( "Release smoke:", status );
		console.log(
			"Release frame timing:",
			await page.locator( "#fps-readout" ).textContent().catch( () => "loading" )
		);
	}, CONTROL_BUDGET_MS );
	try {
		await page.addInitScript( observeWorld );
		// The title phase owns application loading. Navigation only admits the
		// document, so an image cannot consume an unrelated control deadline.
		const response = await page.goto( url, { waitUntil: "commit" } );
		if ( !response || response.status() !== HTTP_OK ) throw Error( "Candidate entry is unavailable" );
		assertCandidateEntry( await response.body(), candidate );
		await exercise( page, result, credentials, tracer );
		const failures = classifyRequestFailures( requestFailures, result.phases );
		result.errors.push( ...failures.errors );
		result.cancelledRequests = failures.cancelled;
		if ( result.errors.length ) throw Error( "Browser recorded runtime or transport errors" );
		result.verdict = "PASS";
	} catch ( error ) {
		result.failure = String( error );
		process.exitCode = 1;
	} finally {
		clearInterval( progress );
		await tracer.close();
		result.requestFailures = requestFailures;
		result.finishedAt = Date.now();
		result.frontend = await page.locator( "output" ).textContent().catch( () => "unavailable" );
		result.frameTiming = await page.locator( "#fps-readout" ).textContent().catch( () => "unavailable" );
		result.workerResources = [ ...(result.workerResources ?? []), ...await collectWorkerResources( page ) ];
		await page.screenshot( { path: path.join( destination, "browser.png" ) } ).catch( () => {} );
		await writeFile( path.join( destination, "report.json" ), JSON.stringify( result, null, 2 ) );
		await browser.close();
		console.log( JSON.stringify( { verdict: result.verdict, phases: result.phases, failure: result.failure } ) );
	}
}

if ( process.argv[1] && import.meta.url === pathToFileURL( path.resolve( process.argv[1] ) ).href ) await main();
