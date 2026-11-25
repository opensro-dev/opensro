/*
===========================================================================

network-wt-compress.test.mjs - compressed WebTransport records

With SRO_WEBTRANSPORT_COMPRESSION on, the server may gzip a large record for
a client that asked (conn_wt_compress.go). The client inflates it in its
place in the stream, bounded by the frame limit, and asks only where the
browser can inflate.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
import { gzipSync } from "node:zlib";
import { flush, record, settle, socketHarness, welcome, wtHarness } from "../helpers/transport-harness.mjs";
const { createNetwork } = await import(
	"../../src/engine/runtime/simulation/worker/network/network.ts"
);

/*
================
compressedRecord

One stream record carrying a gzip-compressed frame: the length prefix's top
bit set over the gzip stream's size (server conn_wt_compress.go).
================
*/
function compressedRecord( frame ) {
	const out = record( gzipSync( frame ) );
	out[3] |= 0x80;
	return out;
}
/*
================
wtLink

A network connected and welcomed over WebTransport.
================
*/
async function wtLink( t ) {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	socketHarness( t );
	const transports = wtHarness( t ), failures = [];
	const network = createNetwork( e => failures.push( e ) );
	network.webTransport( true );
	network.connect( "wss://localhost/transport/ws", "abc" );
	await flush();
	transports[0].readyResolve();
	await flush();
	transports[0].push( record( welcome() ) );
	await flush();
	t.after( () => network.dispose() );
	return { network, transport: transports[0], failures };
}

/*
================
inflated

Waits for a decompression to show: zlib answers from its own thread, so a
fixed number of turns cannot bound it, and the harness freezes setTimeout.
================
*/
async function inflated( done ) {
	const deadline = performance.now() + 20000;
	while ( !done() && performance.now() < deadline ) await settle();
}

test("a compressed stream record is inflated in its place among the raw ones", async t => {
	const { network, transport, failures } = await wtLink( t ), frames = [];
	const large = new Uint8Array( 200000 ).map( ( _, i ) => i % 7 );
	large.set( [ 0x0e, 0 ] );
	const stream = [
		record( Uint8Array.of( 0, 1, 11 ) ),
		compressedRecord( large ),
		record( Uint8Array.of( 1, 1, 12 ) ),
		compressedRecord( Uint8Array.of( 2, 1, 13 ) )
	];
	const bytes = new Uint8Array( stream.reduce( ( total, part ) => total + part.byteLength, 0 ) );
	let at = 0;
	for ( const part of stream ) {
		bytes.set( part, at );
		at += part.byteLength;
	}
	const received = network.receivedBytes();
	// Cut inside the compressed record's prefix and inside its body.
	transport.push( bytes.slice( 0, 9 ) );
	transport.push( bytes.slice( 9, 60 ) );
	transport.push( bytes.slice( 60 ) );
	await inflated( () => {
		network.drain( frame => {
			frames.push( frame );
		} );
		return frames.length >= 5;
	} );
	assert.deepEqual( frames.map( frame => frame.opcode ), [ 2, 256, 14, 257, 258 ] );
	assert.deepEqual( frames[2].payload, large.subarray( 2 ) );
	assert.deepEqual( [ ...frames[4].payload ], [ 13 ] );
	assert.equal( network.receivedBytes() - received, bytes.byteLength, "liveness counts the bytes on the wire" );
	assert.deepEqual( failures, [] );
});

test("a compressed record that does not inflate, or inflates past a frame, ends the session", async t => {
	for (
		const [name, bytes] of /** @type {[string, Uint8Array][]} */ ([
			[ "corrupt", Uint8Array.of( 4, 0, 0, 0x80, 1, 2, 3, 4 ) ],
			[ "oversized", compressedRecord( new Uint8Array( (16 << 20) + 1 ) ) ],
			[ "length past the limit", Uint8Array.of( 1, 0, 0, 0x81, 0 ) ]
		])
	) {
		await t.test( name, async t => {
			const { transport, failures } = await wtLink( t );
			transport.push( bytes );
			await inflated( () => failures.length > 0 );
			assert.equal( failures.length, 1 );
			assert.match( failures[0], /Invalid inbound frame length/ );
		} );
	}
});

test("a browser that cannot inflate does not ask for compressed records", async t => {
	const original = Object.getOwnPropertyDescriptor( globalThis, "DecompressionStream" );
	assert.ok( original );
	Reflect.deleteProperty( globalThis, "DecompressionStream" );
	t.after( () => Object.defineProperty( globalThis, "DecompressionStream", original ) );
	const { transport } = await wtLink( t );
	assert.equal( transport.url, "https://localhost/transport/wt" );
});
