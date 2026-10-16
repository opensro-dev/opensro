/*
===========================================================================

asset-cancel-ack.test.mjs - a late cancellation never kills the asset runtime

A cancelled request keeps its worker slot until the worker posts "released".
Shared pack and range downloads leave their wait on abort (packs.ts
untilCancelled), but a worker inside a native decode that AbortSignal cannot
interrupt (createImageBitmap, DecompressionStream) can still release after
the cancellation window. The main-thread owner must abandon the overdue job,
report asset-cancel-unacknowledged once, and survive the late release.

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

const PROGRAMS = "https://assets.test/assets/effects/programs.json";

/*
================
fixture

createAssets over a stub worker and a controllable clock.
================
*/
function fixture( t ) {
	const old = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	/** @type {{ messages: unknown[]; onmessage: ( event: unknown ) => void; } | undefined} */
	let worker;
	class Worker {
		/** @type {unknown[]} */
		messages = [];
		/** @type {( event: unknown ) => void} */
		onmessage = () => {};
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
		terminate() {}
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
	return { assets, worker, clock };
}

test("a release that arrives after the request deadline does not fail the asset runtime", t => {
	const { assets, worker, clock } = fixture( t );
	const id = assets.request( PROGRAMS );
	clock.now += 100000;
	assets.cancel( id );
	assert.deepEqual( defined( worker ).messages.at( -1 ), { kind: "cancel", id } );
	// At 125 s the request's own deadline has passed; the release has not.
	clock.now += 25000;
	const health = assets.health();
	assert.equal( health.phase, "running", `asset runtime failed: ${health.error}` );
	// The worker finishes its uninterruptible work at 177 s and releases the slot.
	clock.now += 52000;
	assert.equal( assets.health().phase, "running" );
	defined( worker ).onmessage( { data: { kind: "released", id } } );
	assert.equal( assets.available(), 4 );
	assert.equal( assets.health().phase, "running" );
	// The late acknowledgement was reported once, never silently passed.
	assert.deepEqual( assets.takeDegradations().map( d => d.code ), [ "asset-cancel-unacknowledged" ] );
});
