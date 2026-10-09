/*
===========================================================================

asset-delivery.test.mjs - a pack build delivers exactly what it was given

End to end through the real builder and the real client reader: new files
are stored gzip-compressed inside SROPACK2 packs when that saves a tenth,
world animation catalogs carry their source index, two builds of the same
tree are byte-identical, and a deployment with no loose files serves every
member from its packs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { buildAssetPacks } from "../../../../scripts/build/assetPacks.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

const { createPacks } = await import( sourceFileUrl( "src/engine/runtime/assets/worker/packs/packs.ts" ).href );
const GLB = "/assets/future/new-feature.glb", CATALOG = "/assets/world/unlisted-region/animated-objects.json";
const PNG = "/assets/future/already-compressed.png";

/*
================
sha
================
*/
function sha( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
build

A loose tree of three new files packed by the standard builder into root.
================
*/
async function build( t, label ) {
	const root = await mkdtemp( path.join( os.tmpdir(), `sro-delivery-${label}-` ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const files = {
		[GLB]: Buffer.alloc( 128 << 10, 37 ),
		[CATALOG]: Buffer.from( JSON.stringify( { objects: { "native/future.bsr": { glbPublicPath: GLB } } } ) ),
		// Hash output stands for already-compressed media: deterministic, so both
		// builds see the same bytes, and gzip cannot save a tenth of it.
		[PNG]: Buffer.concat(
			Array.from( { length: 128 }, ( _, i ) => createHash( "sha256" ).update( String( i ) ).digest() )
		)
	};
	for ( const [file, bytes] of Object.entries( files ) ) {
		await mkdir( path.dirname( path.join( root, file ) ), { recursive: true } );
		await writeFile( path.join( root, file ), bytes );
	}
	const result = await buildAssetPacks( {
		publicRoot: root,
		outputRoot: path.join( root, "assets/packs" ),
		hashCachePath: path.join( root, "hash-cache.json" ),
		memberCacheRoot: path.join( root, "member-cache" ),
		groups: [ { name: "test", load: "startup", files: Object.keys( files ) } ]
	} );
	const index = JSON.parse( await readFile( result.outputPath, "utf8" ) );
	return { root, files, index };
}

/*
================
reader

The client's pack reader over a deployed tree.
================
*/
function reader( root ) {
	return createPacks( async url =>
		new Uint8Array( await readFile( path.join( root, decodeURIComponent( new URL( url ).pathname ) ) ) )
	);
}

test("a standard build stores new files losslessly, indexes animation sources and is byte-identical twice", async t => {
	const first = await build( t, "a" ), second = await build( t, "b" );
	const row = file => first.index.assets.find( a => a.path === file );
	assert.equal( first.index.version, 2 );
	assert.equal( first.index.deliveryVersion, 2 );
	assert.equal( row( GLB ).stored?.encoding, "gzip", "a compressible model travels compressed" );
	assert.ok( row( GLB ).stored.length < row( GLB ).length / 10 );
	assert.equal( row( PNG ).stored, undefined, "already-compressed media stays raw" );
	assert.deepEqual( row( CATALOG ).animationSources, [ "native/future.bsr" ] );
	assert.equal( row( CATALOG ).animationDigest, row( CATALOG ).sha256 );
	// Reproducible on any machine: the pack (and its URL) depends on content only.
	assert.deepEqual(
		first.index.groups[0].packs.map( p => [ path.basename( p.path ), p.sha256 ] ),
		second.index.groups[0].packs.map( p => [ path.basename( p.path ), p.sha256 ] )
	);
	const pack = await readFile( path.join( first.root, first.index.groups[0].packs[0].path ) );
	const start = 12 + pack.readUInt32LE( 8 ), glb = row( GLB );
	assert.equal( pack[start + glb.offset + 9], 255, "the gzip header names no operating system" );
});

test("the client reader returns every member exactly as it was built", async t => {
	const f = await build( t, "read" ), packs = reader( f.root ), signal = new AbortController().signal;
	t.after( () => packs.dispose() );
	for ( const [file, bytes] of Object.entries( f.files ) ) {
		const read = await packs.read( new URL( "http://localhost" + file ), 1 << 20, signal );
		assert.equal( sha( read ), sha( bytes ), file );
	}
	assert.ok( packs.stats().storedBytes > 0 && packs.stats().storedBytes < packs.stats().decodedBytes );
});

test("a deployment with no loose files serves every member from its packs", async t => {
	const f = await build( t, "packed" );
	for ( const file of Object.keys( f.files ) ) await rm( path.join( f.root, file ) );
	for ( const [file, bytes] of Object.entries( f.files ) ) {
		assert.equal( sha( readPublishedAssetBytesSync( file, f.root ) ), sha( bytes ), file );
	}
});
