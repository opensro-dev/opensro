/*
===========================================================================

native-texture.test.mjs - compressed mip admission and GPU fallback behavior

Fixtures encode individual BC blocks independently of the production decoder.
The GPU recorder checks bytes, mip extents and lifetime on both capability
paths, including malformed resources that must fail before allocation.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";

const { decodeNativeTexture, decodeNativeTextureLevel } = await import(
	"../../src/engine/foundation/assets/native-texture.ts"
);
const { createImages } = await import( "../../src/engine/runtime/renderer/device/images.ts" );

const DXT1 = 0x31545844;
const DXT3 = 0x33545844;
const DXT5 = 0x35545844;
const BLOCK_PIXELS = 16;

/*
================
encodedTexture

Write a 4x4 NTX resource with three authored mips (or fewer, as terrain
ships). The caller supplies one block, reused in the 4x4, 2x2 and 1x1 levels
so cropping can be checked.
================
*/
function encodedTexture( format, block, levels = 3 ) {
	const bytes = new Uint8Array( 20 + block.length * levels );
	const header = new DataView( bytes.buffer );
	[ 0x3158544e, 4, 4, format, levels ].forEach( ( value, index ) => header.setUint32( index * 4, value, true ) );
	for ( let level = 0; level < levels; level++ ) bytes.set( block, 20 + level * block.length );
	return bytes;
}

/*
================
colorBlock

Black and white endpoints make the expected interpolation independent of
565 expansion rounding. Reversed endpoints exercise BC1 transparency.
================
*/
function colorBlock( reversed = false ) {
	const block = new Uint8Array( 8 ), view = new DataView( block.buffer );
	view.setUint16( 0, reversed ? 0 : 0xffff, true );
	view.setUint16( 2, reversed ? 0xffff : 0, true );
	view.setUint32( 4, 0xe4e4e4e4, true );
	return block;
}

/*
================
recordingDevice

Record the public GPU operations without imitating the image owner's branch
logic. Failures can be injected at upload to verify partial cleanup.
================
*/
function recordingDevice( context, compressed = true ) {
	const previous = Object.getOwnPropertyDescriptor( globalThis, "GPUTextureUsage" );
	Object.defineProperty( globalThis, "GPUTextureUsage", {
		configurable: true,
		value: { TEXTURE_BINDING: 4, COPY_DST: 2, RENDER_ATTACHMENT: 16 }
	} );
	context.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "GPUTextureUsage", previous );
		else Reflect.deleteProperty( globalThis, "GPUTextureUsage" );
	} );
	const allocations = [], writes = [], errors = [];
	let destroyed = 0, generated = 0, throwUpload = false;
	const texture = { createView: () => ({}), destroy: () => destroyed++ };
	const gpu = {
		features: new Set( compressed ? [ "texture-compression-bc" ] : [] ),
		pushErrorScope() {},
		popErrorScope: async () => null,
		createTexture( descriptor ) {
			allocations.push( descriptor );
			return texture;
		},
		createBindGroup: () => ({}),
		queue: {
			writeTexture( target, bytes, layout, extent ) {
				if ( throwUpload ) throw Error( "injected upload failure" );
				writes.push( { target, bytes: Uint8Array.from( bytes ), layout, extent } );
			},
			copyExternalImageToTexture() {
				throw Error( "native resources must not become bitmaps" );
			}
		}
	};
	const owner = createImages( {
		current: () => /** @type {GPUDevice} */ (/** @type {unknown} */ (gpu)),
		fail: error => errors.push( error ),
		pipeline: () => /** @type {GPURenderPipeline} */ (/** @type {unknown} */ ({ getBindGroupLayout: () => ({}) })),
		sampler: /** @type {GPUSampler} */ ({}),
		generateMips: () => generated++
	} );
	return {
		owner,
		allocations,
		writes,
		errors,
		destroyed: () => destroyed,
		generated: () => generated,
		failUpload: () => {
			throwUpload = true;
		}
	};
}

test("BC1 preserves transparent selectors and crops the final block at small mips", () => {
	const texture = decodeNativeTexture( encodedTexture( DXT1, colorBlock( true ) ) );
	assert.equal( texture.format, "bc1-rgba-unorm" );
	const pixels = decodeNativeTextureLevel( texture, 0 );
	assert.deepEqual( [ ...pixels.slice( 0, 16 ) ], [
		0,
		0,
		0,
		255,
		255,
		255,
		255,
		255,
		127,
		127,
		127,
		255,
		0,
		0,
		0,
		0
	] );
	assert.equal( decodeNativeTextureLevel( texture, 1 ).byteLength, 16 );
	assert.equal( decodeNativeTextureLevel( texture, 2 ).byteLength, 4 );
});

test("BC2 uses explicit four-bit alpha and opaque four-color interpolation", () => {
	const block = new Uint8Array( 16 );
	for ( let byte = 0; byte < 8; byte++ ) block[byte] = byte * 2 | ((byte * 2 + 1) << 4);
	block.set( colorBlock( true ), 8 );
	const pixels = decodeNativeTextureLevel( decodeNativeTexture( encodedTexture( DXT3, block ) ), 0 );
	for ( let pixel = 0; pixel < BLOCK_PIXELS; pixel++ ) assert.equal( pixels[pixel * 4 + 3], pixel * 17 );
	assert.deepEqual( [ ...pixels.slice( 12, 15 ) ], [ 170, 170, 170 ] );
});

test("BC3 decodes both alpha modes and selectors above bit 31", () => {
	for (
		const [first, second, expected] of /** @type {[number, number, number[]][]} */ ([
			[ 255, 0, [ 255, 0, 218, 182, 145, 109, 72, 36 ] ],
			[ 0, 255, [ 0, 255, 51, 102, 153, 204, 0, 255 ] ]
		])
	) {
		const block = new Uint8Array( 16 );
		block[0] = first;
		block[1] = second;
		let selectors = 0n;
		for ( let pixel = 0; pixel < BLOCK_PIXELS; pixel++ ) selectors |= BigInt( pixel % 8 ) << BigInt( pixel * 3 );
		for ( let byte = 0; byte < 6; byte++ ) block[byte + 2] = Number( (selectors >> BigInt( byte * 8 )) & 255n );
		block.set( colorBlock(), 8 );
		const pixels = decodeNativeTextureLevel( decodeNativeTexture( encodedTexture( DXT5, block ) ), 0 );
		for ( let pixel = 0; pixel < BLOCK_PIXELS; pixel++ ) assert.equal( pixels[pixel * 4 + 3], expected[pixel % 8] );
	}
});

test("native admission rejects malformed headers, trailing bytes and incomplete mip chains", () => {
	const bytes = encodedTexture( DXT1, colorBlock() );
	for ( const size of [ 0, 19, bytes.length - 1 ] ) {
		assert.throws( () => decodeNativeTexture( bytes.slice( 0, size ) ) );
	}
	assert.throws( () => decodeNativeTexture( new Uint8Array( [ ...bytes, 0 ] ) ) );
	for ( const [offset, value] of [ [ 0, 0 ], [ 4, 3 ], [ 8, 16384 ], [ 12, 123 ], [ 16, 9 ] ] ) {
		const bad = bytes.slice();
		new DataView( bad.buffer ).setUint32( offset, value, true );
		assert.throws( () => decodeNativeTexture( bad ) );
	}
	const padded = new Uint8Array( bytes.length + 5 );
	padded.set( bytes, 5 );
	assert.equal( decodeNativeTexture( padded.subarray( 5 ) ).format, "bc1-rgba-unorm" );
});

test("all BC formats upload original mip blocks and retire the allocation exactly once", t => {
	const fixture = recordingDevice( t );
	for ( const format of [ DXT1, DXT3, DXT5 ] ) {
		const block = new Uint8Array( format === DXT1 ? 8 : 16 );
		block.set( colorBlock(), format === DXT1 ? 0 : 8 );
		const texture = decodeNativeTexture( encodedTexture( format, block ) );
		const handle = fixture.owner.commands.upload( texture );
		const writes = fixture.writes.slice( -3 );
		assert.equal( fixture.allocations.at( -1 ).format, texture.format );
		for ( const write of writes ) {
			assert.deepEqual( write.bytes, block );
			assert.deepEqual( write.extent, [ 4, 4 ] );
			assert.equal( write.layout.bytesPerRow, block.length );
			assert.equal( write.layout.rowsPerImage, 1 );
		}
		fixture.owner.commands.release( handle );
		fixture.owner.commands.release( handle );
	}
	fixture.owner.dispose();
	assert.equal( fixture.destroyed(), 3 );
	assert.equal( fixture.generated(), 0 );
});

test("BC fallback uploads bounded RGBA mips and leaves the compressed source reusable", t => {
	const fixture = recordingDevice( t, false );
	const texture = decodeNativeTexture( encodedTexture( DXT1, colorBlock( true ) ) );
	const before = texture.levels.map( bytes => bytes.slice() );
	const handle = fixture.owner.commands.upload( texture );
	assert.equal( fixture.allocations[0].format, "rgba8unorm" );
	assert.deepEqual( fixture.writes.map( row => row.bytes.length ), [ 64, 16, 4 ] );
	assert.deepEqual( fixture.writes.map( row => row.extent ), [ [ 4, 4 ], [ 2, 2 ], [ 1, 1 ] ] );
	assert.equal( fixture.writes[0].bytes[15], 0 );
	assert.deepEqual( texture.levels, before );
	fixture.owner.commands.release( handle );
	fixture.owner.commands.upload( texture );
	fixture.owner.dispose();
	assert.equal( fixture.destroyed(), 2 );
	assert.equal( fixture.generated(), 0 );
});

test("single and partial authored chains stay intact on BC and fallback adapters", t => {
	for ( const compressed of [ true, false ] ) {
		const fixture = recordingDevice( t, compressed );
		for ( const count of [ 1, 2 ] ) {
			const source = decodeNativeTexture( encodedTexture( DXT1, colorBlock(), count ) );
			const before = source.levels.map( bytes => bytes.slice() );
			const firstWrite = fixture.writes.length;
			const handle = fixture.owner.commands.upload( source );
			const allocation = fixture.allocations.at( -1 );
			assert.equal( allocation.format, compressed ? "bc1-rgba-unorm" : "rgba8unorm" );
			assert.equal( allocation.mipLevelCount, count );
			assert.equal( allocation.usage & 16, 0, "authored chains need no render attachment" );
			const writes = fixture.writes.slice( firstWrite );
			assert.equal( writes.length, count );
			for ( const [level, write] of writes.entries() ) {
				assert.equal( write.target.mipLevel, level );
				assert.deepEqual(
					write.bytes,
					compressed ? source.levels[level] : decodeNativeTextureLevel( source, level )
				);
			}
			assert.deepEqual( source.levels, before );
			fixture.owner.commands.release( handle );
		}
		assert.equal( fixture.generated(), 0 );
		assert.equal( fixture.destroyed(), 2 );
		fixture.owner.dispose();
	}
});

test("invalid native layers allocate nothing and a failed upload destroys its partial texture", t => {
	const fixture = recordingDevice( t );
	const texture = decodeNativeTexture( encodedTexture( DXT1, colorBlock() ) );
	assert.throws( () => fixture.owner.commands.upload( { ...texture, levels: [ new Uint8Array( 1 ) ] } ) );
	assert.throws( () =>
		fixture.owner.commands.upload( texture, [ texture, /** @type {ImageBitmap} */ ({ width: 4, height: 4 }) ] )
	);
	assert.equal( fixture.allocations.length, 0 );
	fixture.failUpload();
	assert.throws( () => fixture.owner.commands.upload( texture ), /injected upload failure/ );
	assert.equal( fixture.destroyed(), 1 );
	fixture.owner.dispose();
	assert.equal( fixture.destroyed(), 1 );
});
