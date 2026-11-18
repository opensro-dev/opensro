/*
===========================================================================

refreshAssetFiles.test.mjs - the focused file repack names, places and derives

assets:refresh:files (scripts/refresh_asset_files.mjs) repacks a few
published files without the full repack. These pin what it accepts, where
a new file goes, and that a JSON the packs carry as its gzip twin ships
its new content, not the stale twin.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { gunzipSync } from "node:zlib";
import { newFileGroup, publicFiles, refreshCompressedTwin } from "../../refresh_asset_files.mjs";

/*
================
publicTree

A temporary public root holding the named files. Returns its path.
================
*/
async function publicTree( t, files ) {
	const root = await mkdtemp( path.join( tmpdir(), "refresh-files-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	for ( const [file, bytes] of Object.entries( files ) ) {
		const target = path.join( root, file );
		await mkdir( path.dirname( target ), { recursive: true } );
		await writeFile( target, bytes );
	}
	return root;
}

test("named files resolve to the members the packs hold", async ( t ) => {
	const root = await publicTree( t, {
		"assets/data/teleportData.json": "{}",
		"assets/data/teleportData.json.gz": "x",
		"assets/images/a.png": "png"
	} );
	const members = new Set( [ "/assets/data/teleportdata.json.gz", "/assets/images/a.png" ] );
	// A leading slash is optional; a JSON packed as its twin resolves to it.
	assert.deepEqual( await publicFiles( [ "assets/data/teleportData.json", "/assets/images/a.png" ], members, root ), [
		"/assets/data/teleportData.json.gz",
		"/assets/images/a.png"
	] );
});

test("a name outside the packs' reach is refused", async ( t ) => {
	const root = await publicTree( t, { "assets/packs/manifest.json": "{}" } );
	await assert.rejects( publicFiles( [], new Set(), root ), /at least one/ );
	await assert.rejects( publicFiles( [ "data/x.json" ], new Set(), root ), /\/assets/ );
	await assert.rejects( publicFiles( [ "assets/packs/manifest.json" ], new Set(), root ), /pack tail/ );
	await assert.rejects( publicFiles( [ "assets/data/absent.json" ], new Set(), root ), /not written/ );
});

test("a new image takes its image group; any other new file has no owner here", () => {
	assert.equal( newFileGroup( "/assets/images/Media_extracted/interface/quickparty/qpt_face.png" ), "game-images" );
	assert.equal( newFileGroup( "/assets/data/new.json" ), undefined );
});

test("a JSON's gzip twin is derived again from its minified content", async ( t ) => {
	const root = await publicTree( t, {
		"assets/data/x.json": '{ "rows": [ 1, 2 ] }',
		"assets/data/x.json.gz": "stale"
	} );
	const twin = path.join( root, "assets/data/x.json.gz" );
	assert.equal( await refreshCompressedTwin( twin ), true );
	assert.equal( gunzipSync( await readFile( twin ) ).toString(), '{"rows":[1,2]}' );
	assert.equal( await readFile( path.join( root, "assets/data/x.json" ), "utf8" ), '{"rows":[1,2]}' );
	// A twin already current is left alone.
	assert.equal( await refreshCompressedTwin( twin ), false );
});
