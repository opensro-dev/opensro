/*
===========================================================================

ui-bitmap-upload.test.mjs - a bitmap reaches its UI texture by the route
its adapter takes

#3147: Kepler-generation NVIDIA sessions came up with the font atlas as
rows of dots and large window plates transparent while every
byte-uploaded texture rendered. On those adapters the UI bitmaps upload
from read-back pixels (writeTexture); everywhere else they keep the
external-image copy, and a failed read-back keeps the copy too.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createUiResources } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/ui.ts" ).href
);
const { bitmapPixels } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/pick-alpha.ts" ).href
);
const { adapterRoutesBitmapsViaPixels } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/gpu-facts.ts" ).href
);

/*
================
fakeDevice

Records each writeTexture and each external-image copy separately, so a
test can name the route a bitmap took.
================
*/
function fakeDevice() {
	const created = [], writes = [], copies = [];
	return {
		created,
		writes,
		copies,
		features: new Set(),
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		createBuffer: () => ({ destroy() {} }),
		createSampler: () => ({}),
		createTexture: descriptor => {
			created.push( descriptor );
			return { createView: () => ({}), destroy() {} };
		},
		createBindGroup: () => ({}),
		queue: {
			writeTexture: ( destination, data, layout, size ) => writes.push( { data, layout, size } ),
			writeBuffer() {},
			copyExternalImageToTexture: ( source, destination, size ) => copies.push( { source, size } )
		}
	};
}

/*
================
withUi
================
*/
async function withUi( device, bitmapPixels, run ) {
	const buffer = globalThis.GPUBufferUsage, usage = globalThis.GPUTextureUsage, stage = globalThis.GPUShaderStage;
	globalThis.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
	globalThis.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	const ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm", undefined, bitmapPixels );
	try {
		await ui.ready;
		await run( ui );
	} finally {
		ui.dispose();
		globalThis.GPUBufferUsage = buffer;
		globalThis.GPUTextureUsage = usage;
		globalThis.GPUShaderStage = stage;
	}
}

// The resident 1x1 white texture uploads at construction; the bitmap
// writes are the ones with the bitmap's size.
function bitmapWrite( device ) {
	return device.writes.find( write => write.size[0] === 8 && write.size[1] === 4 );
}

test("a converter's pixels upload by writeTexture and the copy never runs", async () => {
	const device = fakeDevice();
	const data = new Uint8ClampedArray( 8 * 4 * 4 ).fill( 7 );
	await withUi( device, image => ({ data, width: image.width, height: image.height }), ui => {
		ui.texture( "/font/atlas.png", { width: 8, height: 4 } );
		const write = bitmapWrite( device );
		assert.ok( write, "the bitmap wrote its pixels" );
		assert.equal( write.data, data );
		assert.deepEqual( write.layout, { bytesPerRow: 32 } );
		assert.deepEqual( write.size, [ 8, 4 ] );
		assert.equal( device.copies.length, 0, "no external-image copy on the pixel route" );
	} );
});

test("without a converter the bitmap uploads by external-image copy", async () => {
	const device = fakeDevice();
	await withUi( device, undefined, ui => {
		ui.texture( "/font/atlas.png", { width: 8, height: 4 } );
		assert.equal( bitmapWrite( device ), undefined, "no pixel write without a converter" );
		assert.equal( device.copies.length, 1 );
		assert.deepEqual( device.copies[0].size, [ 8, 4 ] );
		assert.equal( device.copies[0].source.flipY, false );
	} );
});

test("a converter that returns null keeps the external-image copy", async () => {
	const device = fakeDevice();
	await withUi( device, () => null, ui => {
		ui.texture( "/font/atlas.png", { width: 8, height: 4 } );
		assert.equal( bitmapWrite( device ), undefined, "a failed read-back writes no pixels" );
		assert.equal( device.copies.length, 1 );
		assert.deepEqual( device.copies[0].size, [ 8, 4 ] );
	} );
});

test("only NVIDIA Kepler adapters take the pixel route", () => {
	const facts = ( vendor, architecture ) => ({
		features: [],
		limits: { maxBufferSize: 0, maxStorageBufferBindingSize: 0, maxTextureDimension2D: 0 },
		vendor,
		architecture
	});
	assert.equal( adapterRoutesBitmapsViaPixels( facts( "nvidia", "kepler" ) ), true );
	assert.equal( adapterRoutesBitmapsViaPixels( facts( "nvidia", "ampere" ) ), false );
	assert.equal( adapterRoutesBitmapsViaPixels( facts( "nvidia", undefined ) ), false );
	assert.equal( adapterRoutesBitmapsViaPixels( facts( "apple", "common-3" ) ), false );
	assert.equal( adapterRoutesBitmapsViaPixels( facts( undefined, undefined ) ), false );
});

/*
================
withCanvas

Stubs OffscreenCanvas for bitmapPixels, which no Node runtime
provides, and restores whatever was there.
================
*/
function withCanvas( canvasClass, run ) {
	const previous = globalThis.OffscreenCanvas;
	globalThis.OffscreenCanvas = canvasClass;
	try {
		run();
	} finally {
		globalThis.OffscreenCanvas = previous;
	}
}

test("bitmapPixels returns what the 2D context read back", () => {
	const sentinel = { data: new Uint8ClampedArray( 4 ), width: 1, height: 1 };
	const drawn = [];
	withCanvas(
		class {
			getContext() {
				return {
					drawImage: ( image, x, y ) => drawn.push( [ image, x, y ] ),
					getImageData: () => sentinel
				};
			}
		},
		() => {
			const image = { width: 1, height: 1 };
			assert.equal( bitmapPixels( image ), sentinel );
			assert.deepEqual( drawn, [ [ image, 0, 0 ] ] );
		}
	);
});

test("bitmapPixels is null when the canvas has no 2D context", () => {
	withCanvas(
		class {
			getContext() {
				return null;
			}
		},
		() => {
			assert.equal( bitmapPixels( { width: 1, height: 1 } ), null );
		}
	);
});
