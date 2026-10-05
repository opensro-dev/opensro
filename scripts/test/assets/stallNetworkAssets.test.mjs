/*
===========================================================================

stallNetworkAssets.test.mjs - the stall network tables' publication

buildStallNetworkAssets copies fmncategorytreedata.txt and
fmntidgroupmapdata.txt byte for byte (they are UTF-16 text) and skips a
missing one.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildStallNetworkAssets } from "../../build/data/buildStallNetworkAssets.mjs";

/*
================
withTemporaryRoots
================
*/
async function withTemporaryRoots( run ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "stall-network-" ) );
	try {
		const sourceRoot = path.join( root, "textdata" );
		const targetRoot = path.join( root, "client-public" );
		await mkdir( sourceRoot, { recursive: true } );
		await run( { sourceRoot, targetRoot } );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
}

test("both tables are published byte for byte; a missing one is skipped", async () => {
	await withTemporaryRoots( async ( roots ) => {
		const tree = Buffer.from( "﻿1\tWK_ETC\tUIIT_CTL_WK_ETC\txxx\t0\t0\r\n", "utf16le" );
		await writeFile( path.join( roots.sourceRoot, "fmncategorytreedata.txt" ), tree );
		const result = await buildStallNetworkAssets( roots );
		assert.deepEqual( result, { written: 1, bytes: tree.length } );
		const published = await readFile(
			path.join( roots.targetRoot, "assets", "textdata", "fmncategorytreedata.txt" )
		);
		assert.deepEqual( published, tree );
		await assert.rejects(
			readFile( path.join( roots.targetRoot, "assets", "textdata", "fmntidgroupmapdata.txt" ) ),
			{ code: "ENOENT" }
		);
	} );
});
