/*
===========================================================================

texel-readback.test.mjs - padded GPU copies unpack to packed RGBA

Rows padded to 256 bytes, in the three portrait formats, come back as
tightly packed RGBA with alpha kept.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { unpackRgba8, paddedRowBytes, texelBytes, opaqueBox, COPY_ROW_ALIGNMENT } = await import(
	"../../src/engine/foundation/rendering/texel-readback.ts"
);

test("rows pad to the copy alignment", () => {
	assert.equal( paddedRowBytes( 1, "rgba8unorm" ), COPY_ROW_ALIGNMENT );
	assert.equal( paddedRowBytes( 64, "rgba8unorm" ), 256 );
	assert.equal( paddedRowBytes( 65, "rgba8unorm" ), 512 );
	assert.equal( paddedRowBytes( 32, "rgba16float" ), 256 );
	assert.equal( texelBytes( "depth24plus" ), 0 );
});

test("rgba8 and bgra8 rows unpack, skipping the padding", () => {
	const width = 2, height = 2, stride = paddedRowBytes( width, "rgba8unorm" );
	const source = new Uint8Array( stride * height ).fill( 99 );
	source.set( [ 1, 2, 3, 4, 5, 6, 7, 8 ], 0 );
	source.set( [ 9, 10, 11, 12, 13, 14, 15, 0 ], stride );
	assert.deepEqual( [ ...unpackRgba8( source, width, height, "rgba8unorm" ) ], [
		1,
		2,
		3,
		4,
		5,
		6,
		7,
		8,
		9,
		10,
		11,
		12,
		13,
		14,
		15,
		0
	] );
	assert.deepEqual( [ ...unpackRgba8( source, width, height, "bgra8unorm" ) ].slice( 0, 4 ), [ 3, 2, 1, 4 ] );
});

test("half floats clamp to bytes and keep a transparent alpha", () => {
	const source = new Uint8Array( paddedRowBytes( 1, "rgba16float" ) );
	const view = new DataView( source.buffer );
	// 1.0, 0.5, 2.0 (clamps), 0.0 alpha.
	[ 0x3c00, 0x3800, 0x4000, 0x0000 ].forEach( ( half, i ) => view.setUint16( i * 2, half, true ) );
	assert.deepEqual( [ ...unpackRgba8( source, 1, 1, "rgba16float" ) ], [ 255, 128, 255, 0 ] );
});

test("short or unreadable copies are refused", () => {
	assert.throws( () => unpackRgba8( new Uint8Array( 10 ), 2, 2, "rgba8unorm" ) );
	assert.throws( () => unpackRgba8( new Uint8Array( 512 ), 1, 1, "depth24plus" ) );
	assert.throws( () => unpackRgba8( new Uint8Array( 512 ), 0, 1, "rgba8unorm" ) );
});

test("the opaque box spans every drawn pixel, exclusive at right and bottom", () => {
	const width = 4, height = 3, rgba = new Uint8ClampedArray( width * height * 4 );
	assert.equal( opaqueBox( rgba, width, height ), null );
	rgba[(1 * width + 1) * 4 + 3] = 255;
	rgba[(2 * width + 2) * 4 + 3] = 1;
	assert.deepEqual( opaqueBox( rgba, width, height ), [ 1, 1, 3, 3 ] );
});
