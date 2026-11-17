/*
===========================================================================

textureContainers.test.mjs - what the build writes, the client reads back

The build-side NTX writer is checked against the client's own decoder
(native-texture.ts), so the two cannot drift apart: a container holds its
levels and its picking mask, and an all-opaque level carries no mask.
The TGA writer and reader round-trip, and an .astc with the wrong block
size is refused.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import {
	astcBlocks,
	NTX_ASTC_4X4,
	ntxContainer,
	pickAlphaSection,
	tgaImage,
	tgaPixels
} from "../../build/textures/textureContainers.mjs";
import { decodeNativeTexture } from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";

const WIDTH = 8;
const HEIGHT = 4;
const ASTC_BLOCK_BYTES = 16;
const ASTC_HEADER = [ 0x13, 0xab, 0xa1, 0x5c ];

/*
================
pattern

An RGBA level with a varied alpha channel, or an opaque one.
================
*/
function pattern( opaque ) {
	const rgba = Buffer.alloc( WIDTH * HEIGHT * 4 );
	for ( let i = 0; i < rgba.length; i++ ) rgba[i] = (i * 29 + 7) & 255;
	if ( opaque ) { for ( let i = 3; i < rgba.length; i += 4 ) rgba[i] = 255; }
	return rgba;
}

/*
================
astcLevel

One ASTC 4x4 level of WIDTH x HEIGHT, the blocks numbered.
================
*/
function astcLevel() {
	const blocks = (WIDTH / 4) * (HEIGHT / 4);
	return Buffer.from( Array.from( { length: blocks * ASTC_BLOCK_BYTES }, ( _, i ) => i & 255 ) );
}

test("an NTX container decodes in the client with its level and mask", () => {
	const rgba = pattern( false );
	const level = astcLevel();
	const texture = decodeNativeTexture(
		new Uint8Array(
			ntxContainer( WIDTH, HEIGHT, NTX_ASTC_4X4, [ level ], [ pickAlphaSection( rgba, WIDTH, HEIGHT ) ] )
		)
	);
	assert.equal( texture.width, WIDTH );
	assert.equal( texture.height, HEIGHT );
	assert.equal( texture.format, "astc-4x4-unorm" );
	assert.deepEqual( Buffer.from( texture.levels[0] ), level );
	assert.deepEqual(
		Array.from( texture.pickAlpha ),
		Array.from( { length: WIDTH * HEIGHT }, ( _, i ) => rgba[i * 4 + 3] )
	);
});

test("an opaque level carries no picking mask", () => {
	assert.equal( pickAlphaSection( pattern( true ), WIDTH, HEIGHT ).length, 0 );
	const texture = decodeNativeTexture(
		new Uint8Array(
			ntxContainer( WIDTH, HEIGHT, NTX_ASTC_4X4, [ astcLevel() ], [
				pickAlphaSection( pattern( true ), WIDTH, HEIGHT )
			] )
		)
	);
	assert.equal( texture.pickAlpha, undefined );
});

test("a TGA written for an encoder reads back as the same pixels", () => {
	const rgba = pattern( false );
	const back = tgaPixels( tgaImage( rgba, WIDTH, HEIGHT ) );
	assert.equal( back.width, WIDTH );
	assert.equal( back.height, HEIGHT );
	assert.deepEqual( back.rgba, rgba );
});

test("an .astc gives its blocks, and a file of another block size is refused", () => {
	const blocks = astcLevel();
	const file = ( side ) =>
		Buffer.concat( [ Buffer.from( [ ...ASTC_HEADER, side, side, 1 ] ), Buffer.alloc( 9 ), blocks ] );
	assert.deepEqual( astcBlocks( file( 4 ), 4 ), blocks );
	assert.throws( () => astcBlocks( file( 6 ), 4 ), /Unexpected astcenc output/ );
});
