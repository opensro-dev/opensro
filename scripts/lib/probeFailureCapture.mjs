/*
===========================================================================

probeFailureCapture.mjs - what a stuck browser test was looking at

A browser test that times out says only which locator it waited for. This
module keeps, for every page the probe launcher opened, the last picture
and the runtime's diagnostics text: taken as the browser closes (a test's
finally runs right after the wait throws, so that is the stuck frame) or,
when the test failed with the page still open (a node:test timeout), at
the hook. A failed test writes them under temp/artifacts/stuck/ and names
the files in its diagnostics; a passing test writes nothing.

probeBrowser.mjs is the one caller: every launch goes through it, so no
test carries its own failure screenshot.

===========================================================================
*/

import { mkdir, writeFile } from "node:fs/promises";
import { basename, join } from "node:path";

// The capture must not turn a stuck test into a hung one.
const CAPTURE_TIMEOUT_MS = 5000;
// The oldest unanswered requests named in a capture: a hung load is one of them.
const MAX_LISTED_REQUESTS = 15;
const STUCK_ROOT = join( "temp", "artifacts", "stuck" );

const livePages = new Set();
// page -> (request -> start ms): what the page still waits on.
const inFlight = new WeakMap();
let captures = [];

/*
================
snapshot

The page's picture and what the runtime says about itself: the diagnostics
<output>, the semantics status line and the URL. Never throws.
================
*/
async function snapshot( page ) {
	if ( page.isClosed() ) return null;
	const [png, text] = await Promise.all( [
		page.screenshot( { timeout: CAPTURE_TIMEOUT_MS } ).catch( error => (String( error ), null) ),
		page.evaluate( () =>
			[
				"url: " + location.href,
				"status: " + (document.querySelector( '[data-gpu-ui] [role="status"]' )?.textContent ?? "(none)"),
				"",
				document.querySelector( "output" )?.textContent ?? "(no diagnostics output)"
			].join( "\n" )
		).catch( error => "page evaluate failed: " + String( error ) )
	] );
	return { png, text: pendingRequests( page ) + text, at: new Date().toISOString() };
}

/*
================
pendingRequests

The page's unanswered requests, oldest first, with their age.
================
*/
function pendingRequests( page ) {
	const now = Date.now();
	const pending = [ ...(inFlight.get( page ) ?? new Map()) ].sort( ( a, b ) => a[1] - b[1] );
	const lines = pending.slice( 0, MAX_LISTED_REQUESTS ).map( ( [request, start] ) =>
		`  ${String( now - start ).padStart( 6 )} ms ${request.method()} ${request.url()}`
	);
	return [ `in flight: ${pending.length}`, ...lines, "" ].join( "\n" );
}

/*
================
captureLivePages
================
*/
async function captureLivePages() {
	for ( const page of livePages ) {
		const shot = await snapshot( page );
		if ( shot ) captures.push( shot );
	}
}

/*
================
watchProbeBrowser

Register a launched page and capture its pages before the browser (or the
persistent context) closes.
================
*/
export function watchProbeBrowser( closer, page ) {
	livePages.add( page );
	page.once( "close", () => livePages.delete( page ) );
	const requests = new Map();
	inFlight.set( page, requests );
	page.on( "request", request => requests.set( request, Date.now() ) );
	page.on( "requestfinished", request => requests.delete( request ) );
	page.on( "requestfailed", request => requests.delete( request ) );
	const close = closer.close.bind( closer );
	closer.close = async ( ...args ) => {
		if ( livePages.has( page ) ) {
			const shot = await snapshot( page );
			if ( shot ) captures.push( shot );
			livePages.delete( page );
		}
		return close( ...args );
	};
}

/*
================
slug
================
*/
function slug( value ) {
	return value.replace( /[^A-Za-z0-9]+/g, "-" ).replace( /^-|-$/g, "" ).slice( 0, 80 ) || "test";
}

/*
================
writeCaptures

Write a failed test's captures and name them in its diagnostics.
================
*/
async function writeCaptures( t ) {
	const file = slug( basename( process.argv[1] ?? "probe", ".mjs" ) );
	const directory = join( STUCK_ROOT, file );
	await mkdir( directory, { recursive: true } );
	for ( const [index, shot] of captures.entries() ) {
		const stem = join( directory, `${slug( t.name )}-${index + 1}` );
		if ( shot.png ) await writeFile( stem + ".png", shot.png );
		await writeFile( stem + ".txt", `captured ${shot.at}\n${shot.text}\n` );
		t.diagnostic( `stuck capture: ${stem}.png / .txt` );
	}
}

// Only a node:test run has a test to fail; a probe script imports this too.
if ( process.env.NODE_TEST_CONTEXT ) {
	const { afterEach, beforeEach } = await import( "node:test" );
	beforeEach( () => {
		captures = [];
	} );
	afterEach( async t => {
		if ( !t.passed ) {
			await captureLivePages();
			if ( captures.length ) {
				await writeCaptures( t ).catch( error => t.diagnostic( "stuck capture failed: " + error ) );
			}
		}
		captures = [];
	} );
}
