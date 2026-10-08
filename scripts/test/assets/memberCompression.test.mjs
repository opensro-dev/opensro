/*
===========================================================================

memberCompression.test.mjs - one verdict per member, cached and verified

The same member can sit in several packs that build at once; it must be
compressed once and written to the cache once. A cache hit is decoded and
checked before reuse, and a member gzip cannot shrink by a tenth stays raw.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { gunzipSync } from "node:zlib";
import { openMemberCompression } from "../../build/shared/memberCompression.mjs";

/*
================
sha
================
*/
function sha( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
cacheRoot
================
*/
async function cacheRoot( t ) {
	const root = await mkdtemp( path.join( tmpdir(), "sro-member-cache-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	return root;
}

/*
================
cacheFiles

Every file under the cache root, temporaries included.
================
*/
async function cacheFiles( root ) {
	const entries = await readdir( root, { recursive: true, withFileTypes: true } );
	return entries.filter( entry => entry.isFile() ).map( entry => entry.name );
}

test("the same member stored by many packs at once is compressed and cached once", async t => {
	const root = await cacheRoot( t ), compression = openMemberCompression( { cacheRoot: root } );
	const bytes = Buffer.alloc( 256 << 10, 7 ), digest = sha( bytes );
	const results = await Promise.all( Array.from( { length: 16 }, () => compression.store( bytes, digest ) ) );
	assert.deepEqual( compression.stats(), { hits: 0, misses: 1 } );
	for ( const result of results ) {
		assert.equal( result.encoding, "gzip" );
		assert.equal( sha( gunzipSync( result.stored ) ), digest );
	}
	assert.deepEqual( await cacheFiles( root ), [ digest + ".gz" ] );
});

test("a cache hit is verified, and a damaged entry is rebuilt instead of served", async t => {
	const root = await cacheRoot( t ), bytes = Buffer.alloc( 64 << 10, 3 ), digest = sha( bytes );
	await openMemberCompression( { cacheRoot: root } ).store( bytes, digest );
	const again = openMemberCompression( { cacheRoot: root } );
	await again.store( bytes, digest );
	assert.deepEqual( again.stats(), { hits: 1, misses: 0 } );
	const entries = await readdir( root, { recursive: true, withFileTypes: true } );
	const entry = entries.find( e => e.isFile() );
	await writeFile( path.join( entry.parentPath, entry.name ), Buffer.from( "not gzip" ) );
	const damaged = openMemberCompression( { cacheRoot: root } );
	const result = await damaged.store( bytes, digest );
	assert.deepEqual( damaged.stats(), { hits: 0, misses: 1 } );
	assert.equal( sha( gunzipSync( result.stored ) ), digest );
});

test("a member gzip cannot shrink by a tenth is stored raw", async t => {
	const root = await cacheRoot( t ), compression = openMemberCompression( { cacheRoot: root } );
	const bytes = Buffer.concat(
		Array.from( { length: 512 }, ( _, i ) => createHash( "sha256" ).update( String( i ) ).digest() )
	);
	const result = await compression.store( bytes, sha( bytes ) );
	assert.equal( result.encoding, null );
	assert.equal( result.stored, bytes );
});
