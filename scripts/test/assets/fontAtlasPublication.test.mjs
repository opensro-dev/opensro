/*
===========================================================================

fontAtlasPublication.test.mjs - packed font atlases publish only when whole

A font atlas descriptor binds its image by digest and dimensions; publication
refuses stale or missing images, even across groups refreshed separately.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import { publishAssetPackManifest, validatePackedFontAtlases } from "../../build/assetPackPublication.mjs";
import { ASSET_SCHEMA } from "../../build/assetSchema.mjs";

const hash = b => createHash( "sha256" ).update( b ).digest( "hex" );
const png = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=",
	"base64"
);
/*
================
fixture
================
*/
async function fixture( t, { height = 1, image = png, expected = hash( png ), name = "another-font" } = {} ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-font-publication-" ) );
	t.after( async () => {
		assert.equal( path.dirname( root ), os.tmpdir() );
		assert.ok( path.basename( root ).startsWith( "sro-font-publication-" ) );
		await rm( root, { recursive: true, force: true } );
	} );
	await mkdir( path.join( root, "assets/packs" ), { recursive: true } );
	const imagePath = "/assets/fonts/" + name + ".png", descriptorPath = "/assets/fonts/" + name + ".json.gz";
	const descriptor = gzipSync(
		Buffer.from(
			JSON.stringify( {
				image: imagePath,
				imageSha256: expected,
				atlasWidth: 1,
				atlasHeight: height,
				fonts: { 0: {} }
			} )
		)
	);
	const members = [ [ descriptorPath, descriptor ], [ imagePath, image ] ], groups = [], assets = [];
	// Separate immutable packs reproduce the cross-group partial-refresh failure.
	for ( const [i, [member, bytes]] of members.entries() ) {
		const header = Buffer.from( "{}" ), prefix = Buffer.alloc( 12 );
		prefix.write( "SROPACK1" );
		prefix.writeUInt32LE( header.length, 8 );
		const identity = Buffer.concat( [ prefix, header, bytes ] ), packPath = "/assets/packs/" + i + ".bin";
		await writeFile( path.join( root, packPath ), identity );
		// A complete client-admissible index: publication validates counts and ownership first.
		groups.push( {
			name: String( i ),
			assetCount: 1,
			totalBytes: bytes.length,
			packs: [ { path: packPath, bytes: identity.length, assetCount: 1, sha256: hash( identity ) } ]
		} );
		assets.push( {
			path: member,
			packPath,
			group: String( i ),
			offset: 0,
			length: bytes.length,
			sha256: hash( bytes )
		} );
	}
	return {
		root,
		index: { assetSchema: ASSET_SCHEMA, groups, assets },
		filename: path.join( root, "assets/packs/manifest.json" )
	};
}
test("publishes a packed atlas without loose sources and discovers arbitrary font names", async t => {
	const { root, index, filename } = await fixture( t );
	await publishAssetPackManifest( root, filename, Buffer.from( JSON.stringify( index ) ) );
	assert.deepEqual( JSON.parse( await readFile( filename ) ), index );
});
test("rejects mixed atlas dimensions before replacing the served manifest", async t => {
	const { root, index, filename } = await fixture( t, { height: 2 } );
	await writeFile( filename, "previous valid publication" );
	await assert.rejects(
		publishAssetPackManifest( root, filename, Buffer.from( JSON.stringify( index ) ) ),
		/dimensions disagree/
	);
	assert.equal( await readFile( filename, "utf8" ), "previous valid publication" );
});
test("same-size stale pixels fail digest binding despite valid individual pack hashes", async t => {
	const { root, index } = await fixture( t, { expected: hash( Buffer.from( "different pixels" ) ) } );
	await assert.rejects( validatePackedFontAtlases( index, root ), /image digest mismatch/ );
});
test("loose corrections cannot disguise stale packed pixels", async t => {
	const { root, index } = await fixture( t, { height: 2 } );
	await mkdir( path.join( root, "assets/fonts" ), { recursive: true } );
	const loose = Buffer.from( png );
	loose.writeUInt32BE( 2, 20 );
	await writeFile( path.join( root, index.assets[1].path ), loose );
	await assert.rejects( validatePackedFontAtlases( index, root ), /dimensions disagree/ );
});
test("partial staging may omit cross-group images; final publication must contain them", async t => {
	const { root, index } = await fixture( t );
	index.assets.pop();
	await validatePackedFontAtlases( index, root, { partial: true } );
	await assert.rejects( validatePackedFontAtlases( index, root ), /image missing/ );
});
