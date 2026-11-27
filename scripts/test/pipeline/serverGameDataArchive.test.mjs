/*
===========================================================================

serverGameDataArchive.test.mjs - the archive round-trips through extract

extractServerGameDataArchive (#4337): the published-id pin reads a
release's served textdata out of its server.srogz. A built archive
extracts byte for byte, a prefix keeps only its files, and an archive
with a corrupt byte is refused before anything is written.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { gunzipSync, gzipSync } from "node:zlib";
import { buildServerGameDataArchive, extractServerGameDataArchive } from "../../build/server/serverGameDataArchive.mjs";
import { sha256Hex } from "../../build/shared/hash.mjs";

/*
================
bundle

A minimal bundle: two textdata tables, one other file, and the manifest
naming each with its digest.
================
*/
async function bundle( root ) {
	const files = {
		"textdata/itemdata.txt": "item rows",
		"textdata/skilldata.txt": "skill rows",
		"navmesh/mapinfo.mfo": "nav"
	};
	for ( const [relative, text] of Object.entries( files ) ) {
		await mkdir( path.dirname( path.join( root, relative ) ), { recursive: true } );
		await writeFile( path.join( root, relative ), text );
	}
	const manifest = {
		format: "sro-game-data-bundle",
		files: Object.entries( files ).map( ( [relative, text] ) => ({
			path: relative,
			size: Buffer.byteLength( text ),
			digest: `sha256:${sha256Hex( Buffer.from( text ) )}`
		}) )
	};
	await writeFile( path.join( root, "manifest.json" ), JSON.stringify( manifest ) );
	return files;
}

test("a built archive extracts byte for byte, and a prefix keeps only its files", async () => {
	const scratch = await mkdtemp( path.join( os.tmpdir(), "srogz-" ) );
	try {
		const files = await bundle( path.join( scratch, "bundle" ) );
		const archive = path.join( scratch, "server.srogz" );
		await buildServerGameDataArchive( path.join( scratch, "bundle" ), archive );

		const out = path.join( scratch, "out" );
		assert.equal( await extractServerGameDataArchive( archive, out, "textdata/" ), 2 );
		assert.deepEqual( (await readdir( out )).sort(), [ "textdata" ] );
		for ( const relative of [ "textdata/itemdata.txt", "textdata/skilldata.txt" ] ) {
			assert.equal( await readFile( path.join( out, relative ), "utf8" ), files[relative] );
		}
		assert.equal( await extractServerGameDataArchive( archive, path.join( scratch, "all" ) ), 4 );
	} finally {
		await rm( scratch, { recursive: true, force: true } );
	}
});

test("a corrupt archive is refused before a file is written", async () => {
	const scratch = await mkdtemp( path.join( os.tmpdir(), "srogz-" ) );
	try {
		await bundle( path.join( scratch, "bundle" ) );
		const archive = path.join( scratch, "server.srogz" );
		await buildServerGameDataArchive( path.join( scratch, "bundle" ), archive );
		const raw = gunzipSync( await readFile( archive ) );
		// The last byte belongs to the last file's contents: its digest no
		// longer matches the manifest.
		raw[raw.length - 1] ^= 0xff;
		await writeFile( archive, gzipSync( raw ) );

		const out = path.join( scratch, "out" );
		await assert.rejects( extractServerGameDataArchive( archive, out ), /digest mismatch/ );
		await assert.rejects( readdir( out ), { code: "ENOENT" } );
	} finally {
		await rm( scratch, { recursive: true, force: true } );
	}
});
