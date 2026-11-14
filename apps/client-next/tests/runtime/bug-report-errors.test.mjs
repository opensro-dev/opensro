/*
===========================================================================

bug-report-errors.test.mjs - uncaught failures reach the report timeline

A stall with no exception anywhere in the report used to end the
investigation: the journal recorded a bare message for runtime failures
the game caught itself, and an uncaught throw (a RangeError inside a
frame loop, a rejected promise) left no stack to read. The journal now
records uncaught errors and rejections with their stacks, sanitised. The
first throw of each failure is kept for the page's life with a repeat
count, so a storm that began before the window still names its stack
(#3425), and a throwing loop cannot evict the player's input history.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readZipEntries } from "../helpers/zip-reader.mjs";
const { createJournal, JOURNAL_WINDOW_MS, timelineEvents } = await import(
	"../../src/engine/runtime/bug-report/journal.ts"
);
const { createDiagnosticUpload } = await import( "../../src/engine/runtime/bug-report/archive.ts" );

const ORIGIN = "http://127.0.0.1:5174";

/*
================
installPage

A fake page: an event-target window and the origin stacks are built from.
================
*/
function installPage( t ) {
	let now = 0;
	t.mock.method( performance, "now", () => now );
	const saved = new Map(
		[ "window", "location" ].map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] )
	);
	t.after( () => {
		for ( const [key, descriptor] of saved ) {
			if ( descriptor ) Object.defineProperty( globalThis, key, descriptor );
			else Reflect.deleteProperty( globalThis, key );
		}
	} );
	const window = new EventTarget();
	Object.defineProperty( globalThis, "window", { value: window, configurable: true } );
	Object.defineProperty( globalThis, "location", { value: { origin: ORIGIN }, configurable: true } );
	const journal = createJournal( /** @type {HTMLCanvasElement} */ ({}) );
	t.after( () => journal.dispose() );
	return {
		window,
		journal,
		advance( ms ) {
			now += ms;
		},
		errors() {
			return journal.since( 0 ).filter( event => event.kind === "error" );
		}
	};
}

/*
================
failWith

An uncaught error whose stack names the page origin, like a real throw.
================
*/
function failWith( message ) {
	const error = new RangeError( message );
	error.stack =
		`RangeError: ${message}\n    at stallBuy (${ORIGIN}/src/stall.ts:10:5)\n    at frame (${ORIGIN}/src/frame.ts:20:3)`;
	return error;
}

test("an uncaught error is recorded with its stack, origin stripped", t => {
	const page = installPage( t );
	const event = new Event( "error" );
	Object.assign( event, {
		error: failWith( "Maximum call stack size exceeded" ),
		message: "Maximum call stack size exceeded"
	} );
	page.window.dispatchEvent( event );
	const [record] = page.errors();
	assert.equal( record?.message, "Maximum call stack size exceeded" );
	assert.match( String( record?.stack ), /RangeError: Maximum call stack size exceeded/ );
	assert.match( String( record?.stack ), /at stallBuy \(\/src\/stall\.ts:10:5\)/ );
	assert.ok( !String( record?.stack ).includes( ORIGIN ), "the page origin stays behind" );
});

test("an unhandled rejection is recorded with its stack", t => {
	const page = installPage( t );
	const event = new Event( "unhandledrejection" );
	Object.assign( event, { reason: failWith( "async boom" ) } );
	page.window.dispatchEvent( event );
	const [record] = page.errors();
	assert.equal( record?.message, "async boom" );
	assert.match( String( record?.stack ), /at stallBuy/ );
});

test("a thrown non-error keeps its message and gains no stack", t => {
	const page = installPage( t );
	const event = new Event( "unhandledrejection" );
	Object.assign( event, { reason: "plain string failure" } );
	page.window.dispatchEvent( event );
	const [record] = page.errors();
	assert.equal( record?.message, "Unhandled rejection: plain string failure" );
	assert.equal( record?.stack, undefined );
});

/*
================
throwNow

Dispatches one uncaught error on the fake page.
================
*/
function throwNow( page, error ) {
	const event = new Event( "error" );
	Object.assign( event, { error, message: error.message } );
	page.window.dispatchEvent( event );
}

test("repeats of one failure are one event with a count", t => {
	const page = installPage( t );
	const failure = failWith( "same throw every frame" );
	for ( let i = 0; i < 5; i++ ) {
		throwNow( page, failure );
		page.advance( 16 );
	}
	const [record, ...rest] = page.errors();
	assert.equal( rest.length, 0, "the same failure is one event" );
	assert.equal( record.count, 5 );
	assert.equal( record.atMs, 0, "the event is the first throw" );
	assert.equal( record.lastAtMs, 64, "and remembers the last" );
});

test("a storm of distinct failures keeps eight, then the ring rate-limits the rest", t => {
	const page = installPage( t );
	for ( let i = 0; i < 30; i++ ) throwNow( page, failWith( `distinct failure ${i}` ) );
	// Eight kept for the page's life, then ten in the ring per minute.
	assert.equal( page.errors().length, 18, "the storm stops at the bound and the rate limit" );
	page.advance( 61000 );
	throwNow( page, failWith( "after the window" ) );
	assert.equal( page.errors().length, 19, "failures record again once the rate window passes" );
});

test("a 1 Hz storm whose first throw is older than the window still reports its stack", t => {
	// #3425: one RangeError, then bursts of the same throw minutes later.
	// The first throw left the two-minute ring and every repeat was
	// dropped as a repeat, so the report carried no stack at all.
	const page = installPage( t );
	const failure = failWith( "Maximum call stack size exceeded" );
	throwNow( page, failure );
	page.advance( 180000 );
	for ( let i = 0; i < 30; i++ ) {
		throwNow( page, failure );
		page.advance( 1000 );
	}
	const errors = page.journal.since( performance.now() - JOURNAL_WINDOW_MS ).filter( e => e.kind === "error" );
	assert.equal( errors.length, 1 );
	assert.match( String( errors[0].stack ), /at stallBuy \(\/src\/stall\.ts:10:5\)/ );
	assert.equal( errors[0].count, 31 );
	assert.equal( errors[0].atMs, 0, "the first throw, from before the window" );
	assert.equal( errors[0].lastAtMs, 209000 );
});

test("the timeline places a kept failure's last throw on the clock of its first", t => {
	// #3462: lastAtMs went out in raw performance.now() milliseconds beside
	// t in replay seconds, so the storm's end could not be placed on t's axis.
	const page = installPage( t );
	const failure = failWith( "Maximum call stack size exceeded" );
	throwNow( page, failure );
	page.advance( 180000 );
	for ( let i = 0; i < 30; i++ ) {
		throwNow( page, failure );
		page.advance( 1000 );
	}
	const zeroMs = 200000;
	const events = timelineEvents( page.journal.since( performance.now() - JOURNAL_WINDOW_MS ), zeroMs );
	const [error] = events.filter( e => e.kind === "error" );
	assert.equal( error.t, -200, "the first throw, in seconds before the replay's first frame" );
	assert.equal( error.lastT, 9, "the last throw, on the same zero and units" );
	assert.equal( error.lastT - error.t, 209 );
	assert.equal( "atMs" in error || "lastAtMs" in error, false, "no raw clock leaves the page" );
});

test("an ungrouped event carries no lastT", t => {
	const page = installPage( t );
	page.advance( 1500 );
	page.journal.record( "ui", { control: "slot", index: 3 } );
	const [event] = timelineEvents( page.journal.since( 0 ), 1000 );
	assert.equal( event.t, 0.5 );
	assert.equal( "lastT" in event, false );
});

test("the shared report keeps a kept failure's count and last time", async () => {
	const input = {
		"timeline.json": JSON.stringify( {
			events: [ {
				kind: "error",
				t: -180,
				stack: "RangeError: Maximum call stack size exceeded\n    at loop (/src/loop.ts:1:1)",
				count: 31,
				lastT: 9
			} ]
		} )
	};
	const blob = await createDiagnosticUpload( input, { id: "BR-TEST", clip: null, maxBytes: 2 * 1024 * 1024 } );
	const entries = readZipEntries( await blob.arrayBuffer() );
	const [event] = JSON.parse( entries.get( "timeline.json" ).data.toString() ).events;
	assert.equal( event.count, 31 );
	assert.equal( event.lastT, 9 );
	assert.match( event.stack, /at loop/ );
});

test("the shared report keeps a sanitised stack, never one carrying a URL", async () => {
	const input = {
		"timeline.json": JSON.stringify( {
			events: [
				{
					kind: "error",
					t: 1,
					message: "private wording",
					stack: "RangeError: boom\n    at stallBuy (/src/stall.ts:10:5)"
				},
				{ kind: "error", t: 2, stack: "Error: x\n    at f (https://example.com/src/f.ts:1:1)" }
			]
		} )
	};
	const blob = await createDiagnosticUpload( input, { id: "BR-TEST", clip: null, maxBytes: 2 * 1024 * 1024 } );
	const entries = readZipEntries( await blob.arrayBuffer() );
	const timeline = JSON.parse( entries.get( "timeline.json" ).data.toString() );
	assert.equal( timeline.events.length, 2 );
	assert.match( timeline.events[0].stack, /at stallBuy \(\/src\/stall\.ts:10:5\)/ );
	assert.equal( timeline.events[0].message, undefined, "the free-text message stays on the player's machine" );
	assert.equal( timeline.events[1].stack, undefined, "a stack still carrying a URL scheme stays behind" );
});

test("dispose disconnects the error capture", t => {
	const page = installPage( t );
	page.journal.dispose();
	const event = new Event( "error" );
	Object.assign( event, { error: failWith( "too late" ), message: "too late" } );
	page.window.dispatchEvent( event );
	assert.equal( page.errors().length, 0 );
});
