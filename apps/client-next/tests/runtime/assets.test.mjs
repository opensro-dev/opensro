/*
===========================================================================

assets.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

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
const { createLoader } = await load( "src/engine/runtime/assets/worker/loader.ts" );
const { readBytes } = await load( "src/engine/foundation/assets/read-bytes.ts" );
const settle = () => new Promise( resolve => setImmediate( resolve ) );
test("bounded reader cancels oversized streams and releases their reader", async () => {
	let cancelled = false;
	const stream = new ReadableStream( {
		/*
================
start
================
		*/
		start( c ) {
			c.enqueue( Uint8Array.of( 1, 2, 3 ) );
		},
		/*
================
cancel
================
		*/
		cancel() {
			cancelled = true;
		}
	} );
	await assert.rejects( readBytes( stream, 2 ), /limit/ );
	assert.ok( cancelled );
	assert.equal( stream.locked, false );
});
test("asset worker transfers exact bytes without retaining the output buffer", async t => {
	t.mock.method( globalThis, "fetch", async () => new Response( Uint8Array.of( 4, 5, 6 ) ) );
	const results = [];
	let transferred;
	const loader = createLoader( ( result, transfer ) => {
		// Progress is time-based telemetry (sent after 150 ms), so a loaded
		// machine can emit one mid-test; only deliveries are under test.
		if ( [ "progress", "job-progress", "job-decoding", "job-fetching" ].includes( result.kind ) ) return;
		transferred = result.buffer;
		results.push( structuredClone( result, { transfer } ) );
	} );
	loader.receive( { kind: "load", id: 1, url: "http://localhost/asset", limit: 3 } );
	await settle();
	assert.equal( results.length, 1 );
	assert.deepEqual( [ ...new Uint8Array( results[0].buffer ) ], [ 4, 5, 6 ] );
	assert.equal( defined( transferred ).byteLength, 0 );
	loader.dispose();
});
test("asset cancellation rejects late completion even if fetch ignores abort", async t => {
	let resolveRequest, signal;
	t.mock.method( globalThis, "fetch", ( _url, options ) => {
		signal = options.signal;
		return new Promise( resolve => resolveRequest = resolve );
	} );
	const results = [],
		loader = createLoader( result => {
			if ( ![ "progress", "job-progress", "job-decoding", "job-fetching" ].includes( result.kind ) ) {
				results.push( result );
			}
		} );
	loader.receive( { kind: "load", id: 1, url: "http://localhost/asset", limit: 3 } );
	loader.receive( { kind: "cancel", id: 1 } );
	assert.ok( defined( signal ).aborted );
	defined( resolveRequest )( new Response( Uint8Array.of( 1 ) ) );
	await settle();
	assert.deepEqual( results, [ { kind: "released", id: 1 } ] );
	loader.dispose();
});
test("asset loader bounds concurrency and aborts every pending request on disposal", t => {
	const signals = [];
	t.mock.method( globalThis, "fetch", ( _url, options ) => {
		signals.push( options.signal );
		return new Promise( () => {} );
	} );
	const results = [],
		loader = createLoader( result => {
			if ( ![ "progress", "job-progress", "job-decoding", "job-fetching" ].includes( result.kind ) ) {
				results.push( result );
			}
		} );
	for ( let id = 1; id <= 5; id++ ) loader.receive( { kind: "load", id, url: "http://localhost/" + id, limit: 3 } );
	// The fifth demand waits for an execution slot; it is not refused.
	assert.equal( signals.length, 4 );
	assert.deepEqual( results, [] );
	loader.dispose();
	assert.ok( signals.every( signal => signal.aborted ) );
});

test("cancelled native decodes retain execution capacity until settlement", async t => {
	const png = new Uint8Array( 24 ), view = new DataView( png.buffer );
	[ 0x89504e47, 0x0d0a1a0a, 13, 0x49484452, 4096, 4096 ].forEach( ( n, i ) => view.setUint32( i * 4, n ) );
	t.mock.method( globalThis, "fetch", async () => new Response( png ) );
	const previous = Object.getOwnPropertyDescriptor( globalThis, "createImageBitmap" );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "createImageBitmap", previous );
		else delete globalThis.createImageBitmap;
	} );
	const decodes = [];
	let active = 0, peak = 0, closed = 0;
	globalThis.createImageBitmap = () => {
		active++;
		peak = Math.max( peak, active );
		return new Promise( resolve =>
			decodes.push( () => {
				active--;
				resolve( {
					width: 4096,
					height: 4096,
					/*
================
close
================
					*/
					close() {
						closed++;
					}
				} );
			} )
		);
	};
	const results = [],
		loader = createLoader( result => {
			if ( ![ "progress", "job-progress", "job-decoding", "job-fetching" ].includes( result.kind ) ) {
				results.push( result );
			}
		} );
	t.after( () => loader.dispose() );
	const request = id =>
		loader.receive( { kind: "load", id, url: "http://localhost/image", limit: 24, decode: "png" } );
	for ( let id = 1; id <= 12; id++ ) {
		request( id );
		await settle();
		loader.receive( { kind: "cancel", id } );
	}
	assert.equal( peak, 4 );
	assert.equal( decodes.length, 4 );
	// Every cancellation was acknowledged on receipt, while its decode
	// (or its wait for one) still holds the execution slot.
	assert.equal( results.length, 12 );
	assert.ok( results.every( r => r.kind === "released" ) );
	decodes[0]();
	await settle();
	assert.equal( closed, 1 );
	request( 13 );
	await settle();
	assert.equal( decodes.length, 5 );
	assert.equal( peak, 4 );
	loader.dispose();
	for ( const finish of decodes.slice( 1 ) ) finish();
	await settle();
	assert.equal( closed, 5 );
	assert.equal(
		results.filter( r => r.kind !== "released" ).length,
		0,
		"cancelled or disposed work never publishes a payload"
	);
	assert.deepEqual(
		results.filter( r => r.kind === "released" ).map( r => r.id ),
		[ 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12 ],
		"every cancellation is acknowledged on receipt"
	);
});

test("cancelled downloads retain capacity even when fetch ignores abort, then recover", async t => {
	const downloads = [];
	t.mock.method( globalThis, "fetch", () => new Promise( resolve => downloads.push( resolve ) ) );
	const results = [],
		loader = createLoader( result => {
			if ( ![ "progress", "job-progress", "job-decoding", "job-fetching" ].includes( result.kind ) ) {
				results.push( result );
			}
		} );
	t.after( () => loader.dispose() );
	const request = id => loader.receive( { kind: "load", id, url: "http://localhost/data", limit: 1 } );
	for ( let id = 1; id <= 4; id++ ) {
		request( id );
		loader.receive( { kind: "cancel", id } );
	}
	// Demand past the four draining executions waits instead of erroring;
	// the four cancellations were already acknowledged on receipt.
	request( 5 );
	assert.equal( downloads.length, 4 );
	assert.equal( results.filter( r => r.kind === "released" ).length, 4 );
	assert.equal( results.filter( r => r.kind === "error" ).length, 0 );
	downloads[0]( new Response( Uint8Array.of( 1 ) ) );
	await settle();
	// The first drain settled: the waiting demand took its slot.
	assert.equal( downloads.length, 5 );
	request( 6 );
	assert.equal( downloads.length, 5, "the sixth demand waits behind the running fifth" );
	downloads[4]( new Response( Uint8Array.of( 2 ) ) );
	await settle();
	assert.equal( results.at( -1 ).id, 5 );
	assert.equal( results.at( -1 ).kind, "bytes" );
	assert.equal( downloads.length, 6, "the sixth demand starts as the fifth settles" );
	for ( const resolve of downloads.slice( 1, 4 ) ) resolve( new Response( Uint8Array.of( 1 ) ) );
	await settle();
	assert.equal( results.filter( r => r.kind !== "released" ).length, 1 );
	assert.equal( results.filter( r => r.kind === "released" ).length, 4 );
});

test("a cancellation is acknowledged on receipt while its decode still drains", async t => {
	// #1427: the cancelled programs.json load sat inside work AbortSignal
	// cannot interrupt, so its settlement - the old acknowledgement - ran
	// past the 30 s window. The acknowledgement now answers the receipt;
	// only the execution slot still waits for the drain.
	const png = new Uint8Array( 24 ), view = new DataView( png.buffer );
	[ 0x89504e47, 0x0d0a1a0a, 13, 0x49484452, 4096, 4096 ].forEach( ( n, i ) => view.setUint32( i * 4, n ) );
	t.mock.method( globalThis, "fetch", async () => new Response( png ) );
	const previous = Object.getOwnPropertyDescriptor( globalThis, "createImageBitmap" );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "createImageBitmap", previous );
		else delete globalThis.createImageBitmap;
	} );
	const decodes = [];
	globalThis.createImageBitmap = () =>
		new Promise( resolve => decodes.push( () => resolve( { width: 4096, height: 4096, close() {} } ) ) );
	const results = [],
		loader = createLoader( result => {
			if ( ![ "progress", "job-progress", "job-decoding", "job-fetching" ].includes( result.kind ) ) {
				results.push( result );
			}
		} );
	t.after( () => loader.dispose() );
	const request = id =>
		loader.receive( { kind: "load", id, url: "http://localhost/image", limit: 24, decode: "png" } );
	for ( let id = 1; id <= 4; id++ ) request( id );
	await settle();
	assert.equal( decodes.length, 4, "four decodes are parked inside createImageBitmap" );
	loader.receive( { kind: "cancel", id: 1 } );
	// No settle, no decode resolution: the acknowledgement already stands.
	assert.deepEqual( results, [ { kind: "released", id: 1 } ] );
	request( 5 );
	await settle();
	assert.equal( decodes.length, 4, "the draining decode still holds its execution slot" );
	decodes[0]();
	await settle();
	assert.equal( decodes.length, 5, "the waiting demand starts when the drain settles" );
	loader.receive( { kind: "cancel", id: 5 } );
	decodes[4]();
	await settle();
	assert.deepEqual(
		results,
		[ { kind: "released", id: 1 }, { kind: "released", id: 5 } ],
		"neither drain publishes a payload or a second acknowledgement"
	);
});
