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
});
