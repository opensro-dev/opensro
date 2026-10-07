/*
===========================================================================

slotEffectPublication.test.mjs - focused slot effects keep packed metadata current

Run the real publisher against an isolated generated tree. A loose catalog
must not hide an older packed copy, and unrelated compacted members survive.

===========================================================================
*/
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { gzipSync } from "node:zlib";
import test from "node:test";
import { buildAssetPacks } from "../../build/assetPacks.mjs";
import { imagePublicPath } from "../../build/shared/cifResources.mjs";
import { slotEffectRuntimeImageReferences } from "../../build/shared/cifRuntimeImageCatalog.mjs";
import { readPackedAssetBytesSync } from "../../lib/publishedAsset.mjs";

const run = promisify( execFile );
const PUBLISHER = fileURLToPath( new URL( "../../refresh_slot_effect_asset_packs.mjs", import.meta.url ) );
const CATALOG = "/assets/cif/cif-sprite-catalog.json";
const SENTINEL = "/assets/unchanged.json";
const UNRELATED_IMAGE = "/assets/images/unchanged.png";
const PNG = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jZ1kAAAAASUVORK5CYII=",
	"base64"
);

/*
================
writeFixture
================
*/
async function writeFixture( root, relative, bytes ) {
	const filename = path.join( root, relative.replace( /^\/+/, "" ) );
	await mkdir( path.dirname( filename ), { recursive: true } );
	await writeFile( filename, bytes );
}

for ( const encoding of [ "identity", "gzip", "both" ] ) {
	test(`slot effect publication refreshes the packed ${encoding} catalog`, async t => {
		const temporary = await mkdtemp( path.join( os.tmpdir(), "sro-slot-publication-" ) );
		t.after( async () => {
			assert.equal( path.dirname( temporary ), path.resolve( os.tmpdir() ) );
			await rm( temporary, { recursive: true, force: true } );
		} );
		const publicRoot = path.join( temporary, "client-public" );
		const oldCatalog = {
			resourcesByDdjPath: { "unchanged.ddj": { publicPath: UNRELATED_IMAGE, width: 1, height: 1 } }
		};
		const oldBytes = Buffer.from( JSON.stringify( oldCatalog ) );
		await writeFixture( publicRoot, CATALOG, oldBytes );
		await writeFixture( publicRoot, CATALOG + ".gz", gzipSync( oldBytes ) );
		await writeFixture( publicRoot, SENTINEL, '{"preserved":true}' );
		await writeFixture( publicRoot, UNRELATED_IMAGE, PNG );
		const catalogFiles = encoding === "identity" ?
			[ CATALOG ] :
			encoding === "gzip" ?
			[ CATALOG + ".gz" ] :
			[ CATALOG, CATALOG + ".gz" ];
		const built = await buildAssetPacks( {
			publicRoot,
			outputRoot: path.join( publicRoot, "assets", "packs" ),
			hashCachePath: path.join( temporary, "hash-cache.json" ),
			groups: [
				{ name: "game-data", load: "startup", files: [ ...catalogFiles, SENTINEL ] },
				{ name: "game-images", load: "startup", files: [ UNRELATED_IMAGE ] }
			]
		} );
		const before = JSON.parse( await readFile( built.outputPath, "utf8" ) );
		await rm( path.join( publicRoot, SENTINEL.slice( 1 ) ) );
		for ( const reference of slotEffectRuntimeImageReferences ) {
			await writeFixture(
				path.join( temporary, "intermediate", "images" ),
				imagePublicPath( reference ).slice( "/assets/images/".length ),
				PNG
			);
		}
		await run( process.execPath, [ PUBLISHER ], {
			env: { ...process.env, SRO_GENERATED_ROOT: temporary },
			timeout: 120000,
			windowsHide: true
		} );
		const catalog = JSON.parse( readPackedAssetBytesSync( CATALOG, publicRoot ).toString( "utf8" ) );
		assert.deepEqual( catalog.resourcesByDdjPath["unchanged.ddj"], oldCatalog.resourcesByDdjPath["unchanged.ddj"] );
		for ( const reference of slotEffectRuntimeImageReferences ) {
			assert.deepEqual( catalog.resourcesByDdjPath[reference], {
				sourcePath: reference,
				publicPath: imagePublicPath( reference ),
				width: 1,
				height: 1
			} );
			assert.deepEqual( readPackedAssetBytesSync( imagePublicPath( reference ), publicRoot ), PNG );
		}
		assert.equal( readPackedAssetBytesSync( SENTINEL, publicRoot ).toString( "utf8" ), '{"preserved":true}' );
		const after = JSON.parse( await readFile( built.outputPath, "utf8" ) );
		for ( const member of [ ...catalogFiles, SENTINEL, UNRELATED_IMAGE ] ) {
			const rows = after.assets.filter( row => row.path === member );
			assert.equal( rows.length, 1, `unique owner for ${member}` );
			const previous = before.assets.find( row => row.path === member );
			assert.equal( rows[0].group, previous.group );
			if ( catalogFiles.includes( member ) ) assert.notEqual( rows[0].sha256, previous.sha256 );
			else assert.equal( rows[0].sha256, previous.sha256 );
		}
	});
}
