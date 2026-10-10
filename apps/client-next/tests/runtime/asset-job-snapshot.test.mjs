/*
===========================================================================

asset-job-snapshot.test.mjs - observe saturated asset handles without consuming them

Distinguishes pending work, an uncollected result and cancellation while proving
that diagnostic reads preserve the consumer's result and capacity ownership.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createAssets } = await import( "../../src/engine/runtime/assets/assets.ts" );

test("asset dump distinguishes held slots without consuming or exposing credentials", t => {
	/** @type {FixtureWorker | undefined} */
	let worker;
	let now = 100;
	t.mock.method( performance, "now", () => now );
	const previous = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	/*
	================
	FixtureWorker
	================
	*/
	class FixtureWorker {
		/** @type {((event: {data: unknown}) => void) | null} */
		onmessage = null;
		/** @type {(() => void) | null} */
		onerror = null;
		/*
		================
		constructor
		================
		*/
		constructor() {
			worker = this;
		}
		/*
		================
		postMessage
		================
		*/
		postMessage() {}
		/*
		================
		terminate
		================
		*/
		terminate() {}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: FixtureWorker } );
	const assets = createAssets();
	t.after( () => {
		assets.dispose();
		if ( previous ) Object.defineProperty( globalThis, "Worker", previous );
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	const ids = Array.from(
		{ length: 4 },
		( _, i ) => assets.request( `https://user:password@assets.test/assets/${i}?token=secret#private` )
	);
	assert.ok( worker );
	const result = { kind: "bytes", id: ids[1], buffer: new ArrayBuffer( 3 ) };
	worker.onmessage?.( { data: result } );
	assets.cancel( ids[2] );
	now += 2000;
	const read = assets.snapshot;
	assert.ok( read );
	const snapshot = read();
	assert.equal( snapshot.phase, "running" );
	assert.equal( snapshot.available, 0 );
	assert.deepEqual( snapshot.jobs.map( row => row.state ), [ "loading", "completed", "cancelling", "loading" ] );
	assert.ok( snapshot.jobs.every( row => row.ageMs === 2000 ) );
	assert.equal( snapshot.jobs[1].result, "bytes" );
	assert.doesNotMatch( JSON.stringify( snapshot ), /password|token|secret|private|assets.test/ );
	Reflect.set( snapshot.jobs, "length", 0 );
	assert.equal( read().jobs.length, 4 );
	assert.equal( assets.available(), 0 );
	assert.equal( assets.take( ids[1] ), result );
	assert.equal( assets.available(), 1 );
	worker.onmessage?.( { data: { kind: "released", id: ids[2] } } );
	assert.equal( assets.available(), 2 );
	worker.onerror?.();
	assert.equal( read().phase, "failed" );
	assert.equal( read().error, "Asset worker failed" );
});
