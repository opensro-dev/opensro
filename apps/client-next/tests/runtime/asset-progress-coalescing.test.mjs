/*
===========================================================================

asset-progress-coalescing.test.mjs - loader progress is bounded, never lost

The asset loader publishes progress at most once per interval. A burst of
reads inside one interval must not post a message per read, and the state
after the burst must still arrive through one trailing publication. Also
pins the in-memory gzip path that replaced Blob-wrapped inflation.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { gzipSync } from "node:zlib";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );
const { gunzipBytes, readBytes } = await import( "../../src/engine/foundation/assets/read-bytes.ts" );
/*
================
settle
================
*/
function settle() {
	return new Promise( resolve => setImmediate( resolve ) );
}

test("a burst of reads inside one interval posts one trailing progress carrying the final state", async t => {
	let now = 0;
	t.mock.method( performance, "now", () => now );
	t.mock.method( performance, "getEntriesByName", () => [] );
	t.mock.method( globalThis, "fetch", async () => new Response( Uint8Array.of( 1, 2, 3, 4 ) ) );
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	/** @type {any[]} */
	const messages = [], loader = createLoader( message => messages.push( message ) );
	t.after( () => loader.dispose() );
	now = 1000;
	const reads = 24;
	for ( let id = 1; id <= reads; id++ ) {
		loader.receive( { kind: "load", id, url: "https://fixture.invalid/file" + id, limit: 16 } );
		await settle();
		await settle();
	}
	assert.equal( messages.filter( message => message.kind === "bytes" ).length, reads );
	const before = messages.filter( message => message.kind === "progress" );
	// The first read opens the interval; every later one only arms the trailing publication.
	assert.equal( before.length, 1 );
	t.mock.timers.tick( 150 );
	const after = messages.filter( message => message.kind === "progress" );
	assert.equal( after.length, 2 );
	assert.equal( after.at( -1 ).progress.bytesRead, reads * 4 );
	assert.equal( after.at( -1 ).progress.filesActive, 0 );
	// Nothing further is pending once the trailing publication ran.
	t.mock.timers.tick( 1000 );
	assert.equal( messages.filter( message => message.kind === "progress" ).length, 2 );
});

test("dispose retires an armed trailing progress publication", async t => {
	let now = 0;
	t.mock.method( performance, "now", () => now );
	t.mock.method( performance, "getEntriesByName", () => [] );
	t.mock.method( globalThis, "fetch", async () => new Response( Uint8Array.of( 9 ) ) );
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	/** @type {any[]} */
	const messages = [], loader = createLoader( message => messages.push( message ) );
	now = 1000;
	for ( let id = 1; id <= 2; id++ ) {
		loader.receive( { kind: "load", id, url: "https://fixture.invalid/d" + id, limit: 4 } );
		await settle();
		await settle();
	}
	const published = messages.filter( message => message.kind === "progress" ).length;
	loader.dispose();
	t.mock.timers.tick( 1000 );
	assert.equal( messages.filter( message => message.kind === "progress" ).length, published );
});

test("in-memory gzip inflates exactly, honours its limit and leaves the input intact", async () => {
	const plain = new TextEncoder().encode( "skill rows ".repeat( 2000 ) );
	const packed = new Uint8Array( gzipSync( plain ) );
	const copy = packed.slice();
	assert.deepEqual( await gunzipBytes( packed, plain.length ), plain );
	assert.deepEqual( packed, copy );
	await assert.rejects( gunzipBytes( packed, plain.length - 1 ), /exceeds byte limit/ );
	await assert.rejects( gunzipBytes( Uint8Array.of( 1, 2, 3 ), 64 ) );
});

test("a single-chunk stream returns its own bytes; several chunks concatenate", async () => {
	const one = Uint8Array.of( 5, 6, 7 );
	/*
	================
	stream
	================
	*/
	function stream( chunks ) {
		return new ReadableStream( {
			/*
			================
			start
			================
			*/
			start( controller ) {
				for ( const chunk of chunks ) controller.enqueue( chunk );
				controller.close();
			}
		} );
	}
	assert.equal( await readBytes( stream( [ one ] ), 3 ), one );
	assert.deepEqual( await readBytes( stream( [ one, Uint8Array.of( 8 ) ] ), 4 ), Uint8Array.of( 5, 6, 7, 8 ) );
	// A view onto a larger buffer is copied, never returned with foreign bytes.
	const view = new Uint8Array( new ArrayBuffer( 8 ), 2, 3 );
	const read = await readBytes( stream( [ view ] ), 3 );
	assert.notEqual( read, view );
	assert.equal( read.buffer.byteLength, 3 );
});
