/*
===========================================================================

release-smoke.mjs - exercise a staged release through the public HTTPS edge.

Use a dedicated, single-character probe account and real player controls. The
probe observes published worker messages without changing packets, responses,
application sources or runtime state. Evidence belongs to one immutable entry.

===========================================================================
*/

import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";

const TITLE_BUDGET_MS = 180_000;
// Dock admission retains the previous scene while uploading eight groups per
// frame. Measured software rendering needed about 3.8 seconds per frame for
// roughly 800 groups. Allow that bounded work; ordinary controls stay at 30s.
const DOCK_BUDGET_MS = 600_000;
// Both entry and document reload create a renderer and admit world textures.
// HTTP cache warmth does not preserve the GPU device across a document reload.
const WORLD_BUDGET_MS = 180_000;
const CONTROL_BUDGET_MS = 30_000;
const MAX_NETWORK_ROWS = 4096;
const HTTP_OK = 200;
const PROBE_VIEWPORT = { width: 1024, height: 768 };
// The scratch roster contains one actor at this authored dock hit position.
const DOCK_PICK = { x: 505, y: 430 };

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
async function exercise( page, result, credentials ) {
	const control = id => page.locator( `[data-ui-id="${id}"]` );
	await control( "frontend:reveal" ).click( { timeout: TITLE_BUDGET_MS } );
	recordPhase( result, "title" );
	await control( "native:servers" ).click();
	await control( `server:${credentials.shard}` ).click();
	await control( "native:server-accept" ).click();
	await fillProbeCredentials( page, credentials );
	const response = page.waitForResponse( row => new URL( row.url() ).pathname.endsWith( "/character/list" ) );
	result.navigation = "login";
	await control( "password" ).press( "Enter" );
	const rosterResponse = await response;
	if ( rosterResponse.status() !== HTTP_OK ) throw Error( "Roster request failed" );
	recordPhase( result, "login" );
	const document = await rosterResponse.json();
	const characters = Array.isArray( document ) ? document : document.characters;
	if ( !Array.isArray( characters ) || characters.length !== 1 || characters[0].name !== credentials.character ) {
		throw Error( "Release probe requires its dedicated single-character roster" );
	}
	await control( "frontend:create" ).waitFor( { timeout: DOCK_BUDGET_MS } );
	recordPhase( result, "roster" );
	await page.mouse.click( DOCK_PICK.x, DOCK_PICK.y );
	await control( "enter" ).click();
	await page.waitForFunction(
		() => document.querySelector( "output" )?.textContent?.includes( "Frontend: world\n" ),
		null,
		{ timeout: WORLD_BUDGET_MS }
	);
	await page.waitForFunction(
		name => Object.values( window.__releaseWorld.entities ).some( row => row.name === name ),
		credentials.character,
		{ timeout: CONTROL_BUDGET_MS }
	);
	await page.locator( "#startup-loading" ).waitFor( { state: "hidden" } );
	recordPhase( result, "world" );
	result.navigation = "world";
	await page.keyboard.press( "i" );
	await control( "inventory-gold" ).waitFor();
	await page.keyboard.press( "i" );
	await control( "inventory-gold" ).waitFor( { state: "detached" } );
	recordPhase( result, "gameplay" );
	result.workerResources = await collectWorkerResources( page );
	result.navigation = "resume";
	await page.reload( { waitUntil: "commit" } );
	result.navigation = "world";
	await page.waitForFunction(
		() => document.querySelector( "output" )?.textContent?.includes( "Frontend: world\n" ),
		null,
		{ timeout: WORLD_BUDGET_MS }
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
	recordPhase( result, "resume" );
}

/*
================
recordPhase

Keep individual phase durations so slow scene construction is distinguishable
from network admission and ordinary controls in the immutable probe evidence.
================
*/
function recordPhase( result, phase ) {
	const elapsedMs = Date.now() - result.startedAt;
	const previousMs = result.phaseTimings.at( -1 )?.elapsedMs ?? 0;
	result.phases[phase] = "PASS";
	const timing = { phase, elapsedMs, durationMs: elapsedMs - previousMs };
	result.phaseTimings.push( timing );
	console.log( "Release phase:", JSON.stringify( timing ) );
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
	const [candidatePath, destination] = process.argv.slice( 2 );
	if ( !candidatePath || !destination ) throw Error( "Usage: release-smoke.mjs CANDIDATE_JSON OUTPUT_DIRECTORY" );
	const candidate = JSON.parse( await readFile( candidatePath, "utf8" ) );
	const credentials = JSON.parse( process.env.RELEASE_PROBE_ACCOUNT ?? "{}" );
	assertCharacterAllowed( credentials.character );
	if ( !credentials.username || !credentials.password || !credentials.character || !credentials.shard ) {
		throw Error( "Missing release probe account" );
	}
	await mkdir( destination, { recursive: true } );
	if ( !process.env.RELEASE_ORIGIN ) throw Error( "Missing RELEASE_ORIGIN for the host under test" );
	const origin = new URL( process.env.RELEASE_ORIGIN ).origin;
	const url = `${origin}/releases/candidates/${candidate.candidate}/index.html`;
	const { browser, page } = await launchProbeBrowser( {
		// Linux software WebGPU needs a real display compositor for visible
		// canvas evidence. CI supplies Xvfb; application behavior is unchanged.
		headed: process.env.RELEASE_HEADED === "1",
		viewport: PROBE_VIEWPORT,
		...(process.env.RELEASE_CHROME ? { executablePath: process.env.RELEASE_CHROME } : {})
	} );
	page.setDefaultTimeout( CONTROL_BUDGET_MS );
	const result = {
		...candidate,
		verdict: "FAIL",
		phases: {},
		phaseTimings: [],
		errors: [],
		network: [],
		startedAt: Date.now()
	};
	page.on( "pageerror", error => result.errors.push( String( error ) ) );
	page.on( "console", message => {
		if ( message.type() === "error" && message.text().includes( "[SRO runtime]" ) ) {
			result.errors.push( message.text() );
		}
	} );
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
		const digest = createHash( "sha256" ).update( await response.body() ).digest( "hex" );
		if ( digest !== candidate.entrySha256 ) throw Error( "HTTPS served a different candidate entry" );
		await exercise( page, result, credentials );
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
