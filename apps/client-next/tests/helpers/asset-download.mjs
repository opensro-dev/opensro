/*
===========================================================================

asset-download.mjs - the asset loader's transport test fixtures

Shared by the download recovery and resume suites: a loader over a mocked
fetch and mocked timers, a real SROPACK2 pack served by range, and bodies
that deliver some chunks and then stall.

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );

// The loader's no-progress window (DOWNLOAD_NO_PROGRESS_MS).
export const NO_PROGRESS_MS = 15000;

/*
================
settle
================
*/
export const settle = () => new Promise( resolve => setImmediate( resolve ) );

/*
================
fixture
================
*/
export function fixture( t, fetch, limit = 3 ) {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	t.mock.method( globalThis, "fetch", fetch );
	const messages = [];
	const loader = createLoader( message => {
		if (
			![ "progress", "job-progress", "job-decoding", "job-fetching", "hd-availability" ].includes( message.kind )
		) {
			messages.push( message );
		}
	} );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://example.test/asset", limit } );
	return { loader, messages };
}

/*
================
packedAsset

A real SROPACK2 pack holding one member, padded past the 4 MiB whole-pack
limit so the loader reads the member by range: the pack header first, then
the member's own block. serve() answers both as a host would. The member is
gzip-stored unless compressed is false: a stored-plain member skips
DecompressionStream, so a test about the transport waits on no real
stream work (#1595).
================
*/
export function packedAsset( compressed = true ) {
	const bytes = new Uint8Array( Buffer.alloc( 4096, 7 ) ),
		stored = compressed ? gzipSync( bytes, { level: 9 } ) : Buffer.from( bytes );
	const hash = value => createHash( "sha256" ).update( value ).digest( "hex" );
	const path = "/assets/test.bin", packPath = "/assets/packs/test-001-000000000000.bin";
	const row = {
		path,
		offset: 0,
		length: bytes.length,
		mime: "application/octet-stream",
		sha256: hash( bytes ),
		...(compressed ? { stored: { length: stored.length, encoding: "gzip" } } : {})
	};
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: [ row ] } ) );
	const prefix = Buffer.alloc( 12 );
	prefix.write( "SROPACK2" );
	prefix.writeUInt32LE( header.length, 8 );
	const unpadded = Buffer.concat( [ prefix, header, stored ] );
	const pack = Buffer.concat( [ unpadded, Buffer.alloc( (5 << 20) - unpadded.length ) ] );
	const manifest = {
		version: 2,
		groups: [ {
			name: "test",
			assetCount: 1,
			packs: [ { path: packPath, bytes: pack.length, sha256: hash( pack ), assetCount: 1 } ]
		} ],
		assets: [ { ...row, packPath } ]
	};
	const dataStart = 12 + header.length;
	/*
	================
	serve

	The host's answer to one request: the manifest, or a 206 slice of the pack.
	================
	*/
	function serve( url, options ) {
		if ( url.endsWith( "/manifest.json" ) ) return Response.json( manifest );
		const range = /bytes=(\d+)-(\d+)/.exec( options?.headers?.Range ?? "" );
		assert.ok( range, `pack read without a range: ${url}` );
		const [start, end] = range.slice( 1 ).map( Number );
		return new Response( pack.subarray( start, end + 1 ), {
			status: 206,
			headers: { "Content-Range": `bytes ${start}-${end}/${pack.length}` }
		} );
	}
	// The member's own block, as opposed to the pack header reads before it.
	const isMember = options => options?.headers?.Range?.startsWith( `bytes=${dataStart}-` ) ?? false;
	return { bytes, manifest, path, serve, isMember };
}

/*
================
stallingBody

A response whose body delivers the given chunks, one per read, then stalls.
init is the response's status and headers (a 206 slice, a validator).
================
*/
/**
 * @param {Uint8Array[]} chunks
 * @param {ResponseInit} [init]
 */
export function stallingBody( chunks, init ) {
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
		} ),
		init
	);
}
