/*
===========================================================================

fingerprintSourceRoots.test.mjs - a source root keys on content, a data root on stat

Temp roots stand in for the checkout and the generated tree (no game
data), with a hash cache of the test's own. A source root (the checkout's
files) keeps its hash when only mtimes move, as in a fresh worktree, and
moves on a byte change; a data root still moves on a touch; a root with
parts counts only those entries, so a cache beside them can change freely.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { openFileHashCache } from "../../build/shared/fileHashCache.mjs";
import { hashRoots } from "../../build/shared/resourceBuildFingerprint.mjs";

/*
================
fixture

A temp tree with art/ (two files) and intermediate/ (images/, a manifest
and a cache folder), plus a hash cache file.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-fingerprint-roots-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const art = path.join( root, "art" ), intermediate = path.join( root, "intermediate" );
	await mkdir( path.join( intermediate, "images" ), { recursive: true } );
	await mkdir( path.join( intermediate, "texture-variants" ), { recursive: true } );
	await mkdir( art, { recursive: true } );
	await writeFile( path.join( art, "a.png" ), "alpha" );
	await writeFile( path.join( art, "b.png" ), "beta" );
	await writeFile( path.join( intermediate, "images", "x.png" ), "x" );
	await writeFile( path.join( intermediate, "image-manifest.csv" ), "x.png\n" );
	await writeFile( path.join( intermediate, "texture-variants", "v.ktx" ), "v1" );
	const hashes = await openFileHashCache( path.join( root, "hash-cache.json" ) );
	const hash = roots => hashRoots( roots, hashes );
	return { art, intermediate, hash };
}

/*
================
touch

Moves a file's mtime without changing its bytes, as a fresh checkout does.
================
*/
async function touch( file, seconds ) {
	const at = new Date( Date.now() + seconds * 1000 );
	await utimes( file, at, at );
}

test("a source root keeps its hash when only mtimes move, and moves on a byte change", async t => {
	const { art, hash } = await fixture( t );
	const roots = [ { label: "survival-art", absolutePath: art, source: true } ];
	const before = await hash( roots );
	await touch( path.join( art, "a.png" ), 120 );
	await touch( path.join( art, "b.png" ), 240 );
	assert.equal( await hash( roots ), before, "a checkout's mtimes are not content" );
	await writeFile( path.join( art, "a.png" ), "alphA" );
	await touch( path.join( art, "a.png" ), 360 );
	assert.notEqual( await hash( roots ), before, "a byte change moves the key" );
});

test("a data root still moves on a touch", async t => {
	const { art, hash } = await fixture( t );
	const roots = [ { label: "extracted", absolutePath: art } ];
	const before = await hash( roots );
	await touch( path.join( art, "a.png" ), 120 );
	assert.notEqual( await hash( roots ), before );
});

test("a root with parts counts only those entries", async t => {
	const { intermediate, hash } = await fixture( t );
	const roots = [ {
		label: "rebuild-assets",
		absolutePath: intermediate,
		parts: [ "images", "image-manifest.csv", "image-discovery.json" ]
	} ];
	const before = await hash( roots );
	await writeFile( path.join( intermediate, "texture-variants", "v.ktx" ), "v2 refilled" );
	assert.equal( await hash( roots ), before, "a cache beside the parts is not an input" );
	await writeFile( path.join( intermediate, "image-discovery.json" ), "{}" );
	assert.notEqual( await hash( roots ), before, "an absent part appearing moves the key" );
	const withDiscovery = await hash( roots );
	await writeFile( path.join( intermediate, "images", "x.png" ), "x2" );
	await touch( path.join( intermediate, "images", "x.png" ), 120 );
	assert.notEqual( await hash( roots ), withDiscovery, "a converted image moving moves the key" );
});
