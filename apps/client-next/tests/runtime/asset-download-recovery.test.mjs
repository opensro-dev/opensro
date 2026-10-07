/*
===========================================================================

asset-download-recovery.test.mjs - bounded recovery at the asset transport

Exercise the shipping worker with failed fetches and interrupted bodies.
Mock time keeps backoff deterministic; cancellation and integrity failures
must retain their original meaning.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );

/*
================
settle
================
*/
const settle = () => new Promise( resolve => setImmediate( resolve ) );

/*
================
fixture
================
*/
function fixture( t, fetch ) {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	t.mock.method( globalThis, "fetch", fetch );
	const messages = [];
	const loader = createLoader( message => {
		if ( message.kind !== "progress" ) messages.push( message );
	} );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://example.test/asset", limit: 3 } );
	return { loader, messages };
}

for ( const status of [ null, 408, 429, 500, 502, 503, 504 ] ) {
	test(`asset transport recovers after ${status ?? "network rejection"}`, async t => {
		let attempts = 0;
		const { messages } = fixture( t, async () => {
			if ( ++attempts === 1 ) {
				if ( status === null ) throw new TypeError( "Load failed" );
				return new Response( "unavailable", { status } );
			}
			return new Response( Uint8Array.of( 1, 2, 3 ) );
		} );
		await settle();
		assert.equal( attempts, 1 );
		assert.equal( messages.length, 0 );
		t.mock.timers.tick( 249 );
		await settle();
		assert.equal( attempts, 1 );
		t.mock.timers.tick( 1 );
		await settle();
		assert.equal( attempts, 2 );
		assert.equal( messages.length, 1 );
		assert.equal( messages[0].kind, "bytes" );
		assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 1, 2, 3 ] );
	});
}

test("interrupted response body restarts the read without publishing partial bytes", async t => {
	let attempts = 0;
	const { messages } = fixture( t, async () => {
		if ( ++attempts !== 1 ) return new Response( Uint8Array.of( 4, 5, 6 ) );
		let reads = 0;
		return new Response(
			new ReadableStream( {
				/*
			================
			pull
			================
			*/
				pull( controller ) {
					if ( reads++ === 0 ) controller.enqueue( Uint8Array.of( 9 ) );
					else controller.error( new TypeError( "Load failed" ) );
				}
			} )
		);
	} );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2 );
	assert.equal( messages.length, 1 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 4, 5, 6 ] );
});

test("persistent transport failure exhausts exactly three attempts", async t => {
	let attempts = 0;
	const { messages } = fixture( t, async () => {
		attempts++;
		throw new TypeError( "Load failed" );
	} );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2 );
	t.mock.timers.tick( 999 );
	await settle();
	assert.equal( attempts, 2 );
	t.mock.timers.tick( 1 );
	await settle();
	assert.equal( attempts, 3 );
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "error" );
	assert.match( messages[0].error, /Load failed/ );
	assert.equal( messages[0].transient, true );
	t.mock.timers.tick( 60000 );
	await settle();
	assert.equal( attempts, 3 );
});

for ( const dispose of [ false, true ] ) {
	test(`backoff is cancelled promptly by ${dispose ? "disposal" : "request cancellation"}`, async t => {
		let attempts = 0;
		const { loader, messages } = fixture( t, async () => {
			attempts++;
			throw new TypeError( "Load failed" );
		} );
		await settle();
		if ( dispose ) loader.dispose();
		else loader.receive( { kind: "cancel", id: 1 } );
		await settle();
		assert.deepEqual( messages, dispose ? [] : [ { kind: "released", id: 1 } ] );
		t.mock.timers.tick( 60000 );
		await settle();
		assert.equal( attempts, 1 );
	});
}

for ( const status of [ 400, 401, 403, 404, 410 ] ) {
	test(`permanent HTTP ${status} is not retried`, async t => {
		let attempts = 0;
		const { messages } = fixture( t, async () => {
			attempts++;
			return new Response( "unavailable", { status } );
		} );
		await settle();
		t.mock.timers.tick( 60000 );
		await settle();
		assert.equal( attempts, 1 );
		assert.equal( messages[0].kind, "error" );
		assert.match( messages[0].error, new RegExp( `Asset HTTP ${status}` ) );
		assert.equal( messages[0].transient, undefined );
	});
}

test("oversized response remains a permanent byte-budget failure", async t => {
	let attempts = 0;
	const { messages } = fixture( t, async () => {
		attempts++;
		return new Response( Uint8Array.of( 1, 2, 3, 4 ) );
	} );
	await settle();
	t.mock.timers.tick( 60000 );
	await settle();
	assert.equal( attempts, 1 );
	assert.equal( messages[0].kind, "error" );
	assert.match( messages[0].error, /byte limit/ );
	assert.equal( messages[0].transient, undefined );
});

/*
================
failedBody
================
*/
function failedBody() {
	return new ReadableStream( {
		/*
		================
		start
		================
		*/
		start( controller ) {
			controller.error( new TypeError( "body disconnected" ) );
		}
	} );
}

test("HTTP failure survives a rejected response-body cleanup", async t => {
	let attempts = 0;
	const { messages } = fixture( t, async () => {
		attempts++;
		return new Response( failedBody(), { status: 404 } );
	} );
	await settle();
	t.mock.timers.tick( 60000 );
	await settle();
	assert.equal( attempts, 1 );
	assert.match( messages[0].error, /Asset HTTP 404/ );
});

/*
================
packedAsset

A valid compressed member lets tests exercise shared pack lifetime through
the real loader. The range case omits its transport to demand a pack header.
================
*/
function packedAsset( ranged = false ) {
	const bytes = Uint8Array.of( 1, 2, 3 ), compressed = gzipSync( bytes );
	const hash = value => createHash( "sha256" ).update( value ).digest( "hex" );
	const digest = hash( compressed ), path = "/assets/test.bin", packPath = "/assets/packs/test.bin";
	const transportPath = `/assets/packs/transport/${digest}.gz`;
	const manifest = {
		version: 1,
		groups: [ {
			name: "test",
			assetCount: 1,
			packs: [ {
				path: packPath,
				bytes: 5 << 20,
				sha256: "0".repeat( 64 ),
				assetCount: 1
			} ]
		} ],
		assets: [ {
			path,
			packPath,
			offset: 0,
			length: bytes.length,
			mime: "application/octet-stream",
			sha256: hash( bytes ),
			...(ranged ? {} : {
				transport: {
					path: transportPath,
					length: compressed.length,
					sha256: digest,
					encoding: "gzip"
				}
			})
		} ]
	};
	return { bytes, compressed, manifest, path, transportPath };
}

for ( const dispose of [ false, true ] ) {
	test( `shared transport backoff respects ${dispose ? "owner disposal" : "subscriber cancellation"}`, {
		timeout: 10000
	}, async t => {
		t.mock.timers.enable( { apis: [ "setTimeout" ] } );
		const asset = packedAsset(), messages = [];
		let deliver = () => {};
		/** @type {Promise<void>} */
		const deliveredResult = new Promise( resolve => {
			deliver = resolve;
		} );
		let attempts = 0;
		/** @type {{ signal?: AbortSignal }} */
		const transport = {};
		t.mock.method( globalThis, "fetch", async ( url, options ) => {
			if ( url.endsWith( "/manifest.json" ) ) return Response.json( asset.manifest );
			assert.ok( url.endsWith( asset.transportPath ) );
			transport.signal = options.signal ?? undefined;
			if ( ++attempts === 1 ) throw new TypeError( "Load failed" );
			return new Response( asset.compressed );
		} );
		const loader = createLoader( message => {
			if ( message.kind !== "progress" ) messages.push( message );
			if ( message.kind === "bytes" || message.kind === "error" ) deliver();
		} );
		t.after( () => loader.dispose() );
		for ( const id of [ 1, 2 ] ) {
			loader.receive( { kind: "load", id, url: `https://example.test${asset.path}`, limit: 3 } );
		}
		await settle();
		assert.equal( attempts, 1 );
		if ( dispose ) loader.dispose();
		else loader.receive( { kind: "cancel", id: 1 } );
		assert.ok( transport.signal );
		assert.equal( transport.signal.aborted, dispose );
		t.mock.timers.tick( 250 );
		// Decompression completes on a worker thread. Counted event-loop turns
		// can finish first under the full test load; wait for the actual result.
		if ( !dispose ) await deliveredResult;
		else await settle();
		if ( dispose ) {
			assert.equal( attempts, 1 );
			assert.deepEqual( messages, [] );
		} else {
			assert.equal( attempts, 2 );
			const delivered = messages.filter( row => row.kind === "bytes" );
			assert.equal( delivered.length, 1 );
			assert.equal( delivered[0].id, 2 );
			assert.deepEqual( new Uint8Array( delivered[0].buffer ), asset.bytes );
			assert.ok( !messages.some( row => row.kind === "error" ) );
		}
	} );
}

test("invalid range stays permanent even when response cleanup rejects", async t => {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	const asset = packedAsset( true ), messages = [];
	let attempts = 0;
	t.mock.method( globalThis, "fetch", async url => {
		if ( url.endsWith( "/manifest.json" ) ) return Response.json( asset.manifest );
		attempts++;
		return new Response( failedBody(), { status: 206, headers: { "Content-Range": "bytes 0-9/10" } } );
	} );
	const loader = createLoader( message => {
		if ( message.kind !== "progress" ) messages.push( message );
	} );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: `https://example.test${asset.path}`, limit: 3 } );
	await settle();
	t.mock.timers.tick( 60000 );
	await settle();
	assert.equal( attempts, 1 );
	assert.equal( messages[0].kind, "error" );
	assert.match( messages[0].error, /Invalid asset range response/ );
});
