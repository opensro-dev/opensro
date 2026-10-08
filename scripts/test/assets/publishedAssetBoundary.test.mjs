import assert from "node:assert/strict";
import { gzipSync } from "node:zlib";
import { mkdir, mkdtemp, readFile, rename, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { buildAssetPacks, compressAssetPackZstd } from "../../build/assetPacks.mjs";
import {
	listPublishedAssetPathsSync,
	publishedAssetExistsSync,
	readPackedAssetBytesSync,
	readPublishedAssetJsonSync
} from "../../lib/publishedAsset.mjs";

test("published assets keep one logical read contract across loose and compact installations", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-published-asset-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	const dataRoot = path.join( publicRoot, "assets", "data" );
	const packsRoot = path.join( publicRoot, "assets", "packs" );
	const logicalPath = "/assets/data/catalog.json";
	const gzipPath = path.join( dataRoot, "catalog.json.gz" );
	await mkdir( dataRoot, { recursive: true } );
	await writeFile( gzipPath, gzipSync( Buffer.from( JSON.stringify( { source: "pack", rows: [ 1, 2, 3 ] } ) ) ) );

	const result = await buildAssetPacks( {
		publicRoot,
		outputRoot: packsRoot,
		hashCachePath: path.join( tempRoot, "file-hash-cache.json" ),
		groups: [ { name: "game-data", load: "startup", files: [ `${logicalPath}.gz` ] } ]
	} );
	const index = JSON.parse( await readFile( result.outputPath, "utf8" ) );
	assert.equal( index.assets.length, 1, "one generated representation is packed exactly once" );

	const loosePath = path.join( dataRoot, "catalog.json" );
	await writeFile( loosePath, JSON.stringify( { source: "loose" } ) );
	assert.deepEqual( readPublishedAssetJsonSync( logicalPath, publicRoot ), { source: "loose" } );

	const archiveRoot = path.join( tempRoot, "authoring-projection" );
	await mkdir( archiveRoot, { recursive: true } );
	await rename( loosePath, path.join( archiveRoot, "catalog.json" ) );
	await rename( gzipPath, path.join( archiveRoot, "catalog.json.gz" ) );
	const identityPublicPath = index.groups[0].packs[0].path;
	// What `pnpm assets compact` does: write the pack's zstd copy and record it in
	// the index, so the reader serves the compact tree after the identity pack goes.
	const identityFile = path.join( publicRoot, identityPublicPath.replace( /^\/+/, "" ) );
	const compact = await compressAssetPackZstd( await readFile( identityFile ) );
	await writeFile( identityFile + ".zst", compact );
	index.groups[0].packs[0].zstdPath = identityPublicPath + ".zst";
	index.groups[0].packs[0].zstdBytes = compact.length;
	await writeFile( result.outputPath, JSON.stringify( index ) );
	await rename(
		path.join( publicRoot, identityPublicPath.replace( /^\/+/, "" ) ),
		path.join( archiveRoot, path.basename( identityPublicPath ) )
	);

	assert.equal( publishedAssetExistsSync( logicalPath, publicRoot ), true );
	assert.deepEqual( readPublishedAssetJsonSync( logicalPath, publicRoot ), { source: "pack", rows: [ 1, 2, 3 ] } );
	assert.deepEqual( listPublishedAssetPathsSync( "/assets/data/", publicRoot ), [ `${logicalPath}.gz` ] );
	await writeFile( loosePath, JSON.stringify( { source: "new-unpublished-build" } ) );
	assert.deepEqual( readPublishedAssetJsonSync( logicalPath, publicRoot ), { source: "new-unpublished-build" } );
	assert.deepEqual( JSON.parse( readPackedAssetBytesSync( logicalPath, publicRoot ) ), {
		source: "pack",
		rows: [ 1, 2, 3 ]
	}, "delivery verification cannot pass against shadowing loose data" );
	await writeFile( path.join( dataRoot, "unpublished.json" ), "{}" );
	assert.throws(
		() => readPackedAssetBytesSync( "/assets/data/unpublished.json", publicRoot ),
		/neither loose nor packed/
	);
});
