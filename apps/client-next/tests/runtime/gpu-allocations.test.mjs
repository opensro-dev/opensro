/*
===========================================================================

gpu-allocations.test.mjs - the page's standing GPU allocation totals

Drives gpu-allocations.ts three ways: the counters themselves (a
create joins, a destroy leaves, the floor holds), the texture byte
estimate (formats, mip chains, arrays, 3-D, the compressed families),
and the device instrumentation (a fake device: wrapped creations are
counted, a double destroy returns bytes once, a resource that
refuses wrapping is left alone and the renderer's object untouched).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGpuAllocations, instrumentGpuAllocations, textureAllocationBytes } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/gpu-allocations.ts" ).href
);

test("creations join the standing total and destroys return their bytes", () => {
	const totals = createGpuAllocations();
	totals.noteBuffer( 100 );
	totals.noteBuffer( 250 );
	totals.noteTexture( 4096 );
	assert.deepEqual( totals.read(), { buffers: 2, bufferBytes: 350, textures: 1, textureBytes: 4096 } );
	totals.dropBuffer( 100 );
	assert.deepEqual( totals.read(), { buffers: 1, bufferBytes: 250, textures: 1, textureBytes: 4096 } );
	totals.dropBuffer( 9999 );
	totals.dropTexture( 9999 );
	assert.deepEqual(
		totals.read(),
		{ buffers: 0, bufferBytes: 0, textures: 0, textureBytes: 0 },
		"drops floor at zero: a lost device's late destroys never go negative"
	);
});

test("the texture estimate prices formats, mips, arrays and depth", () => {
	// rgba8 at 256x256: the full mip chain sums to 4/3 of the base level.
	assert.equal(
		textureAllocationBytes( { size: { width: 256, height: 256 }, format: "rgba8unorm", mipLevelCount: 9 } ),
		349_524
	);
	// One level only, as a render target asks.
	assert.equal(
		textureAllocationBytes( { size: { width: 64, height: 64 }, format: "rgba16float" } ),
		32_768
	);
	// An array holds its layers at every level.
	assert.equal(
		textureAllocationBytes( {
			size: { width: 16, height: 16, depthOrArrayLayers: 6 },
			format: "rgba8unorm",
			mipLevelCount: 1
		} ),
		6_144
	);
	// A 3-D texture halves its depth with the level: 8^3 + 4^3 + 2^3 + 1.
	assert.equal(
		textureAllocationBytes( {
			size: { width: 8, height: 8, depthOrArrayLayers: 8 },
			format: "r8unorm",
			mipLevelCount: 4,
			dimension: "3d"
		} ),
		585
	);
	// Compressed blocks round up: bc7 at 10x10 is 3x3 blocks of 16 bytes.
	assert.equal(
		textureAllocationBytes( { size: { width: 10, height: 10 }, format: "bc7-rgba-unorm" } ),
		144
	);
	// ASTC takes its block shape from the name: 12x12 texels a block.
	assert.equal(
		textureAllocationBytes( { size: { width: 24, height: 24 }, format: "astc-12x12-unorm-srgb" } ),
		64
	);
	// The array form of size reads the same as the dict form.
	assert.equal(
		textureAllocationBytes( { size: [ 64, 64, 1 ], format: "rgba16float" } ),
		32_768
	);
});

/*
================
fakeDevice

A GPUDevice-shaped fake: creations hand out plain objects whose
destroy counts its calls.
================
*/
function fakeDevice() {
	const made = { buffers: [], textures: [] };
	return {
		made,
		device: {
			createBuffer( descriptor ) {
				let destroys = 0;
				const buffer = {
					size: descriptor.size,
					get destroys() {
						return destroys;
					},
					destroy() {
						destroys++;
					}
				};
				made.buffers.push( buffer );
				return descriptor.frozen ? Object.freeze( buffer ) : buffer;
			},
			createTexture( descriptor ) {
				const texture = {
					descriptor,
					destroys: 0,
					destroy() {
						this.destroys++;
					}
				};
				made.textures.push( texture );
				return texture;
			}
		}
	};
}

test("an instrumented device counts what it creates and returns", () => {
	const totals = createGpuAllocations();
	const { device, made } = fakeDevice();
	instrumentGpuAllocations( device, totals );
	const buffer = device.createBuffer( { size: 1024 } );
	const texture = device.createTexture( { size: { width: 64, height: 64 }, format: "rgba8unorm" } );
	assert.deepEqual( totals.read(), { buffers: 1, bufferBytes: 1024, textures: 1, textureBytes: 16_384 } );
	buffer.destroy();
	buffer.destroy();
	assert.equal( made.buffers[0].destroys, 2, "the resource's own destroy still runs every call" );
	assert.deepEqual(
		totals.read(),
		{ buffers: 0, bufferBytes: 0, textures: 1, textureBytes: 16_384 },
		"a double destroy returns the bytes once"
	);
	texture.destroy();
	assert.deepEqual( totals.read(), { buffers: 0, bufferBytes: 0, textures: 0, textureBytes: 0 } );
});

test("a resource that refuses wrapping is created uncounted", () => {
	const totals = createGpuAllocations();
	const { device, made } = fakeDevice();
	instrumentGpuAllocations( device, totals );
	const buffer = device.createBuffer( { size: 5, frozen: true } );
	assert.equal( buffer, made.buffers[0], "creation still returns the resource" );
	assert.deepEqual(
		totals.read(),
		{ buffers: 0, bufferBytes: 0, textures: 0, textureBytes: 0 },
		"an unwrappable resource is left out rather than miscounted"
	);
	buffer.destroy();
	assert.equal( made.buffers[0].destroys, 1, "its destroy was never wrapped" );
});

test("two device generations count into the same totals", () => {
	const totals = createGpuAllocations();
	const first = fakeDevice(), second = fakeDevice();
	instrumentGpuAllocations( first.device, totals );
	instrumentGpuAllocations( second.device, totals );
	first.device.createBuffer( { size: 10 } );
	second.device.createBuffer( { size: 20 } );
	assert.equal( totals.read().bufferBytes, 30, "a recreated device keeps counting into the same totals" );
});
