/*
===========================================================================

nameFilterAsset.test.mjs - the full build's name-filter publication

buildNameFilterAsset is the only producer of /assets/textdata/abusefilter.txt.
These tests pin that it copies the retail bytes unchanged (the list is CP949,
so any re-encoding would corrupt it) and that a missing source skips cleanly.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildNameFilterAsset } from "../../build/data/buildNameFilterAsset.mjs";

/*
================
withTemporaryRoots
================
*/
async function withTemporaryRoots( run ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "name-filter-" ) );
	try {
		const sourceRoot = path.join( root, "textdata" );
		const targetRoot = path.join( root, "client-public" );
		await mkdir( sourceRoot, { recursive: true } );
		await run( { sourceRoot, targetRoot } );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
}

test("name filter is published byte for byte, including CP949 text", async () => {
	await withTemporaryRoots( async ( roots ) => {
		// "욕설" in CP949 followed by a CRLF row break, as in the retail list.
		const retail = Buffer.from( [ 0xbf, 0xe5, 0xbc, 0xb3, 0x0d, 0x0a, 0x61, 0x62, 0x63, 0x0d, 0x0a ] );
		await writeFile( path.join( roots.sourceRoot, "abusefilter.txt" ), retail );

		const result = await buildNameFilterAsset( roots );

		assert.deepEqual( result, { written: true, bytes: retail.length } );
		const published = await readFile( path.join( roots.targetRoot, "assets", "textdata", "abusefilter.txt" ) );
		assert.deepEqual( published, retail );
	} );
});

test("a missing retail name filter skips without publishing", async () => {
	await withTemporaryRoots( async ( roots ) => {
		const result = await buildNameFilterAsset( roots );

		assert.deepEqual( result, { written: false, bytes: 0 } );
		await assert.rejects( readFile( path.join( roots.targetRoot, "assets", "textdata", "abusefilter.txt" ) ), {
			code: "ENOENT"
		} );
	} );
});
