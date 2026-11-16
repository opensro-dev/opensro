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

/*
================
sharedErrors

The error events of a shared report built from the given timeline events.
================
*/
async function sharedErrors( events ) {
	const input = { "timeline.json": JSON.stringify( { events } ) };
	const blob = await createDiagnosticUpload( input, { id: "BR-TEST", clip: null, maxBytes: 2 * 1024 * 1024 } );
	const entries = readZipEntries( await blob.arrayBuffer() );
	return JSON.parse( entries.get( "timeline.json" ).data.toString() ).events;
}

test("the shared report keeps a stack's frames but never the thrown free text", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 1,
		message: "private wording",
		stack: "RangeError: private wording\n    at stallBuy (/src/stall.ts:10:5)"
	} ] );
	assert.match( event.stack, /at stallBuy \(\/src\/stall\.ts:10:5\)/ );
	assert.equal( event.errorClass, "RangeError" );
	assert.equal( event.message, undefined, "the free-text message stays on the player's machine" );
	assert.ok( !event.stack.includes( "private wording" ), "nor rides along in the stack's header" );
});

test("a stack carrying an origin keeps its frames without scheme, host or query", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 2,
		message: "Maximum call stack size exceeded",
		stack: "RangeError: Maximum call stack size exceeded\n" +
			"    at walk (https://play.example.com/assets/index-abc.js?v=7:1:2345)\n" +
			"    at step (blob:https://play.example.com/0f3c-11aa:9:8)\n" +
			"    at run@https://play.example.com/assets/main.js#top:3:4"
	} ] );
	assert.equal( event.errorClass, "RangeError" );
	assert.equal( event.message, "Maximum call stack size exceeded", "an engine text names the failure" );
	assert.equal(
		event.stack,
		"RangeError: Maximum call stack size exceeded\n" +
			"    at walk (/assets/index-abc.js:1:2345)\n" +
			"    at step (/0f3c-11aa:9:8)\n" +
			"    at run@/assets/main.js:3:4"
	);
	assert.ok( !/example|:\/\/|\?v=/.test( event.stack ) );
});

test("a deep stack keeps its first twelve frames", async () => {
	const frames = Array.from( { length: 40 }, ( _, i ) => `    at f${i} (/src/f.ts:${i}:1)` );
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 3,
		stack: [ "RangeError: Maximum call stack size exceeded", ...frames ].join( "\n" )
	} ] );
	const kept = event.stack.split( "\n" );
	assert.equal( kept.length, 13 );
	assert.equal( kept[1], frames[0] );
	assert.equal( kept[12], frames[11] );
});

test("a multi-line message never ships a line as a frame, even one with an @", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 5,
		message: "chat said hi\nmeet me at a@b.c",
		stack: "TypeError: chat said hi\nmeet me at a@b.c\nfoo@bar.baz\n    at send (/src/chat.ts:4:2)"
	} ] );
	assert.equal( event.stack, "TypeError\n    at send (/src/chat.ts:4:2)" );
	assert.ok( !/@|meet|chat said/.test( event.stack ) );
});

test("a headerless Firefox stack keeps its frames, and only its frames", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 6,
		message: "too much recursion",
		stack: "walk@https://play.example.com/assets/index.js?v=3:1:200\n" +
			"step@https://play.example.com/assets/index.js:9:8\n" +
			"meet me at a@b.c\n"
	} ] );
	assert.equal( event.errorClass, undefined, "Firefox names no class in its stack" );
	assert.equal( event.message, "too much recursion" );
	assert.equal( event.stack, "Error: too much recursion\nwalk@/assets/index.js:1:200\nstep@/assets/index.js:9:8" );
});

test("a local file frame keeps only the file's name", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 7,
		stack: "RangeError: Maximum call stack size exceeded\n    at walk (file:///C:/Users/someone/game/index.js:1:2)"
	} ] );
	assert.equal( event.stack, "RangeError\n    at walk (index.js:1:2)" );
});

// opensro-web2-claude's review probe on #3702, kept as written.
test("review probe: a message line shaped like a V8 frame never ships", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 6,
		message: "parse failed\n    at PLAYER TEXT HERE:1:2",
		stack: "TypeError: parse failed\n    at PLAYER TEXT HERE:1:2\n    at send (/src/chat.ts:4:2)"
	} ] );
	assert.ok( !/PLAYER TEXT/.test( event.stack ?? "" ), event.stack );
	assert.match( event.stack, /at send/ );
});

test("the journal's header count skips a long message's every line, even past its slice", async t => {
	const page = installPage( t );
	const message = "x".repeat( 600 ) + "\n    at PLAYER TEXT HERE:1:2";
	const error = new TypeError( message );
	error.stack = `TypeError: ${message}\n    at send (${ORIGIN}/src/chat.ts:4:2)`;
	const event = new Event( "error" );
	Object.assign( event, { error, message } );
	page.window.dispatchEvent( event );
	const [record] = page.errors();
	assert.equal( record?.headerLines, 2, "counted from the whole message, not the 500-character slice" );
	assert.equal( String( record?.message ).length, 500 );
	const [shared] = await sharedErrors( [ { ...record, t: 1 } ] );
	assert.equal( shared.stack, "TypeError\n    at send (/src/chat.ts:4:2)" );
	assert.equal( shared.headerLines, undefined, "the count itself is never shared" );
});

test("a message at the journal's cap with no header count ships no frames", async () => {
	const [event] = await sharedErrors( [ {
		kind: "error",
		t: 7,
		message: "x".repeat( 500 ),
		stack: "TypeError: " + "x".repeat( 500 ) + "\n    at PLAYER TEXT HERE:1:2\n    at send (/src/chat.ts:4:2)"
	} ] );
	assert.equal( event.stack, undefined, "the cut message's true length is unknown" );
	assert.equal( event.errorClass, "TypeError" );
});

test("a thrown non-error shares neither a class nor a message", async () => {
	const [event] = await sharedErrors( [ { kind: "error", t: 4, message: "a player wrote this" } ] );
	assert.equal( event.errorClass, undefined );
	assert.equal( event.message, undefined );
	assert.equal( event.stack, undefined );
	assert.equal( event.kind, "error" );
});

test("dispose disconnects the error capture", t => {
	const page = installPage( t );
	page.journal.dispose();
	const event = new Event( "error" );
	Object.assign( event, { error: failWith( "too late" ), message: "too late" } );
	page.window.dispatchEvent( event );
	assert.equal( page.errors().length, 0 );
});
