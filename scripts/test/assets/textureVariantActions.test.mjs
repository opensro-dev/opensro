/*
===========================================================================

textureVariantActions.test.mjs - the ASTC variant pass as cached actions

Plan slice B2. Runs the real pass (NTX decode, the pinned astcenc) over a
fixture tree into a private output and a private action store. The second
run must lay every variant down from the store: same bytes, nothing
encoded, and nothing read from the encoder's own cache (the test deletes
it). A source whose bytes change misses and is encoded again; a source
only touched (same bytes, new time) still hits.

The pinned astcenc is windows-x64 only (encoders.mjs), and the pass skips
elsewhere, so the test does too.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { buildTextureVariants } from "../../build/textures/buildTextureVariants.mjs";
import { openStore } from "../../build/graph/store.mjs";
import { textureVariantPath } from "../../../apps/client-next/src/engine/foundation/assets/texture-variants.ts";

const NTX_MAGIC = 0x3158544e;
const D3DFMT_A8R8G8B8 = 21;
const NTX_HEADER_BYTES = 20;
const FIXTURE_JOBS = 2;
const TEXTURES = [
	"/assets/world/testville/object-textures/alpha.texture",
	"/assets/world/testville/object-textures/opaque.texture"
];

/*
================
bgraTexture

A BGRA NTX with its whole mip chain, painted by paint( x, y ) -> RGBA.
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
	const header = Buffer.alloc( NTX_HEADER_BYTES );
	header.writeUInt32LE( NTX_MAGIC, 0 );
	header.writeUInt32LE( width, 4 );
	header.writeUInt32LE( height, 8 );
	header.writeUInt32LE( D3DFMT_A8R8G8B8, 12 );
	header.writeUInt32LE( levels.length, 16 );
	return Buffer.concat( [ header, ...levels ] );
}

/*
================
variantBytes

The variant the pass wrote for one published path under out.
================
*/
function variantBytes( out, publicPath ) {
	return readFileSync( path.join( out, "client-public", ...textureVariantPath( publicPath, "astc" ).split( "/" ) ) );
}

test("a second build lays every texture variant down from the action store", async ( t ) => {
	if ( process.platform !== "win32" ) {
		t.skip( "the pinned astcenc is windows-x64 only" );
		return;
	}
	const root = mkdtempSync( path.join( tmpdir(), "sro-texvar-actions-" ) );
	const publicRoot = path.join( root, "public" ), out = path.join( root, "out" );
	const objects = path.join( publicRoot, "assets", "world", "testville", "object-textures" );
	mkdirSync( objects, { recursive: true } );
	const alpha = path.join( objects, "alpha.texture" ), opaque = path.join( objects, "opaque.texture" );
	writeFileSync( alpha, bgraTexture( 8, 8, ( x ) => [ x * 32, 200, 40, x < 4 ? 0 : 255 ] ) );
	writeFileSync( opaque, bgraTexture( 8, 8, () => [ 30, 90, 160, 255 ] ) );
	const store = await openStore( { root: path.join( root, "actions" ) } );
	const run = () => buildTextureVariants( { publicRoot, out, store, jobs: FIXTURE_JOBS } );
	try {
		const first = await run();
		assert.equal( first.textures, 2 );
		assert.equal( first.stored, 0 );
		assert.equal( first.encoded, 2 );
		const before = TEXTURES.map( publicPath => variantBytes( out, publicPath ) );

		// The encoder's cache is gone and the variants are deleted: only the
		// store can supply them, and it must supply the same bytes.
		rmSync( path.join( out, "intermediate" ), { recursive: true, force: true } );
		rmSync( path.join( out, "client-public" ), { recursive: true, force: true } );
		const second = await run();
		assert.equal( second.stored, 2 );
		assert.equal( second.placed, 2 );
		assert.equal( second.encoded + second.cached, 0 );
		assert.deepEqual( TEXTURES.map( publicPath => variantBytes( out, publicPath ) ), before );

		// A touched source keeps its key; a changed byte misses.
		const later = new Date( Date.now() + 60000 );
		utimesSync( opaque, later, later );
		const bytes = readFileSync( alpha );
		bytes[bytes.length - 1] ^= 0xff;
		writeFileSync( alpha, bytes );
		const third = await run();
		assert.equal( third.stored, 1 );
		assert.equal( third.encoded + third.cached, 1 );
	} finally {
		await store.shutdown();
		rmSync( root, { recursive: true, force: true } );
	}
});
