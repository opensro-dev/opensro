/*
===========================================================================

asset-download-recovery.test.mjs - bounded recovery at the asset transport

Exercise the shipping worker with failed fetches and interrupted bodies.
Mock time keeps backoff deterministic; cancellation and integrity failures
must retain their original meaning. A download that receives nothing for
15 s, waiting for headers or mid-body, is abandoned and retried as a
transport failure; any received byte restarts that window.

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
		// Each attempt fetches under its own signal (the no-progress window), and
		// the first attempt has already ended: the owner's signal is what stops
		// the backoff, which the attempt count below proves.
		assert.ok( transport.signal );
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

const NO_PROGRESS_MS = 15000;

/*
================
hangingFetch

A fetch that never answers until its signal aborts, as a stalled connection
does, and records how each attempt ended.
================
*/
function hangingFetch( ended ) {
	return ( url, options ) =>
		new Promise( ( resolve, reject ) => {
			options.signal.addEventListener( "abort", () => {
				ended.push( "aborted" );
				reject( options.signal.reason );
			}, { once: true } );
		} );
}

/*
================
stallingBody

A response whose body delivers the given chunks, one per read, then stalls.
================
*/
function stallingBody( chunks ) {
	let next = 0;
	return new Response(
		new ReadableStream( {
			/*
			================
			pull
			================
			*/
			pull( controller ) {
				if ( next < chunks.length ) controller.enqueue( chunks[next++] );
				else return new Promise( () => {} );
			}
		} )
	);
}

test("a request with no headers for the no-progress window is retried, then answered as transient", async t => {
	const ended = [];
	let attempts = 0;
	const { messages } = fixture( t, ( url, options ) => {
		attempts++;
		return hangingFetch( ended )( url, options );
	} );
	await settle();
	t.mock.timers.tick( NO_PROGRESS_MS - 1 );
	await settle();
	assert.equal( attempts, 1, "no abort before the window ends" );
	t.mock.timers.tick( 1 );
	await settle();
	assert.deepEqual( ended, [ "aborted" ] );
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2 );
	t.mock.timers.tick( NO_PROGRESS_MS );
	await settle();
	t.mock.timers.tick( 1000 );
	await settle();
	assert.equal( attempts, 3 );
	t.mock.timers.tick( NO_PROGRESS_MS );
	await settle();
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "error" );
	assert.match( messages[0].error, /No download progress for 15000 ms/ );
	assert.equal( messages[0].transient, true );
});

test("a body that stalls mid-read is abandoned and retried without publishing partial bytes", async t => {
	let attempts = 0;
	const { messages } = fixture(
		t,
		async () => ++attempts === 1 ? stallingBody( [ Uint8Array.of( 9 ) ] ) : new Response( Uint8Array.of( 4, 5, 6 ) )
	);
	await settle();
	t.mock.timers.tick( NO_PROGRESS_MS );
	await settle();
	assert.equal( messages.length, 0 );
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2 );
	assert.equal( messages.length, 1 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 4, 5, 6 ] );
});

test("every received chunk restarts the window; an empty chunk does not", async t => {
	const pulls = [];
	let attempts = 0;
	const { messages } = fixture( t, async () => {
		if ( ++attempts > 1 ) return new Response( Uint8Array.of( 4, 5, 6 ) );
		return new Response(
			new ReadableStream( {
				/*
				================
				pull
				================
				*/
				pull( controller ) {
					return new Promise( resolve => {
						pulls.push( chunk => {
							if ( chunk === null ) controller.close();
							else controller.enqueue( chunk );
							resolve();
						} );
					} );
				}
			} )
		);
	} );
	await settle();
	// Real bytes every 10 s keep a slow download alive past the window.
	for ( let i = 0; i < 2; i++ ) {
		t.mock.timers.tick( 10000 );
		pulls.shift()( Uint8Array.of( i ) );
		await settle();
	}
	assert.equal( attempts, 1, "a progressing download is not abandoned" );
	// An empty chunk is not progress: 15 s after the last byte, it stalls.
	t.mock.timers.tick( 10000 );
	pulls.shift()( new Uint8Array( 0 ) );
	await settle();
	t.mock.timers.tick( 5000 );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2, "the empty chunk did not restart the window" );
	assert.equal( messages.length, 1 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 4, 5, 6 ], "no partial first-attempt bytes" );
});

test("the caller's cancellation during a stalled request stays a cancellation", async t => {
	const ended = [];
	const { loader, messages } = fixture( t, hangingFetch( ended ) );
	await settle();
	t.mock.timers.tick( NO_PROGRESS_MS - 1 );
	loader.receive( { kind: "cancel", id: 1 } );
	await settle();
	assert.deepEqual( messages, [ { kind: "released", id: 1 } ] );
	t.mock.timers.tick( 60000 );
	await settle();
	assert.deepEqual( ended, [ "aborted" ], "one attempt, ended by the caller, never retried" );
});

test("a stalled body whose cancel never settles still frees the attempt for its retry", async t => {
	let attempts = 0;
	const { messages } = fixture( t, async () => {
		if ( ++attempts > 1 ) return new Response( Uint8Array.of( 4, 5, 6 ) );
		return new Response(
			new ReadableStream( {
				/*
				================
				pull
				================
				*/
				pull() {
					return new Promise( () => {} );
				},
				/*
				================
				cancel
				================
				*/
				cancel() {
					return new Promise( () => {} );
				}
			} )
		);
	} );
	await settle();
	t.mock.timers.tick( NO_PROGRESS_MS );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2, "the hanging cleanup did not hold the request" );
	assert.equal( messages.length, 1 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 4, 5, 6 ] );
});

test("a cancellation racing the stall timer stays a cancellation", async t => {
	const ended = [];
	const { loader, messages } = fixture( t, hangingFetch( ended ) );
	await settle();
	// The stall abort and the caller's cancel land in the same turn.
	t.mock.timers.tick( NO_PROGRESS_MS );
	loader.receive( { kind: "cancel", id: 1 } );
	await settle();
	t.mock.timers.tick( 60000 );
	await settle();
	assert.deepEqual( messages, [ { kind: "released", id: 1 } ] );
	assert.deepEqual( ended, [ "aborted" ], "never retried after the cancel" );
});

/*
================
hangingCancel

A readable body whose cancel never settles, for cleanup that must not
hold the request.
================
*/
function hangingCancel( bytes ) {
	return new ReadableStream( {
		/*
		================
		start
		================
		*/
		start( controller ) {
			controller.enqueue( bytes );
		},
		/*
		================
		pull
		================
		*/
		pull() {
			return new Promise( () => {} );
		},
		/*
		================
		cancel
		================
		*/
		cancel() {
			return new Promise( () => {} );
		}
	} );
}

test("an HTTP failure whose body cancel never settles still retries", async t => {
	let attempts = 0;
	const { messages } = fixture( t, async () =>
		++attempts === 1 ?
			new Response( hangingCancel( Uint8Array.of( 1 ) ), { status: 503 } ) :
			new Response( Uint8Array.of( 4, 5, 6 ) ) );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 4, 5, 6 ] );
});

test("a byte-limit failure whose body cancel never settles still answers", async t => {
	const { messages } = fixture( t, async () => new Response( hangingCancel( Uint8Array.of( 1, 2, 3, 4 ) ) ) );
	await settle();
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "error" );
	assert.match( messages[0].error, /exceeds byte limit/ );
	assert.equal( messages[0].transient, undefined, "a budget failure stays permanent" );
});

test(
	"a stalled shared transport is abandoned once and serves the subscriber that stayed",
	{ timeout: 10000 },
	async t => {
		t.mock.timers.enable( { apis: [ "setTimeout" ] } );
		const asset = packedAsset(), messages = [], transports = [];
		let deliver = () => {};
		/** @type {Promise<void>} */
		const delivered = new Promise( resolve => {
			deliver = resolve;
		} );
		t.mock.method( globalThis, "fetch", ( url, options ) => {
			if ( url.endsWith( "/manifest.json" ) ) return Promise.resolve( Response.json( asset.manifest ) );
			transports.push( options.signal );
			// The first transport request never answers; the retry does.
			if ( transports.length === 1 ) {
				return new Promise( ( resolve, reject ) => {
					options.signal.addEventListener( "abort", () => reject( options.signal.reason ), { once: true } );
				} );
			}
			return Promise.resolve( new Response( asset.compressed ) );
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
		assert.equal( transports.length, 1, "both subscribers share one transport request" );
		loader.receive( { kind: "cancel", id: 1 } );
		await settle();
		assert.equal( transports[0].aborted, false, "one subscriber leaving does not end the shared request" );
		t.mock.timers.tick( NO_PROGRESS_MS );
		await settle();
		assert.equal( transports[0].aborted, true, "the stall abandons the shared request" );
		t.mock.timers.tick( 250 );
		await delivered;
		assert.equal( transports.length, 2, "exactly one retry for both subscribers" );
		const bytes = messages.filter( row => row.kind === "bytes" );
		assert.equal( bytes.length, 1 );
		assert.equal( bytes[0].id, 2 );
		assert.deepEqual( new Uint8Array( bytes[0].buffer ), asset.bytes );
		assert.ok( !messages.some( row => row.kind === "error" ) );
	}
);

test("headers just inside the window restart it: the stalled body is abandoned a full window later", async t => {
	let attempts = 0, answer;
	const { messages } = fixture( t, ( url, options ) => {
		if ( ++attempts > 1 ) return Promise.resolve( new Response( Uint8Array.of( 4, 5, 6 ) ) );
		return new Promise( ( resolve, reject ) => {
			answer = () => resolve( stallingBody( [] ) );
			options.signal.addEventListener( "abort", () => reject( options.signal.reason ), { once: true } );
		} );
	} );
	await settle();
	t.mock.timers.tick( NO_PROGRESS_MS - 100 );
	answer();
	await settle();
	// 15 s after the start, but only 100 ms after the headers: still waiting.
	t.mock.timers.tick( 100 );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 1, "the headers restarted the window" );
	// A full window after the headers, the stalled body is abandoned.
	t.mock.timers.tick( NO_PROGRESS_MS - 350 );
	await settle();
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( attempts, 2 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ 4, 5, 6 ] );
});
