/*
===========================================================================

publicWrite.test.mjs - public tree writes leave identical outputs untouched

A warm build republishes thousands of images, models and manifests whose
bytes did not change. Each writer must keep such a file's mtime (the stat
fingerprints and sidecar freshness checks read it) and must still write a
file whose bytes differ, including one of the same size.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, stat, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { publishBytesAtomically } from "../../build/shared/atomicPublish.mjs";
import { openFileHashCache } from "../../build/shared/fileHashCache.mjs";
import { copyIntoPublicTree, writeIntoPublicTree, writeIntoPublicTreeSync } from "../../build/shared/publicWrite.mjs";

const OLD_SECONDS = 1_000_000_000;

/*
================
withTarget

A temp directory holding target.bin = "abc", backdated so a rewrite shows.
================
*/
async function withTarget( run ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-public-write-" ) );
	const target = path.join( root, "target.bin" );
	await writeFile( target, "abc" );
	await utimes( target, OLD_SECONDS, OLD_SECONDS );
	try {
		await run( root, target );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
}

/*
================
mtimeSeconds
================
*/
async function mtimeSeconds( file ) {
	return Math.round( (await stat( file )).mtimeMs / 1000 );
}

const WRITERS = {
	writeIntoPublicTree: ( target, bytes ) => writeIntoPublicTree( target, bytes ),
	writeIntoPublicTreeSync: async ( target, bytes ) => writeIntoPublicTreeSync( target, bytes ),
	publishBytesAtomically: ( target, bytes ) => publishBytesAtomically( target, bytes ),
	copyIntoPublicTree: async ( target, bytes ) => {
		const source = `${target}.source`;
		await writeFile( source, bytes );
		const hashCache = await openFileHashCache( path.join( path.dirname( target ), "hash-cache.json" ) );
		await copyIntoPublicTree( source, target, { hashCache } );
	}
};

for ( const [name, write] of Object.entries( WRITERS ) ) {
	test(`${name} keeps an identical target and rewrites a changed one`, async () => {
		await withTarget( async ( root, target ) => {
			await write( target, Buffer.from( "abc" ) );
			assert.equal( await mtimeSeconds( target ), OLD_SECONDS, "identical bytes were rewritten" );

			// Same size, different bytes: the size check alone must not pass it.
			await write( target, new Uint8Array( Buffer.from( "abd" ) ) );
			assert.equal( await readFile( target, "utf8" ), "abd" );
			assert.notEqual( await mtimeSeconds( target ), OLD_SECONDS );

			const created = path.join( root, "created.bin" );
			await write( created, Buffer.from( "new" ) );
			assert.equal( await readFile( created, "utf8" ), "new" );
		} );
	});
}

test("publishBytesAtomically still forces a write on request", async () => {
	await withTarget( async ( root, target ) => {
		assert.equal( await publishBytesAtomically( target, Buffer.from( "abc" ) ), false );
		assert.equal( await publishBytesAtomically( target, Buffer.from( "abc" ), { skipIfUnchanged: false } ), true );
		assert.notEqual( await mtimeSeconds( target ), OLD_SECONDS );
	} );
});

/*
================
withCopyPair

A temp source and target holding "abc", both backdated, and a hash cache
of their own.
================
*/
async function withCopyPair( run ) {
	await withTarget( async ( root, target ) => {
		const source = path.join( root, "source.bin" );
		await writeFile( source, "abc" );
		await utimes( source, OLD_SECONDS, OLD_SECONDS );
		const hashCache = await openFileHashCache( path.join( root, "hash-cache.json" ) );
		await run( { root, source, target, hashCache } );
	} );
}

test("copyIntoPublicTree remembers an identical pair and then answers it by stat", async () => {
	await withCopyPair( async ( { source, target, hashCache } ) => {
		await copyIntoPublicTree( source, target, { hashCache } );
		const sourceStat = await stat( source ), targetStat = await stat( target );
		assert.ok( hashCache.peekFileHash( source, sourceStat ), "the compared source was not remembered" );
		assert.equal( hashCache.peekFileHash( target, targetStat ), hashCache.peekFileHash( source, sourceStat ) );
		// Change the target's bytes behind the cache's back, keeping its size and
		// time: a remembered pair is trusted without reading either file.
		await writeFile( target, "xyz" );
		await utimes( target, OLD_SECONDS, OLD_SECONDS );
		await copyIntoPublicTree( source, target, { hashCache } );
		assert.equal( await readFile( target, "utf8" ), "xyz" );
	} );
});

test("copyIntoPublicTree copies when the source changed since the pair was remembered", async () => {
	await withCopyPair( async ( { source, target, hashCache } ) => {
		await copyIntoPublicTree( source, target, { hashCache } );
		await writeFile( source, "abd" );
		await copyIntoPublicTree( source, target, { hashCache } );
		assert.equal( await readFile( target, "utf8" ), "abd" );
	} );
});

test("copyIntoPublicTree still refuses a missing source", async () => {
	await withCopyPair( async ( { root, target, hashCache } ) => {
		await assert.rejects( copyIntoPublicTree( path.join( root, "absent.bin" ), target, { hashCache } ), {
			code: "ENOENT"
		} );
	} );
});
