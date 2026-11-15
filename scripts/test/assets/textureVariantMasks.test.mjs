/*
===========================================================================

textureVariantMasks.test.mjs - the picking-mask cache of buildTextureVariants

A variant's picking mask is computed once per source digest and decoder:
a second texture of the same bytes reads the record instead of decoding,
an opaque source records an empty mask, and a different decoder source
names a different cache, so a decoder fix recomputes every mask.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { codeClosure } from "../../build/shared/codeStamp.mjs";
import { cachedPickAlpha, maskCacheRoot, maskCodeHash } from "../../build/textures/buildTextureVariants.mjs";

const DIGEST = "ab".repeat( 32 );

/*
================
scratchMaskRoot
================
*/
function scratchMaskRoot( t ) {
	const root = mkdtempSync( path.join( os.tmpdir(), "sro-pka-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	const maskRoot = maskCacheRoot( root, "1".repeat( 64 ) );
	mkdirSync( maskRoot, { recursive: true } );
	return maskRoot;
}

test("a mask is computed once and then read back unchanged", async t => {
	const maskRoot = scratchMaskRoot( t );
	const mask = Buffer.from( [ 0x50, 0x4b, 0x41, 0x38, 0, 128, 255 ] );
	let picks = 0;
	const pick = () => (picks++, mask);
	assert.deepEqual( await cachedPickAlpha( maskRoot, DIGEST, pick ), mask );
	assert.deepEqual( await cachedPickAlpha( maskRoot, DIGEST, pick ), mask );
	assert.equal( picks, 1 );
});

test("an opaque source records an empty mask and is not decoded again", async t => {
	const maskRoot = scratchMaskRoot( t );
	let picks = 0;
	const pick = () => (picks++, Buffer.alloc( 0 ));
	await cachedPickAlpha( maskRoot, DIGEST, pick );
	assert.equal( (await cachedPickAlpha( maskRoot, DIGEST, pick )).length, 0 );
	assert.equal( picks, 1 );
});

test("another decoder source names another mask cache", () => {
	const root = path.join( os.tmpdir(), "sro-pka-names" );
	const first = maskCacheRoot( root, "1".repeat( 64 ) );
	assert.equal( maskCacheRoot( root, "1".repeat( 64 ) ), first );
	assert.notEqual( maskCacheRoot( root, "2".repeat( 64 ) ), first );
	assert.ok( path.basename( first ).startsWith( "pick-alpha-" ) );
});

test("the mask key covers the mask writer and the decoder it imports", async () => {
	const closure = (await codeClosure(
		fileURLToPath( new URL( "../../build/textures/buildTextureVariants.mjs", import.meta.url ) )
	))
		.map( file => path.basename( file ) );
	assert.ok( closure.includes( "buildTextureVariants.mjs" ) );
	assert.ok( closure.includes( "native-texture.ts" ) );
	assert.match( await maskCodeHash(), /^[0-9a-f]{64}$/ );
});
