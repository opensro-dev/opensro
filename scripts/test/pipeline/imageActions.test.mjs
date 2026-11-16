/*
===========================================================================

imageActions.test.mjs - source images convert by content, not by time

A fake converter stands in for convert_images.py (its --plan and
--convert-list contract). A rerun converts nothing; a source rewritten
with the same bytes and a fresh time converts nothing; a changed source,
a new toolchain or new converter code converts exactly what they touch;
a deleted PNG comes back from the store; a failed batch stores nothing.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, stat, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { openStore } from "../../build/graph/store.mjs";
import { convertSourceImages } from "../../build/images/imageActions.mjs";
import { openFileHashCache } from "../../build/shared/fileHashCache.mjs";

/*
================
fixture

extracted/ holds two sources; the fake converter writes "png:<source
bytes>" for each listed output, as the real one writes a PNG.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-image-actions-test-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const extracted = path.join( root, "extracted" ), images = path.join( root, "images" );
	await mkdir( path.join( extracted, "Media_extracted" ), { recursive: true } );
	await writeFile( path.join( extracted, "Media_extracted", "a.ddj" ), "alpha" );
	await writeFile( path.join( extracted, "Media_extracted", "b.tga" ), "beta" );
	const state = { toolchain: "pillow 1", code: "code-1", converted: [], fail: false };
	const assets = [
		{ source: "Media_extracted/a.ddj", output: "Media_extracted/a.png", kind: "ddj-dds" },
		{ source: "Media_extracted/b.tga", output: "Media_extracted/b.png", kind: "tga" }
	];
	const spawn = async ( args ) => {
		if ( args[0] === "--plan" ) {
			await writeFile(
				args[1],
				JSON.stringify( { format: "sro-image-plan", toolchain: state.toolchain, assets } )
			);
			return { status: 0 };
		}
		const listed = JSON.parse( await readFile( args[1], "utf8" ) );
		for ( const output of listed ) {
			const asset = assets.find( row => row.output === output );
			const target = path.join( images, ...output.split( "/" ) );
			await mkdir( path.dirname( target ), { recursive: true } );
			// The real converter replaces the PNG whole (convert_asset).
			await rm( target, { force: true } );
			await writeFile(
				target,
				`png:${await readFile( path.join( extracted, ...asset.source.split( "/" ) ), "utf8" )}`
			);
			state.converted.push( output );
		}
		return { status: state.fail ? 1 : 0 };
	};
	const run = async () => {
		const store = await openStore( {
			root: path.join( root, "store" ),
			hashCache: await openFileHashCache( path.join( root, "hash-cache.json" ) ),
			codeHashOf: async () => state.code
		} );
		try {
			return await convertSourceImages( spawn, {
				store,
				extracted,
				images,
				script: path.join( root, "convert_images.py" )
			} );
		} finally {
			await store.shutdown();
		}
	};
	const png = name => readFile( path.join( images, "Media_extracted", name ), "utf8" );
	return { extracted, images, state, run, png };
}

test("a rerun and a touched source convert nothing", async ( t ) => {
	const f = await fixture( t );
	const first = await f.run();
	assert.deepEqual( [ first.status, first.converted, first.hits ], [ 0, 2, 0 ] );
	assert.equal( await f.png( "a.png" ), "png:alpha" );
	f.state.converted = [];
	const again = await f.run();
	assert.deepEqual( [ again.converted, again.hits, again.placed ], [ 0, 2, 0 ] );
	// The variant rebuild rewrites its sources with the same bytes, fresh times.
	const later = new Date( Date.now() + 60000 );
	await writeFile( path.join( f.extracted, "Media_extracted", "a.ddj" ), "alpha" );
	await utimes( path.join( f.extracted, "Media_extracted", "a.ddj" ), later, later );
	const touched = await f.run();
	assert.deepEqual( [ touched.converted, f.state.converted ], [ 0, [] ] );
});

test("a changed source, toolchain or converter converts what it touches", async ( t ) => {
	const f = await fixture( t );
	await f.run();
	f.state.converted = [];
	await writeFile( path.join( f.extracted, "Media_extracted", "b.tga" ), "BETA" );
	await f.run();
	assert.deepEqual( f.state.converted, [ "Media_extracted/b.png" ] );
	assert.equal( await f.png( "b.png" ), "png:BETA" );
	f.state.converted = [];
	f.state.toolchain = "pillow 2";
	assert.equal( (await f.run()).converted, 2 );
	f.state.converted = [];
	f.state.code = "code-2";
	assert.equal( (await f.run()).converted, 2 );
});

test("a deleted PNG comes back from the store without a conversion", async ( t ) => {
	const f = await fixture( t );
	await f.run();
	f.state.converted = [];
	await rm( path.join( f.images, "Media_extracted", "a.png" ) );
	const result = await f.run();
	assert.deepEqual( [ result.converted, result.placed, f.state.converted ], [ 0, 1, [] ] );
	assert.equal( await f.png( "a.png" ), "png:alpha" );
});

test("the store and the tree share one copy of each PNG", async ( t ) => {
	const f = await fixture( t );
	await f.run();
	const info = await stat( path.join( f.images, "Media_extracted", "a.png" ) );
	if ( info.nlink < 2 ) return t.skip( "this volume does not hard link" );
	assert.equal( info.nlink, 2 );
});

test("a failed batch stores nothing", async ( t ) => {
	const f = await fixture( t );
	f.state.fail = true;
	assert.equal( (await f.run()).status, 1 );
	f.state.fail = false;
	f.state.converted = [];
	assert.equal( (await f.run()).converted, 2, "nothing from the failed batch was trusted" );
});
