/*
===========================================================================

bloom-lifecycle.test.mjs - opt-in float resources and native handback

Observe GPU allocation descriptors through the existing strict device fake.
Native draws must never allocate the experimental shader or blur targets.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_SHADER_STAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";
const { createBloom } = await import( "../../src/engine/runtime/renderer/device/bloom.ts" );
globalThis.GPUShaderStage = GPU_SHADER_STAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

test("native bloom skips float allocations and toggles reuse compiled pipelines", async () => {
	const gpu = createStrictGpu();
	/** @type {any} The strict fake implements the GPU operations this owner uses. */
	const device = gpu.device;
	const modules = [], pipelines = [], textures = [], retired = [];
	const originalModule = device.createShaderModule.bind( device );
	const originalPipeline = device.createRenderPipeline.bind( device );
	const originalTexture = device.createTexture.bind( device );
	device.createShaderModule = descriptor => {
		modules.push( descriptor.label );
		return originalModule( descriptor );
	};
	device.createRenderPipeline = descriptor => {
		pipelines.push( descriptor.label );
		return originalPipeline( descriptor );
	};
	device.createTexture = descriptor => {
		textures.push( descriptor.format );
		return originalTexture( descriptor );
	};
	const bloom = createBloom( device, "bgra8unorm", resource => retired.push( resource ) );
	try {
		await bloom.ready;
		bloom.prepare( 64, 64, false, true );
		const native = bloom.prepare( 64, 64, true, false );
		assert.ok( native );
		assert.deepEqual( modules, [ "native-bloom" ] );
		assert.equal( pipelines.length, 5 );
		assert.deepEqual( textures, [ "bgra8unorm", "bgra8unorm", "bgra8unorm" ] );
		const float = bloom.prepare( 64, 64, true, true );
		assert.ok( float );
		assert.deepEqual( modules, [ "native-bloom", "float-bloom" ] );
		assert.equal( pipelines.filter( label => label.startsWith( "float-bloom-" ) ).length, 8 );
		assert.deepEqual( textures.slice( 3 ), [ "bgra8unorm", ...Array( 4 ).fill( "rgba16float" ) ] );
		assert.equal( retired.length, 3 );
		assert.throws( () => native.encode( device.createCommandEncoder(), native.view ), /Stale bloom/ );
		bloom.prepare( 64, 64, true, true );
		assert.equal( textures.length, 8, "Same size and mode must reuse targets" );
		bloom.prepare( 96, 96, true, true );
		assert.equal( pipelines.length, 13, "Resize must reuse float pipelines" );
		assert.equal( retired.length, 8 );
		assert.throws( () => float.encode( device.createCommandEncoder(), float.view ), /Stale bloom/ );
		bloom.prepare( 96, 96, true, false );
		assert.deepEqual( textures.slice( -3 ), Array( 3 ).fill( "bgra8unorm" ) );
		bloom.prepare( 96, 96, true, true );
		assert.equal( modules.length, 2, "Re-enabling must reuse the compiled float shader" );
		assert.equal( pipelines.length, 13 );
		bloom.prepare( 96, 96, false, false );
		assert.equal( retired.length, textures.length, "Disabling retires every allocated target" );
	} finally {
		bloom.dispose();
	}
});
