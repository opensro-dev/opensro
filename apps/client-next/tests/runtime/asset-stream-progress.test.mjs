/*
===========================================================================

asset-stream-progress.test.mjs - activity while an HTTP response is unfinished

Decoded stream activity must advance without pretending it is wire usage.
The loader publishes it before the asset becomes available to consumers.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );
// Bounds every wait for a loader message, so a missing one fails the test
// instead of hanging it.
const MESSAGE_TIMEOUT_MS = 10000;

/*
================
settle
================
*/
function settle() {
	return new Promise( resolve => setImmediate( resolve ) );
}

/*
================
createInbox

The loader's messages, and a wait for the first one that matches. The loader
answers through asynchronous steps (WebCrypto digests, the 150 ms progress
beat) whose timing depends on machine load, so a test waits for the message
itself, never for a count of event-loop turns or a fixed delay.
================
*/
function createInbox() {
	const messages = [], waiters = new Set();
	return {
		messages,
		/*
		================
		post
		================
		*/
		post( message ) {
			messages.push( message );
			for ( const waiter of waiters ) {
				if ( waiter.match( message ) ) waiter.resolve( message );
			}
		},
		/*
		================
		until
		================
		*/
		until( match, label ) {
			const found = messages.find( match );
			if ( found ) return Promise.resolve( found );
			return new Promise( ( resolve, reject ) => {
				const waiter = {
					match,
					resolve: message => {
						clearTimeout( timer );
						waiters.delete( waiter );
						resolve( message );
					}
				};
				const timer = setTimeout( () => {
					waiters.delete( waiter );
					reject( Error( `No ${label} within ${MESSAGE_TIMEOUT_MS} ms` ) );
				}, MESSAGE_TIMEOUT_MS );
				waiters.add( waiter );
			} );
		}
	};
}

test("unfinished compressed or cached responses report stream activity separately from wire bytes", async t => {
	let now = 0;
	/** @type {{stream?: ReadableStreamDefaultController<Uint8Array>}} */
	const fixture = {};
	t.mock.method( performance, "now", () => now );
	t.mock.method( performance, "getEntriesByName", () => [] );
	t.mock.method( globalThis, "fetch", async () =>
		new Response(
			new ReadableStream( {
				start( controller ) {
					fixture.stream = controller;
				}
			} )
		) );
	const messages = [], loader = createLoader( message => messages.push( message ) );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://fixture.invalid/stream", limit: 16 } );
	await settle();
	const stream = fixture.stream;
	assert.ok( stream );
	now = 200;
	stream.enqueue( Uint8Array.of( 1, 2, 3 ) );
	await settle();
	const first = messages.find( message => message.kind === "progress" );
	assert.equal( first?.progress.bytesRead, 3 );
	assert.equal( first?.progress.bytesReceived, 0 );
	assert.equal( messages.some( message => message.kind === "bytes" ), false );
	now = 400;
	stream.enqueue( Uint8Array.of( 4, 5 ) );
	await settle();
	assert.equal( messages.filter( message => message.kind === "progress" ).at( -1 )?.progress.bytesRead, 5 );
	stream.close();
	await settle();
	assert.deepEqual( [ ...new Uint8Array( messages.find( message => message.kind === "bytes" ).buffer ) ], [
		1,
		2,
		3,
		4,
		5
	] );
	// The watchdog's notes rode along: cumulative per-job bytes as the stream
	// moved, then the fetch-complete mark before the result.
	assert.deepEqual( messages.filter( message => message.kind === "job-progress" ), [
		{ kind: "job-progress", id: 1, bytes: 3 },
		{ kind: "job-progress", id: 1, bytes: 5 }
	] );
	const decoding = messages.findIndex( message => message.kind === "job-decoding" );
	assert.ok( decoding > -1 && decoding < messages.findIndex( message => message.kind === "bytes" ) );
});

test("a packed /assets load notes byte progress from the packs plumbing", async t => {
	// Production loads are packed: packs.ts fetches the manifest and the
	// member through its own lifetime, never the load's signal. The notes
	// must still arrive, or the watchdog never slides for a real load.
	const bytes = Uint8Array.of( 1, 2, 3 );
	const digest = createHash( "sha256" ).update( bytes ).digest( "hex" );
	const packPath = "/assets/packs/test.bin";
	const manifest = {
		version: 2,
		groups: [ {
			name: "test",
			assetCount: 1,
			packs: [ { path: packPath, bytes: 100, sha256: digest, assetCount: 1 } ]
		} ],
		assets: [ {
			path: "/assets/test.bin",
			packPath,
			offset: 0,
			length: 3,
			mime: "application/octet-stream",
			sha256: digest
		} ]
	};
	t.mock.method( performance, "getEntriesByName", () => [] );
	/** @type {ReadableStreamDefaultController<Uint8Array> | undefined} */
	let stream;
	/** @type {(value?: unknown) => void} */
	let memberStarted = () => {};
	const memberRead = new Promise( resolve => memberStarted = resolve );
	t.mock.method( globalThis, "fetch", async url => {
		if ( url.endsWith( "/manifest.json" ) ) return Response.json( manifest );
		if ( url.endsWith( packPath ) ) return new Response( null, { status: 404 } );
		// A slow member: one chunk arrives, then the body waits. The note
		// must reach the watchdog while the load is still in flight.
		return new Response(
			new ReadableStream( {
				start( controller ) {
					stream = controller;
					memberStarted();
				}
			} )
		);
	} );
	const inbox = createInbox(), loader = createLoader( message => inbox.post( message ) );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://example.test/assets/test.bin", limit: 3 } );
	await memberRead;
	assert.ok( stream, "the member read is in flight" );
	stream.enqueue( bytes );
	// Notes publish on the coalesced 150 ms beat, while the load is in flight.
	const note = await inbox.until(
		message => message.kind === "job-progress" && message.id === 1 && message.bytes >= 3,
		"byte-progress note"
	);
	assert.ok( note, "packed plumbing must note byte progress" );
	assert.equal( inbox.messages.some( message => message.kind === "bytes" ), false, "noted before the answer" );
	stream.close();
	const answer = await inbox.until( message => message.kind === "bytes", "load answer" );
	assert.equal( answer.id, 1 );
});
