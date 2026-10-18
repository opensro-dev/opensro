/*
===========================================================================

asset-watchdog-progress.test.mjs - a slow link is slow, never fatal

The asset watchdog used to be a flat 120 s total cap: a route whose bytes
keep trickling (production measured one at 40 KB/s) crossed it mid-load and
the whole runtime failed. The watchdog is now a stall window the worker
slides with per-job byte notes, a shorter fixed window once the primary
bytes are in and only decode remains, and an absolute ceiling even a
trickle cannot cross. Expiry is per job and transient; the worker and the
other jobs carry on.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createAssets } = await load( "src/engine/runtime/assets/assets.ts" );

const REGION = "https://assets.test/assets/world/outdoor/regions/region-62a8.json";
const TIMEOUT_ERROR = "Asset worker timed out while loading /assets/world/outdoor/regions/region-62a8.json";

/*
================
fixture

createAssets over a stub worker and a controllable clock.
================
*/
function fixture( t ) {
	const old = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	/** @type {{ messages: { kind: string; id?: number; }[]; onmessage: ( event: unknown ) => void; terminated: boolean; } | undefined} */
	let worker;
	class Worker {
		/** @type {{ kind: string; id?: number; }[]} */
		messages = [];
		/** @type {( event: unknown ) => void} */
		onmessage = () => {};
		terminated = false;
		constructor() {
			worker = this;
		}
		/*
		================
		postMessage
		================
		*/
		postMessage( m ) {
			this.messages.push( m );
		}
		/*
		================
		terminate
		================
		*/
		terminate() {
			this.terminated = true;
		}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: Worker } );
	const clock = { now: 1000 };
	t.mock.method( performance, "now", () => clock.now );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "Worker", old );
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	const assets = createAssets();
	t.after( () => assets.dispose() );
	return { assets, worker: defined( worker ), clock };
}

test("byte progress keeps a slow load alive far past the old 120 s total deadline", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	// A trickling route: a byte note every 100 s, well inside the stall window.
	for ( let step = 1; step <= 5; step++ ) {
		clock.now += 100000;
		worker.onmessage( { data: { kind: "job-progress", id, bytes: step * 500 } } );
		assert.equal( assets.health().phase, "running" );
		assert.equal( assets.take( id ), null, "a moving load must not complete or fail" );
	}
	clock.now += 100000;
	worker.onmessage( { data: { kind: "bytes", id, buffer: new ArrayBuffer( 8 ) } } );
	const result = assets.take( id );
	assert.equal( result?.kind, "bytes" );
	assert.equal( worker.terminated, false );
	assert.deepEqual( worker.messages.filter( m => m.kind === "cancel" ), [] );
});

test("silence expires one job with the transient timeout error and the runtime lives", t => {
	const { assets, worker, clock } = fixture( t );
	const slow = assets.request( REGION );
	const other = assets.request( "https://assets.test/assets/ui/text.json" );
	// The other job completes at once; only the slow one is still loading.
	worker.onmessage( { data: { kind: "bytes", id: other, buffer: new ArrayBuffer( 4 ) } } );
	clock.now += 121000;
	assert.equal( assets.health().phase, "running" );
	assert.equal( worker.terminated, false );
	assert.deepEqual( worker.messages.filter( m => m.kind === "cancel" ), [ { kind: "cancel", id: slow } ] );
	assert.deepEqual( assets.take( slow ), { kind: "error", id: slow, error: TIMEOUT_ERROR, transient: true } );
	// The other job's completion is untouched by the slow job's expiry.
	assert.equal( assets.take( other )?.kind, "bytes" );
	// The expired job keeps its slot until the worker acknowledges the
	// cancel: the worker still counts it against its own capacity.
	assert.equal( assets.available(), 3 );
	worker.onmessage( { data: { kind: "released", id: slow } } );
	assert.equal( assets.available(), 4 );
});

test("an endless trickle still dies at the absolute ceiling", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	for ( let step = 1; step <= 59; step++ ) {
		clock.now += 10000;
		worker.onmessage( { data: { kind: "job-progress", id, bytes: step * 100 } } );
	}
	// 590 s in and still moving: alive.
	assert.equal( assets.health().phase, "running" );
	assert.equal( assets.take( id ), null );
	clock.now += 11000;
	assert.equal( assets.health().phase, "running" );
	assert.deepEqual( assets.take( id ), { kind: "error", id, error: TIMEOUT_ERROR, transient: true } );
});

test("the decode window runs from the fetch-complete mark and is shorter than the stall window", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 10000;
	worker.onmessage( { data: { kind: "job-decoding", id } } );
	// 59 s of decode silence: alive. 61 s: the decode window has run out,
	// long before the 120 s stall window would have.
	clock.now += 59000;
	assert.equal( assets.take( id ), null );
	clock.now += 2000;
	assert.equal( assets.health().phase, "running" );
	assert.deepEqual( assets.take( id ), { kind: "error", id, error: TIMEOUT_ERROR, transient: true } );
});

test("byte progress during decode slides the decode window, never the ceiling", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 10000;
	worker.onmessage( { data: { kind: "job-decoding", id } } );
	// A decoder read at 50 s of decode buys another decode window.
	clock.now += 50000;
	worker.onmessage( { data: { kind: "job-progress", id, bytes: 42 } } );
	clock.now += 59000;
	assert.equal( assets.take( id ), null, "a decoder read must slide the decode window" );
	clock.now += 2000;
	assert.equal( assets.health().phase, "running" );
	assert.deepEqual( assets.take( id ), { kind: "error", id, error: TIMEOUT_ERROR, transient: true } );
});

test("an expired job's late completion and release are inert", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 121000;
	assets.health();
	// The worker finishes anyway, then releases the slot: neither changes
	// the delivered error, and neither faults the runtime.
	worker.onmessage( { data: { kind: "bytes", id, buffer: new ArrayBuffer( 8 ) } } );
	worker.onmessage( { data: { kind: "released", id } } );
	assert.equal( assets.health().phase, "running" );
	assert.deepEqual( assets.take( id ), { kind: "error", id, error: TIMEOUT_ERROR, transient: true } );
	// A release arriving after collection is likewise expected.
	worker.onmessage( { data: { kind: "released", id } } );
	assert.equal( assets.health().phase, "running" );
	assert.equal( assets.available(), 4 );
});

test("an expired job holds its slot until the worker acknowledges, then frees it", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 121000;
	assets.health();
	assert.deepEqual( assets.take( id ), { kind: "error", id, error: TIMEOUT_ERROR, transient: true } );
	// The error is delivered once, and the slot is still the job's: a retry
	// must wait for the acknowledgement, never over-admit into the worker.
	assert.equal( assets.take( id ), null );
	assert.equal( assets.available(), 3 );
	worker.onmessage( { data: { kind: "released", id } } );
	assert.equal( assets.available(), 4 );
	assert.equal( assets.health().phase, "running" );
});

test("a wedged expired job is abandoned and reported, like a wedged cancellation", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 121000;
	assets.health();
	assets.take( id );
	// The worker never acknowledges (an uninterruptible decode outlives the
	// acknowledgement window): the slot stays reserved and the fault is
	// reported once, but the runtime lives.
	clock.now += 31000;
	assert.equal( assets.health().phase, "running" );
	assert.equal( assets.available(), 3 );
	assert.deepEqual( assets.takeDegradations().map( d => d.code ), [ "asset-cancel-unacknowledged" ] );
});

test("a decode-phase download flips the job back to the download window", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 10000;
	worker.onmessage( { data: { kind: "job-decoding", id } } );
	clock.now += 10000;
	worker.onmessage( { data: { kind: "job-fetching", id } } );
	// 100 s of silence in the restored download window: alive (the 60 s
	// decode window would have killed it). At 121 s it expires.
	clock.now += 100000;
	assert.equal( assets.take( id ), null );
	clock.now += 21000;
	assert.equal( assets.health().phase, "running" );
	assert.deepEqual( assets.take( id ), { kind: "error", id, error: TIMEOUT_ERROR, transient: true } );
});

test("a healthy fast load behaves exactly as before", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( REGION );
	clock.now += 30000;
	worker.onmessage( { data: { kind: "bytes", id, buffer: new ArrayBuffer( 8 ) } } );
	assert.equal( assets.take( id )?.kind, "bytes" );
	assert.equal( assets.health().phase, "running" );
	assert.equal( worker.terminated, false );
	assert.deepEqual( worker.messages.filter( m => m.kind === "cancel" ), [] );
});
