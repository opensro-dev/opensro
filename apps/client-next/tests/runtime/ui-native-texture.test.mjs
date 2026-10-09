/*
===========================================================================

ui-native-texture.test.mjs - the UI renderer draws native block textures

A native texture (a .texture minimap tile) uploads its first level's blocks
unchanged where the adapter samples BC, decodes to RGBA8 where it does not,
and costs the UI budget what it actually occupies.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createUiResources } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/ui.ts" ).href );

/*
================
fakeDevice

Records each texture's descriptor and each level write.
================
*/
function fakeDevice( bc ) {
	const created = [], writes = [];
	return {
		created,
		writes,
		features: new Set( bc ? [ "texture-compression-bc" ] : [] ),
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
			copyExternalImageToTexture() {}
		}
	};
}

/*
================
bc1

One level of solid red DXT1: color0 = color1 = 0xF800, every index 0.
================
*/
function bc1( width, height ) {
	const blocks = new Uint8Array( (width / 4) * (height / 4) * 8 );
	for ( let offset = 0; offset < blocks.length; offset += 8 ) {
		blocks[offset + 1] = 0xf8;
		blocks[offset + 3] = 0xf8;
	}
	return { kind: "native-texture", width, height, format: "bc1-rgba-unorm", levels: [ blocks ] };
}

/*
================
withUi
================
*/
async function withUi( device, run ) {
	const buffer = globalThis.GPUBufferUsage, usage = globalThis.GPUTextureUsage, stage = globalThis.GPUShaderStage;
	globalThis.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
	globalThis.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	const ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm" );
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

test("an adapter with BC uploads the first level's blocks unchanged", async () => {
	const device = fakeDevice( true );
	await withUi( device, ui => {
		const tile = bc1( 256, 256 );
		ui.texture( "/minimap/1x1.texture", tile );
		const descriptor = device.created.at( -1 );
		assert.equal( descriptor.format, "bc1-rgba-unorm" );
		assert.equal( descriptor.usage & 4, 0, "a block texture is never a render target" );
		const write = device.writes.at( -1 );
		assert.equal( write.data, tile.levels[0] );
		assert.deepEqual( write.layout, { bytesPerRow: 64 * 8, rowsPerImage: 64 } );
		assert.deepEqual( write.size, [ 256, 256 ] );
	} );
});

test("an adapter without BC draws the decoded level as RGBA8", async () => {
	const device = fakeDevice( false );
	await withUi( device, ui => {
		ui.texture( "/minimap/1x1.texture", bc1( 8, 8 ) );
		assert.equal( device.created.at( -1 ).format, "rgba8unorm" );
		const write = device.writes.at( -1 );
		assert.equal( write.data.length, 8 * 8 * 4 );
		assert.deepEqual( [ ...write.data.subarray( 0, 4 ) ], [ 255, 0, 0, 255 ] );
		assert.deepEqual( write.layout, { bytesPerRow: 32, rowsPerImage: 8 } );
	} );
});

test("the UI budget counts the blocks a native texture occupies", async () => {
	// Two 4096x2048 DXT1 tiles hold 8 MiB; as RGBA8 they need 64 MiB, which
	// with the resident 1x1 white texture exceeds the 64 MiB budget.
	await withUi( fakeDevice( true ), ui => {
		ui.texture( "/a.texture", bc1( 4096, 2048 ) );
		ui.texture( "/b.texture", bc1( 4096, 2048 ) );
	} );
	await withUi( fakeDevice( false ), ui => {
		ui.texture( "/a.texture", bc1( 4096, 2048 ) );
		assert.throws( () => ui.texture( "/b.texture", bc1( 4096, 2048 ) ), /residency budget exceeded/ );
	} );
});
