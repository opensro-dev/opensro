import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import * as zlib from "node:zlib";
import { mkdtemp, mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import test from "node:test";

import {
	ASSET_PACK_MAGIC,
	buildAssetPacks,
	compressAssetPackZstd,
	listPublicAssetFiles
} from "../../build/assetPacks.mjs";
import { minifyJsonBytes, optimizeJsonAssets } from "../../build/jsonAssetCompression.mjs";
import { buildUiImagePreloadManifest } from "../../build/uiImagePreload.mjs";

const gunzipAsync = promisify( zlib.gunzip );

test("minifyJsonBytes removes only whitespace outside JSON strings", () => {
	const source = Buffer.from(
		`{
      "message": " keep spaces and \\"quotes\\" inside ",
      "items": [
        { "id": 1, "name": "one" },
        { "id": 2, "name": "two" }
      ]
    }`,
		"utf8"
	);

	const minified = minifyJsonBytes( source ).toString( "utf8" );

	assert.equal(
		minified,
		`{"message":" keep spaces and \\"quotes\\" inside ","items":[{"id":1,"name":"one"},{"id":2,"name":"two"}]}`
	);
	assert.deepEqual( JSON.parse( minified ), JSON.parse( source.toString( "utf8" ) ) );
});

test("optimizeJsonAssets writes minified JSON and reversible precompressed sidecars", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-json-compression-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const assetRoot = path.join( tempRoot, "assets" );
	await mkdir( path.join( assetRoot, "world" ), { recursive: true } );
	const jsonPath = path.join( assetRoot, "world", "region-test.json" );
	await writeFile(
		jsonPath,
		`${
			JSON.stringify(
				{
					format: "fixture",
					records: [
						{ id: 1, path: "/assets/world/example/texture-a.png" },
						{ id: 2, path: "/assets/world/example/texture-a.png" }
					]
				},
				null,
				2
			)
		}\n`,
		"utf8"
	);

	const summary = await optimizeJsonAssets( {
		root: assetRoot,
		publicRoot: assetRoot,
		encodings: [ "gzip" ],
		compressMinBytes: 0,
		force: true
	} );

	const minified = await readFile( jsonPath );
	const gzip = await gunzipAsync( await readFile( `${jsonPath}.gz` ) );

	assert.equal( minified.toString( "utf8" ), JSON.stringify( JSON.parse( minified.toString( "utf8" ) ) ) );
	assert.deepEqual( JSON.parse( gzip.toString( "utf8" ) ), JSON.parse( minified.toString( "utf8" ) ) );
	assert.equal( summary.jsonFiles, 1 );
	assert.equal( summary.byEncoding.gzip.files, 1 );
	assert.equal( summary.files[0].path, "world/region-test.json" );
	await assert.rejects(
		stat( `${jsonPath}.br` ),
		( error ) => error instanceof Error && "code" in error && error.code === "ENOENT"
	);
});

test("optimizeJsonAssets refuses a sidecar encoding nothing publishes", async ( t ) => {
	const assetRoot = await mkdtemp( path.join( os.tmpdir(), "sro-json-encoding-" ) );
	t.after( () => rm( assetRoot, { recursive: true, force: true } ) );
	await assert.rejects(
		optimizeJsonAssets( { root: assetRoot, publicRoot: assetRoot, encodings: [ "br" ] } ),
		/No published sidecar encoding br/
	);
});

test("optimizeJsonAssets can exclude generated pack-owned JSON manifests", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-json-compression-exclude-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const assetRoot = path.join( tempRoot, "assets" );
	const vatPath = path.join( assetRoot, "char", "vat", "europe", "example.vat.json" );
	const normalPath = path.join( assetRoot, "world", "region-test.json" );
	await mkdir( path.dirname( vatPath ), { recursive: true } );
	await mkdir( path.dirname( normalPath ), { recursive: true } );
	await writeFile( vatPath, '{ "format": "sro-avatar-vat" }\n', "utf8" );
	await writeFile( normalPath, '{ "format": "region" }\n', "utf8" );

	const summary = await optimizeJsonAssets( {
		root: assetRoot,
		publicRoot: assetRoot,
		encodings: [ "gzip" ],
		compressMinBytes: 0,
		force: true,
		exclude: [ /[/\\]char[/\\]vat[/\\].*\.vat\.json$/i ]
	} );

	assert.equal( summary.jsonFiles, 1 );
	assert.equal( summary.files[0].path, "world/region-test.json" );
	assert.equal( await readFile( vatPath, "utf8" ), '{ "format": "sro-avatar-vat" }\n' );
});

test("buildUiImagePreloadManifest indexes native interface images and interactive state siblings", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-ui-preload-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const imageRoot = path.join( tempRoot, "assets", "images", "Media_extracted", "interface", "outer" );
	await mkdir( imageRoot, { recursive: true } );
	await writeFile( path.join( imageRoot, "zoomout.png" ), "normal" );
	await writeFile( path.join( imageRoot, "zoomout_focus.png" ), "focus" );
	await writeFile( path.join( imageRoot, "zoomout_press.png" ), "press" );
	await writeFile( path.join( imageRoot, "server_window.png" ), "server-window" );
	await writeFile( path.join( imageRoot, "server_select.png" ), "server-select" );

	const targetPath = path.join( tempRoot, "assets", "ui", "preload-images.json" );
	const result = await buildUiImagePreloadManifest( { imageRoot, targetPath } );
	const manifest = JSON.parse( await readFile( targetPath, "utf8" ) );

	assert.equal( result.imageCount, 5 );
	assert.deepEqual(
		manifest.images.map( ( image ) => image.path ),
		[
			"/assets/images/Media_extracted/interface/outer/server_select.png",
			"/assets/images/Media_extracted/interface/outer/server_window.png",
			"/assets/images/Media_extracted/interface/outer/zoomout.png",
			"/assets/images/Media_extracted/interface/outer/zoomout_focus.png",
			"/assets/images/Media_extracted/interface/outer/zoomout_press.png"
		]
	);
	assert.equal(
		manifest.images.find( ( image ) => image.path.endsWith( "server_window.png" ) )?.reason,
		"native-interface"
	);
	assert.equal(
		manifest.images.find( ( image ) => image.path.endsWith( "zoomout_focus.png" ) )?.reason,
		"interactive-state"
	);
});

test("buildAssetPacks writes reusable 50 MiB-style binary packs with path offsets", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-asset-packs-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	const imageRoot = path.join( publicRoot, "assets", "images", "Media_extracted", "interface", "outer" );
	await mkdir( imageRoot, { recursive: true } );
	await writeFile( path.join( imageRoot, "button.png" ), "normal" );
	await writeFile( path.join( imageRoot, "button_focus.png" ), "focus" );
	await writeFile( path.join( imageRoot, "button_press.png" ), "press" );

	const result = await buildAssetPacks( {
		publicRoot,
		outputRoot: path.join( publicRoot, "assets", "packs" ),
		// Keep hash-cache writes inside the temp tree: without this the test pollutes
		// (and once truncated) the shared production cache at .state/file-hash-cache.json.
		hashCachePath: path.join( tempRoot, "file-hash-cache.json" ),
		targetBytes: 50 * 1024 * 1024,
		groups: [
			{
				name: "native-ui",
				load: "startup",
				files: [
					"/assets/images/Media_extracted/interface/outer/button.png",
					"/assets/images/Media_extracted/interface/outer/button_focus.png",
					"/assets/images/Media_extracted/interface/outer/button_press.png"
				]
			}
		]
	} );
	const index = JSON.parse( await readFile( result.outputPath, "utf8" ) );
	const packPublicPath = index.groups[0].packs[0].path;
	const packPath = path.join( publicRoot, packPublicPath.replace( /^\/+/, "" ) );
	const packBuffer = await readFile( packPath );
	const zstdPath = `${packPath}.zst`;

	assert.equal( packBuffer.subarray( 0, 8 ).toString( "ascii" ), ASSET_PACK_MAGIC );
	assert.equal( index.groups[0].load, "startup" );
	assert.equal( index.groups[0].targetBytes, 50 * 1024 * 1024 );
	assert.equal( index.assets.length, 3 );
	assert.match( packPublicPath, /^\/assets\/packs\/native-ui-001-[a-f0-9]{12}\.bin$/ );
	// The build writes the identity pack only; `pnpm assets compact` makes the zstd copy.
	assert.equal( index.groups[0].packs[0].zstdPath, undefined );
	await assert.rejects( stat( zstdPath ), { code: "ENOENT" } );

	const headerLength = packBuffer.readUInt32LE( 8 );
	const header = JSON.parse( packBuffer.subarray( 12, 12 + headerLength ).toString( "utf8" ) );
	const focusEntry = header.files.find( ( file ) => file.path.endsWith( "button_focus.png" ) );
	const dataStart = 12 + headerLength;
	const focusBytes = packBuffer.subarray(
		dataStart + focusEntry.offset,
		dataStart + focusEntry.offset + focusEntry.length
	);
	const focusManifestEntry = index.assets.find( ( asset ) => asset.path.endsWith( "button_focus.png" ) );

	assert.equal( focusEntry.mime, "image/png" );
	assert.equal( focusBytes.toString( "utf8" ), "focus" );
	assert.ok( focusManifestEntry );
	assert.equal( index.groups[0].packs[0].sha256, createHash( "sha256" ).update( packBuffer ).digest( "hex" ) );
	assert.equal( focusEntry.sha256, createHash( "sha256" ).update( focusBytes ).digest( "hex" ) );
	assert.equal( focusManifestEntry.sha256, focusEntry.sha256 );
	assert.equal( focusManifestEntry.offset, focusEntry.offset );
	assert.equal( focusManifestEntry.length, focusEntry.length );

	if ( typeof zlib.zstdDecompressSync === "function" ) {
		assert.deepEqual( zlib.zstdDecompressSync( await compressAssetPackZstd( packBuffer ) ), packBuffer );
	}
});

test("listPublicAssetFiles collects image files and excludes native UI paths", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-public-assets-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	await mkdir( path.join( publicRoot, "assets", "images", "Media_extracted", "interface", "outer" ), {
		recursive: true
	} );
	await mkdir( path.join( publicRoot, "assets", "world", "constantinople", "terrain-lightmaps" ), {
		recursive: true
	} );
	await mkdir( path.join( publicRoot, "assets", "packs" ), { recursive: true } );
	await writeFile(
		path.join( publicRoot, "assets", "images", "Media_extracted", "interface", "outer", "server_window.png" ),
		"ui"
	);
	await writeFile(
		path.join( publicRoot, "assets", "world", "constantinople", "terrain-lightmaps", "104-77.dds" ),
		"dds"
	);
	await writeFile( path.join( publicRoot, "assets", "world", "constantinople", "region-694e.json" ), "{}" );
	await writeFile( path.join( publicRoot, "assets", "packs", "manifest.json" ), "{}" );

	const files = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets" ],
		extensions: [ ".png", ".dds", ".json" ],
		exclude: [ "/assets/images/Media_extracted/interface/outer/server_window.png" ]
	} );

	assert.deepEqual( files, [
		"/assets/world/constantinople/region-694e.json",
		"/assets/world/constantinople/terrain-lightmaps/104-77.dds"
	] );
});
