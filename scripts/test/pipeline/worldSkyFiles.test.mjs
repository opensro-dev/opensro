/*
===========================================================================

worldSkyFiles.test.mjs - the sky sweep reads only what it cannot vouch for

A temp world tree stands in for the published one (no game data), and a
hash cache the test controls stands in for the file hash cache: it
vouches for a file's hash until the test says the file changed, the way
the real one does while a file's size and mtime hold. The sweep finds the
files that carry a sky; a rerun skips every file it knows is skyless (a
rewrite the cache still vouches for stays unseen, which proves the file
was not read); a changed file is read; a sky file is always read fresh;
SRO_BUILD_HASH_CACHE=0 forgets every verdict.

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { worldSkyFiles } from "../../build/world/assets/worldSkyFiles.mjs";

const SKYLESS = '{"name":"plain","grid":[1,2,3,4,5]}';
const SKY = '{"name":"ward","sky":{"a":1},"g":9}';

/*
================
vouchingCache

The file hash cache's surface worldSkyFiles uses. A file keeps the hash
it was last read with until changed( file ) drops it.
================
*/
function vouchingCache() {
	const hashes = new Map();
	return {
		peekFileHash: file => hashes.get( file ),
		noteFileBytes( file, _stat, bytes ) {
			const sha256 = createHash( "sha256" ).update( bytes ).digest( "hex" );
			hashes.set( file, sha256 );
			return sha256;
		},
		async save() {},
		changed: file => hashes.delete( file )
	};
}

/*
================
fixture

A temp tree: world/ with two sky files, one skyless region in a
subfolder and a non-JSON file, plus the ledger beside it.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-world-sky-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const world = path.join( root, "world" );
	await mkdir( path.join( world, "outdoor", "regions" ), { recursive: true } );
	await writeFile( path.join( world, "outdoor", "shared.json" ), SKY );
	await writeFile( path.join( world, "town.json" ), SKY );
	await writeFile( path.join( world, "outdoor", "regions", "region-1.json" ), SKYLESS );
	await writeFile( path.join( world, "notes.txt" ), SKY );
	const hashCache = vouchingCache();
	const options = { hashCache, ledgerPath: path.join( root, "ledger.json" ) };
	const sweep = async () =>
		(await worldSkyFiles( world, options )).map( row => path.relative( world, row.file ).replaceAll( "\\", "/" ) )
			.sort();
	return { world, hashCache, sweep, region: path.join( world, "outdoor", "regions", "region-1.json" ) };
}

test("the sweep finds every .json that carries a sky, with its bytes", async t => {
	const { world, hashCache } = await fixture( t );
	const found = await worldSkyFiles( world, { hashCache, ledgerPath: path.join( world, "..", "ledger.json" ) } );
	assert.deepEqual( found.map( row => row.bytes.toString() ), [ SKY, SKY ] );
});

test("a rerun skips a known skyless file and reads a changed one", async t => {
	const { hashCache, sweep, region } = await fixture( t );
	assert.deepEqual( await sweep(), [ "outdoor/shared.json", "town.json" ] );
	await writeFile( region, SKY );
	assert.deepEqual( await sweep(), [ "outdoor/shared.json", "town.json" ], "a vouched skyless file is not read" );
	hashCache.changed( region );
	assert.deepEqual( await sweep(), [ "outdoor/regions/region-1.json", "outdoor/shared.json", "town.json" ] );
});

test("a sky file is read fresh every sweep", async t => {
	const { world, sweep } = await fixture( t );
	await sweep();
	// The cache still vouches for town.json's old hash, whose verdict is
	// "sky": that verdict means "read it", so the current bytes decide.
	await writeFile( path.join( world, "town.json" ), SKYLESS );
	assert.deepEqual( await sweep(), [ "outdoor/shared.json" ] );
});

test("SRO_BUILD_HASH_CACHE=0 forgets every verdict", async t => {
	const { sweep, region } = await fixture( t );
	await sweep();
	await writeFile( region, SKY );
	const previous = process.env.SRO_BUILD_HASH_CACHE;
	process.env.SRO_BUILD_HASH_CACHE = "0";
	t.after( () => {
		if ( previous === undefined ) delete process.env.SRO_BUILD_HASH_CACHE;
		else process.env.SRO_BUILD_HASH_CACHE = previous;
	} );
	assert.deepEqual( await sweep(), [ "outdoor/regions/region-1.json", "outdoor/shared.json", "town.json" ] );
});
