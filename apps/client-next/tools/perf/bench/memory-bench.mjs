/*
===========================================================================

memory-bench.mjs - what the client's tab holds in memory, per process

Usage:
  node tools/perf/bench/memory-bench.mjs [--device iphone13|desktop]
       [--settle SECONDS] [--json FILE] [--headed]

Resets the scratch character at the Jangan field, boots the client in
system Chrome sized as the device (iPhone 13: 844x390 CSS px at DPR 3,
landscape), enters the world, settles, collects garbage and prints
Chrome's own detailed memory dump: each process's private footprint and
its main allocators.

It never uses Playwright (core/raw-cdp.mjs): Playwright's DevTools Network
domain makes the renderer keep a copy of every response body, which a
memory number must not include. Every Network.* method is refused.

The iPhone 13 device is desktop Chrome emulating the phone's viewport and
DPR, not WebKit on a phone: its numbers are Chrome's, not Safari's.

Run it against the dev server or a bench bundle, as fps-bench:

  pnpm --filter @sro/client-next build:bench
  pnpm --filter @sro/client-next preview:bench
  SRO_PROBE_CLIENT_NEXT_BASE_URL=http://127.0.0.1:4181 node tools/perf/bench/memory-bench.mjs

===========================================================================
*/
import { writeFile } from "node:fs/promises";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { assertCharacterAllowed } from "../../../../../scripts/lib/probeCharacter.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials, resolveProbeDivisionId } from "../../../../../scripts/lib/probeSession.mjs";
import { attachPage, launchRawChrome, memoryDump, REPORTED_ALLOCATORS } from "../core/raw-cdp.mjs";
import { parseOptions } from "../core/report.mjs";

const USAGE = "memory-bench.mjs [--device iphone13|desktop] [--settle SECONDS] [--json FILE] [--headed]";
const CHARACTER = assertCharacterAllowed( process.env.SRO_PROBE_CHARACTER ?? "asd2", {
	context: "client-next memory bench"
} );
// fps-bench's busier field outside Jangan.
const FIXTURE = {
	id: "fps-bench-jangan-water-ghost-field",
	movementMode: 3,
	start: { regionId: 25511, x: 1110, y: 64, z: 1800 },
	startYawRadians: 0
};
const DEVICES = {
	iphone13: { width: 844, height: 390, deviceScaleFactor: 3, mobile: true },
	desktop: { width: 1600, height: 900, deviceScaleFactor: 1, mobile: false }
};
const PHASE_TIMEOUT_MS = 120000;
const PHASE_POLL_MS = 250;
const ENTER_RETRY_MS = 1000;
// The dock is entered at the playable-session helper's viewport and click
// point (its single character's model); the device size applies after.
const DOCK_VIEWPORT = { width: 1024, height: 768, deviceScaleFactor: 1, mobile: false };
const DOCK_CLICK = { x: 505, y: 430 };

/*
================
evaluate

Runs expression in the page and returns its value; a thrown error fails
the run with the page's message.
================
*/
async function evaluate( cdp, sessionId, expression ) {
	const { result, exceptionDetails } = await cdp.send( "Runtime.evaluate", {
		expression,
		awaitPromise: true,
		returnByValue: true
	}, sessionId );
	if ( exceptionDetails ) throw Error( exceptionDetails.exception?.description ?? exceptionDetails.text );
	return result.value;
}

/*
================
waitForPhase

Polls the bound runtime until its session reaches phase (and, for the
world, the character), failing on a session error.
================
*/
async function waitForPhase( cdp, sessionId, phase, character = null ) {
	const deadline = Date.now() + PHASE_TIMEOUT_MS;
	for ( ;; ) {
		const state = await evaluate(
			cdp,
			sessionId,
			"(() => { const s = globalThis.__memoryRuntime?.sessionState(); return s ? { phase: s.phase, character: s.character, error: s.error ?? null } : null; })()"
		);
		if ( state?.error && /failed|disconnected|character-select/.test( state.phase ) ) throw Error( state.error );
		if ( state?.phase === phase && (character === null || state.character === character) ) return;
		if ( Date.now() > deadline ) {
			throw Error( `the session did not reach ${phase} (last ${JSON.stringify( state )})` );
		}
		await new Promise( resolve => setTimeout( resolve, PHASE_POLL_MS ) );
	}
}

/*
================
filterCharacterList

Answers /character/list with only the scratch character, through the Fetch
domain (request interception in the browser process; it does not enable
the renderer's Network inspection). Returns the function that stops it.
================
*/
async function filterCharacterList( cdp, sessionId ) {
	const stop = cdp.on( async message => {
		if ( message.method !== "Fetch.requestPaused" || message.sessionId !== sessionId ) return;
		const { requestId, responseStatusCode, responseHeaders } = message.params;
		try {
			const { body, base64Encoded } = await cdp.send( "Fetch.getResponseBody", { requestId }, sessionId );
			const list = JSON.parse( base64Encoded ? Buffer.from( body, "base64" ).toString( "utf8" ) : body );
			if ( !list.characters?.some( row => row.name === CHARACTER ) ) {
				throw Error( `scratch character ${CHARACTER} does not exist` );
			}
			list.characters = list.characters.filter( row => row.name === CHARACTER );
			await cdp.send( "Fetch.fulfillRequest", {
				requestId,
				responseCode: responseStatusCode,
				responseHeaders: (responseHeaders ?? []).filter( header =>
					header.name.toLowerCase() !== "content-length"
				),
				body: Buffer.from( JSON.stringify( list ) ).toString( "base64" )
			}, sessionId );
		} catch ( error ) {
			console.error( "character list filter:", error.message );
			await cdp.send( "Fetch.continueRequest", { requestId }, sessionId ).catch( () => {} );
		}
	} );
	await cdp.send(
		"Fetch.enable",
		{ patterns: [ { urlPattern: "*/character/list", requestStage: "Response" } ] },
		sessionId
	);
	return async () => {
		stop();
		await cdp.send( "Fetch.disable", {}, sessionId );
	};
}

/*
================
bootIntoWorld

The playable-session helper's path without Playwright: bind the page's own
runtime (the dev entry or the bench bundle's facade), log in, enter the
world as the scratch character.
================
*/
async function bootIntoWorld( cdp, sessionId ) {
	const stopFilter = await filterCharacterList( cdp, sessionId );
	await cdp.send( "Page.navigate", { url: CLIENT_NEXT_BASE_URL }, sessionId );
	const deadline = Date.now() + PHASE_TIMEOUT_MS;
	for ( ;; ) {
		const bound = await evaluate(
			cdp,
			sessionId,
			`(async () => {
			if ( document.readyState !== "complete" ) return false;
			const dev = [ ...document.querySelectorAll( 'script[type="module"][src]' ) ]
				.map( script => new URL( script.getAttribute( "src" ) ?? "", location.href ) )
				.find( url => url.origin === location.origin && url.pathname === "/src/bootstrap.ts" );
			const url = dev ? dev.href : new URL( "/assets/bench-runtime.js", location.href ).href;
			globalThis.__memoryRuntime = (await import( url )).runtime;
			return !!globalThis.__memoryRuntime;
		})()`
		).catch( () => false );
		if ( bound ) break;
		if ( Date.now() > deadline ) throw Error( "the page exposed no runtime: use the dev server or a bench bundle" );
		await new Promise( resolve => setTimeout( resolve, PHASE_POLL_MS ) );
	}
	await waitForPhase( cdp, sessionId, "signed-out" );
	const { loginId, loginPassword } = resolveProbeCredentials();
	const login = { kind: "login", id: loginId, password: loginPassword, serverId: resolveProbeDivisionId() };
	await evaluate(
		cdp,
		sessionId,
		`globalThis.__memoryRuntime.session( { ...${JSON.stringify( login )}, apiBase: location.origin + "/api" } )`
	);
	await waitForPhase( cdp, sessionId, "character-select" );
	// The dock selects a character by its 3D model, so the bench enters as
	// the playable-session helper does: the dock shows only the scratch
	// character, at the helper's 1024x768 click point, and Enter follows.
	const entryDeadline = Date.now() + PHASE_TIMEOUT_MS;
	while (
		await evaluate( cdp, sessionId, "globalThis.__memoryRuntime.sessionState()?.phase" ) === "character-select"
	) {
		if ( Date.now() > entryDeadline ) {
			const seen = await evaluate(
				cdp,
				sessionId,
				`JSON.stringify( {
				characters: (globalThis.__memoryRuntime.sessionState()?.characters ?? []).map( row => row.name ),
				controls: [ ...document.querySelectorAll( "[data-ui-id]" ) ].map( c => c.dataset.uiId + (c.disabled ? "(disabled)" : "") ).slice( 0, 40 )
			} )`
			);
			throw Error( `the dock never admitted entry as ${CHARACTER}: ${seen}` );
		}
		for ( const type of [ "mousePressed", "mouseReleased" ] ) {
			await cdp.send( "Input.dispatchMouseEvent", {
				type,
				x: DOCK_CLICK.x,
				y: DOCK_CLICK.y,
				button: "left",
				clickCount: 1
			}, sessionId );
		}
		await evaluate(
			cdp,
			sessionId,
			`(() => {
			const enter = document.querySelector( '[data-ui-id="enter"]' );
			if ( enter && !enter.disabled ) enter.click();
		})()`
		);
		await new Promise( resolve => setTimeout( resolve, ENTER_RETRY_MS ) );
	}
	await waitForPhase( cdp, sessionId, "world", CHARACTER );
	await stopFilter();
}

/*
================
report

One line per process with a footprint over a MiB, largest first.
================
*/
function report( rows ) {
	const short = name => name.replace( "partition_alloc/partitions/", "pa:" );
	const lines = [
		`${"process".padEnd( 10 )}${"footprint".padStart( 10 )}  ${REPORTED_ALLOCATORS.map( short ).join( "  " )}`
	];
	for ( const row of [ ...rows ].sort( ( a, b ) => b.footprint - a.footprint ) ) {
		if ( row.footprint < 1 ) continue;
		const cells = REPORTED_ALLOCATORS.map( name =>
			(row.allocators[name] ?? 0).toFixed( 0 ).padStart( short( name ).length )
		);
		lines.push(
			`${row.type.slice( 0, 10 ).padEnd( 10 )}${row.footprint.toFixed( 0 ).padStart( 10 )}  ${cells.join( "  " )}`
		);
	}
	const total = rows.reduce( ( sum, row ) => sum + row.footprint, 0 );
	lines.push( `total private footprint ${total.toFixed( 0 )} MiB (DevTools Network never enabled)` );
	return lines.join( "\n" );
}

/*
================
main
================
*/
async function main() {
	const options = parseOptions(
		process.argv.slice( 2 ),
		{ device: "iphone13", settle: 30, json: "", headed: false },
		USAGE
	);
	const device = DEVICES[options.device];
	if ( !device ) throw Error( `unknown device ${options.device}\nusage: ${USAGE}` );
	await resetMissionMovementFixture( { characterName: CHARACTER, fixture: FIXTURE, timeoutMs: 60000 } );
	const chrome = await launchRawChrome( { headed: options.headed } );
	try {
		const { cdp } = chrome;
		const sessionId = await attachPage( cdp );
		await cdp.send( "Emulation.setDeviceMetricsOverride", DOCK_VIEWPORT, sessionId );
		await bootIntoWorld( cdp, sessionId );
		await cdp.send( "Emulation.setDeviceMetricsOverride", device, sessionId );
		await new Promise( resolve => setTimeout( resolve, options.settle * 1000 ) );
		await cdp.send( "HeapProfiler.collectGarbage", {}, sessionId );
		const rows = await memoryDump( cdp );
		console.log(
			`memory-bench: ${options.device} at ${FIXTURE.id}, ${options.settle} s settled, ${CLIENT_NEXT_BASE_URL}`
		);
		console.log( report( rows ) );
		if ( options.json ) {
			await writeFile( options.json, JSON.stringify( { device: options.device, rows }, null, "\t" ) );
		}
	} finally {
		await chrome.close();
	}
}

await main();
