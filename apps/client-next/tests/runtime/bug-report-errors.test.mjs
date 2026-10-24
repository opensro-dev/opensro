/*
===========================================================================

bug-report-errors.test.mjs - uncaught failures reach the report timeline

A stall with no exception anywhere in the report used to end the
investigation: the journal recorded a bare message for runtime failures
the game caught itself, and an uncaught throw (a RangeError inside a
frame loop, a rejected promise) left no stack to read. The journal now
records uncaught errors and rejections with their stacks, sanitised and
rate-limited so a throwing loop cannot evict the player's input history.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readZipEntries } from "../helpers/zip-reader.mjs";
const { createJournal } = await import( "../../src/engine/runtime/bug-report/journal.ts" );
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

test("repeats and storms are rate-limited, then the window reopens", t => {
	const page = installPage( t );
	const failure = failWith( "same throw every frame" );
	for ( let i = 0; i < 5; i++ ) {
		const event = new Event( "error" );
		Object.assign( event, { error: failure, message: failure.message } );
		page.window.dispatchEvent( event );
	}
	assert.equal( page.errors().length, 1, "the same failure is recorded once" );
	for ( let i = 0; i < 15; i++ ) {
		const event = new Event( "error" );
		Object.assign( event, { error: failWith( `distinct failure ${i}` ), message: `distinct failure ${i}` } );
		page.window.dispatchEvent( event );
	}
	assert.equal( page.errors().length, 10, "the storm stops at the rate limit" );
	page.advance( 61000 );
	const event = new Event( "error" );
	Object.assign( event, { error: failWith( "after the window" ), message: "after the window" } );
	page.window.dispatchEvent( event );
	assert.equal( page.errors().length, 11, "failures record again once the window passes" );
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
