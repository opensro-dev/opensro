/*
===========================================================================

brightness-lifecycle.test.mjs - brightness scene target lifetime (#3908)

Exercises the production owner against retained GPU resources. The
Normal step owns nothing; any other step owns one scene intermediate
and one gamma pass, and a resized, switched-off or disposed owner
retires what it no longer presents.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";
const { createBrightness } = await import( "../../src/engine/runtime/renderer/device/brightness.ts" );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

test("Normal owns nothing, other steps own one intermediate, stale draws throw", () => {
	const gpu = createStrictGpu(), pipelines = [], passes = [], exponents = [];
	const device = {
		...gpu.device,
		createRenderPipeline( descriptor ) {
			pipelines.push( descriptor );
			return gpu.device.createRenderPipeline();
		},
		queue: {
			...gpu.device.queue,
			writeBuffer( buffer, offset, data ) {
				exponents.push( data[0] );
				gpu.device.queue.writeBuffer( buffer );
			}
		}
	};
	const brightness = createBrightness( /** @type {any} */ (device), "bgra8unorm" );
	const output = gpu.device.createTexture( { label: "canvas", size: [ 32, 32 ] } );
	/*
	================
	encode
	================
	*/
	function encode( draw ) {
		const encoder = gpu.device.createCommandEncoder();
		const tracked = {
			...encoder,
			beginRenderPass( descriptor ) {
				passes.push( descriptor );
				return encoder.beginRenderPass( descriptor );
			}
		};
		draw.encode( tracked, output.createView() );
		gpu.device.queue.submit( [ encoder.finish() ] );
	}
	try {
		assert.equal( brightness.prepare( 32, 32, 1 ), undefined );
		assert.equal( pipelines.length, 0, "the Normal step compiles no gamma pass" );
		const bright = brightness.prepare( 32, 32, 1.3 );
		assert.equal( pipelines.length, 1 );
		assert.ok( Math.abs( exponents[0] - 1 / 1.3 ) < 1e-6, "the step's inverse gamma is uploaded" );
		encode( bright );
		assert.equal( passes[0].colorAttachments[0].loadOp, "clear" );
		brightness.prepare( 32, 32, 1.3 );
		assert.equal( exponents.length, 1, "an unchanged gamma uploads nothing" );
		brightness.prepare( 32, 32, 0.85 );
		assert.equal( exponents.length, 2 );
		assert.ok( exponents[1] > 1, "a darker step raises the exponent" );
		const resized = brightness.prepare( 64, 64, 0.85 );
		assert.throws( () => encode( bright ), /Stale brightness target/ );
		encode( resized );
		assert.equal( pipelines.length, 1, "resize rebinds, without recompiling the pass" );
		brightness.prepare( 64, 64, 1 );
		assert.throws( () => encode( resized ), /Stale brightness target/, "Normal retires the intermediate" );
		encode( brightness.prepare( 32, 32, 1.15 ) );
	} finally {
		brightness.dispose();
		output.destroy();
	}
	assert.equal( gpu.live(), 0 );
	assert.throws( () => brightness.prepare( 32, 32, 1.3 ), /Brightness owner disposed/ );
});
