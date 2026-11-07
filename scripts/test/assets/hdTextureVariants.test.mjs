/*
===========================================================================

hdTextureVariants.test.mjs - the HD texture lane and its own pack index

Port-only (#2061). Builds synthetic BGRA NTX sources in a fixture tree
and runs the real lane (NTX decode, Real-ESRGAN x2, texconv BC7,
astcenc) over them: a variant is x2 with a re-derived chain and exact
level bytes under the client's own decoder, an alpha source carries its
picking mask, an opaque one does not, and a texture that cannot upscale
to whole blocks keeps its original. Then the HD pack step: its index
lives under /hd, is admitted under the /hd root, and is rejected under
the default /assets root - the separation the zero-HD-bytes rule needs.

The pinned tools are windows-x64 only (encoders.mjs), so the tool tests
skip elsewhere; the lane itself also skips there.

===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { buildHdTextureVariants } from "../../build/textures/buildHdTextureVariants.mjs";
import { buildHdTexturePacks, hdTexturesEnabled } from "../../build/textures/buildHdTexturePacks.mjs";
import { validateAssetPackIndex } from "../../build/assetPacks.mjs";
import {
	decodeNativeTexture,
	nativeTextureLevelBytes
} from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";
import { hdTextureVariantPath } from "../../../apps/client-next/src/engine/foundation/assets/texture-variants.ts";

const NTX_MAGIC = 0x3158544e;
const D3DFMT_A8R8G8B8 = 21;
const WIN32_ONLY = "the pinned HD tools are windows-x64 only";

/*
================
bgraTexture

A synthetic uncompressed NTX with a full chain; paint(x, y) gives the
level-0 RGBA of each texel, deeper levels repeat it at their size.
================
*/
function bgraTexture( width, height, paint ) {
	const levels = [];
	for ( let w = width, h = height;; w = Math.max( 1, w >> 1 ), h = Math.max( 1, h >> 1 ) ) {
		const bytes = Buffer.alloc( w * h * 4 );
		for ( let y = 0; y < h; y++ ) {
			for ( let x = 0; x < w; x++ ) {
				const [red, green, blue, alpha] = paint( x, y ), at = (y * w + x) * 4;
				bytes[at] = blue;
				bytes[at + 1] = green;
				bytes[at + 2] = red;
				bytes[at + 3] = alpha;
			}
		}
		levels.push( bytes );
		if ( w === 1 && h === 1 ) break;
	}
	const header = Buffer.alloc( 20 );
	header.writeUInt32LE( NTX_MAGIC, 0 );
	header.writeUInt32LE( width, 4 );
	header.writeUInt32LE( height, 8 );
	header.writeUInt32LE( D3DFMT_A8R8G8B8, 12 );
	header.writeUInt32LE( levels.length, 16 );
	return Buffer.concat( [ header, ...levels ] );
}

/*
================
fixture

A throwaway published tree: an alpha-bearing and an opaque world object
texture, one too narrow to upscale into whole blocks, and a terrain
lightmap the lane must not touch.
================
*/
function fixture() {
	const root = mkdtempSync( path.join( tmpdir(), "sro-hd-fixture-" ) );
	const publicRoot = path.join( root, "public" ), out = path.join( root, "out" );
	const objects = path.join( publicRoot, "assets", "world", "testville", "object-textures" );
	const lightmaps = path.join( publicRoot, "assets", "world", "testville", "terrain-lightmaps" );
	mkdirSync( objects, { recursive: true } );
	mkdirSync( lightmaps, { recursive: true } );
	writeFileSync(
		path.join( objects, "alpha.texture" ),
		bgraTexture( 8, 8, ( x ) => [ x * 32, 200, 40, x < 4 ? 0 : 255 ] )
	);
	writeFileSync( path.join( objects, "opaque.texture" ), bgraTexture( 8, 8, () => [ 30, 90, 160, 255 ] ) );
	writeFileSync( path.join( objects, "narrow.texture" ), bgraTexture( 1, 8, () => [ 30, 90, 160, 255 ] ) );
	writeFileSync( path.join( lightmaps, "lm.texture" ), bgraTexture( 8, 8, () => [ 30, 90, 160, 255 ] ) );
	return { root, publicRoot, out };
}

/*
================
publishedHd

The fixture's HD variant of one published path, decoded by the client.
================
*/
function publishedHd( out, publicPath, format ) {
	const variantPath = hdTextureVariantPath( publicPath, format );
	const target = path.join( out, "client-public", ...variantPath.split( "/" ) );
	return existsSync( target ) ? decodeNativeTexture( new Uint8Array( readFileSync( target ) ) ) : null;
}

test("the HD lane is opt-in", () => {
	assert.equal( hdTexturesEnabled( {} ), false );
	assert.equal( hdTexturesEnabled( { SRO_HD_TEXTURES: "1" } ), true );
	assert.equal( hdTexturesEnabled( { SRO_HD_TEXTURES: "true" } ), false );
});

test("the HD lane upscales world object textures to x2 BC7 and ASTC variants", async ( t ) => {
	if ( process.platform !== "win32" ) {
		t.skip( WIN32_ONLY );
		return;
	}
	const { root, publicRoot, out } = fixture();
	try {
		const summary = await buildHdTextureVariants( { publicRoot, out, jobs: 2 } );
		// The lightmap is outside the world object families; the narrow
		// texture cannot upscale to whole 4x4 blocks and keeps its original.
		assert.equal( summary.textures, 3 );
		assert.equal( summary.encoded, 2 );
		assert.equal( summary.kept, 1 );

		for ( const format of [ "bc7", "astc" ] ) {
			const alpha = publishedHd( out, "/assets/world/testville/object-textures/alpha.texture", format );
			assert.equal( alpha.format, format === "bc7" ? "bc7-rgba-unorm" : "astc-4x4-unorm" );
			assert.equal( alpha.width, 16 );
			assert.equal( alpha.height, 16 );
			assert.equal( alpha.levels.length, 5 );
			for ( let level = 0; level < alpha.levels.length; level++ ) {
				assert.equal(
					alpha.levels[level].byteLength,
					nativeTextureLevelBytes( alpha.format, Math.max( 1, 16 >> level ), Math.max( 1, 16 >> level ) )
				);
			}
			// The picking mask rides the variant: its source alpha is
			// anywhere below 255, so picking needs the upscaled mask.
			assert.equal( alpha.pickAlpha?.length, 16 * 16 );

			const opaque = publishedHd( out, "/assets/world/testville/object-textures/opaque.texture", format );
			assert.equal( opaque.pickAlpha, undefined );
		}
		assert.equal( publishedHd( out, "/assets/world/testville/object-textures/narrow.texture", "bc7" ), null );
		assert.equal( publishedHd( out, "/assets/world/testville/terrain-lightmaps/lm.texture", "bc7" ), null );

		// A second run is served by the digest cache and re-publishes.
		const rerun = await buildHdTextureVariants( { publicRoot, out, jobs: 2 } );
		assert.equal( rerun.encoded, 0 );
		assert.equal( rerun.cached, 2 );
	} finally {
		rmSync( root, { recursive: true, force: true } );
	}
});

test("the HD packs sit behind their own index under /hd, never /assets", async ( t ) => {
	if ( process.platform !== "win32" ) {
		t.skip( WIN32_ONLY );
		return;
	}
	const { root, publicRoot, out } = fixture();
	try {
		await buildHdTextureVariants( { publicRoot, out, jobs: 2 } );
		const hdRoot = path.join( out, "client-public" );
		const summary = await buildHdTexturePacks( {
			publicRoot: hdRoot,
			hashCachePath: path.join( root, "hash-cache.json" ),
			memberCacheRoot: path.join( root, "member-cache" )
		} );
		assert.equal( summary.assetCount, 4 );

		const index = JSON.parse( readFileSync( path.join( hdRoot, "hd", "packs", "manifest.json" ), "utf8" ) );
		assert.deepEqual(
			index.groups.map( group => group.name ).sort(),
			[ "texture-variants-hd-astc", "texture-variants-hd-bc7" ]
		);
		assert.ok( index.assets.every( asset => asset.path.startsWith( "/hd/texture-variants/" ) ) );
		assert.ok( index.assets.every( asset => asset.packPath.startsWith( "/hd/packs/" ) ) );
		// Admitted under its own root, rejected under the default one: the
		// two indexes can never be merged by accident.
		assert.doesNotThrow( () => validateAssetPackIndex( index, "/hd" ) );
		assert.throws( () => validateAssetPackIndex( index ), /not client-admissible/ );
		for ( const group of index.groups ) {
			for ( const pack of group.packs ) {
				const packFile = path.join( hdRoot, ...pack.path.split( "/" ) );
				assert.equal( existsSync( packFile ), true, pack.path );
				assert.equal( readFileSync( packFile ).length, pack.bytes );
			}
		}
	} finally {
		rmSync( root, { recursive: true, force: true } );
	}
});

test("an HD build with no variants writes no pack index", async () => {
	const root = mkdtempSync( path.join( tmpdir(), "sro-hd-empty-" ) );
	try {
		const summary = await buildHdTexturePacks( {
			publicRoot: root,
			hashCachePath: path.join( root, "hash-cache.json" ),
			memberCacheRoot: path.join( root, "member-cache" )
		} );
		assert.equal( summary.skipped, true );
		assert.equal( existsSync( path.join( root, "hd", "packs", "manifest.json" ) ), false );
	} finally {
		rmSync( root, { recursive: true, force: true } );
	}
});
