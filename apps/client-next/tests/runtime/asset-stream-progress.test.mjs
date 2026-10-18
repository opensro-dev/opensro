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
/*
================
settle
================
*/
function settle() {
	return new Promise( resolve => setImmediate( resolve ) );
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
	t.mock.method( globalThis, "fetch", async url => {
		if ( url.endsWith( "/manifest.json" ) ) return Response.json( manifest );
		if ( url.endsWith( packPath ) ) return new Response( null, { status: 404 } );
		// A slow member: one chunk arrives, then the body waits. The note
		// must reach the watchdog while the load is still in flight.
		return new Response(
			new ReadableStream( {
				start( controller ) {
					stream = controller;
				}
			} )
		);
	} );
	const messages = [], loader = createLoader( message => messages.push( message ) );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://example.test/assets/test.bin", limit: 3 } );
	await settle();
	await settle();
	await settle();
	assert.ok( stream, "the member read is in flight" );
	stream.enqueue( bytes );
	// Notes publish on the coalesced 150 ms beat; wait past one beat.
	await new Promise( resolve => setTimeout( resolve, 250 ) );
	const notes = messages.filter( message => message.kind === "job-progress" && message.id === 1 );
	assert.ok( notes.length > 0, "packed plumbing must note byte progress" );
	assert.ok( notes.at( -1 ).bytes >= 3 );
	stream.close();
	await settle();
	assert.equal( messages.find( message => message.kind === "bytes" )?.id, 1 );
});
