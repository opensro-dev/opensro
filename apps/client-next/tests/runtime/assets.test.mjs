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
		if ( result.kind === "progress" ) return;
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
			if ( result.kind !== "progress" ) results.push( result );
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
			if ( result.kind !== "progress" ) results.push( result );
		} );
	for ( let id = 1; id <= 5; id++ ) loader.receive( { kind: "load", id, url: "http://localhost/" + id, limit: 3 } );
	assert.equal( signals.length, 4 );
	assert.equal( results[0].id, 5 );
	assert.equal( results[0].kind, "error" );
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
			if ( result.kind !== "progress" ) results.push( result );
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
	assert.equal( results.length, 8 );
	assert.ok( results.every( r => r.kind === "error" ) );
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
		8,
		"cancelled or disposed work never publishes a payload"
	);
	assert.deepEqual( results.filter( r => r.kind === "released" ), [ { kind: "released", id: 1 } ] );
});

test("cancelled downloads retain capacity even when fetch ignores abort, then recover", async t => {
	const downloads = [];
	t.mock.method( globalThis, "fetch", () => new Promise( resolve => downloads.push( resolve ) ) );
	const results = [],
		loader = createLoader( result => {
			if ( result.kind !== "progress" ) results.push( result );
		} );
	t.after( () => loader.dispose() );
	const request = id => loader.receive( { kind: "load", id, url: "http://localhost/data", limit: 1 } );
	for ( let id = 1; id <= 4; id++ ) {
		request( id );
		loader.receive( { kind: "cancel", id } );
	}
	request( 5 );
	assert.equal( downloads.length, 4 );
	assert.equal( results[0].kind, "error" );
	downloads[0]( new Response( Uint8Array.of( 1 ) ) );
	await settle();
	request( 6 );
	assert.equal( downloads.length, 5 );
	downloads[4]( new Response( Uint8Array.of( 2 ) ) );
	await settle();
	assert.equal( results.at( -1 ).id, 6 );
	assert.equal( results.at( -1 ).kind, "bytes" );
	for ( const resolve of downloads.slice( 1, 4 ) ) resolve( new Response( Uint8Array.of( 1 ) ) );
	await settle();
	assert.equal( results.filter( r => r.kind !== "released" ).length, 2 );
	assert.equal( results.filter( r => r.kind === "released" ).length, 4 );
});
