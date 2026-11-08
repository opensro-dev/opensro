/*
===========================================================================

ambient-occlusion-lifecycle.test.mjs - ambient occlusion target lifetime

Exercises the production owner (experimental, #2094) against the strict
GPU fake: off owns nothing and compiles nothing, on totals the frame's
depth at half resolution behind a stale-target guard, and only an
enable, disable or resize replaces the blurred view the surfaces
sample (geometry binding 15). A target with no measurement yet - just
enabled, resized or re-enabled - names no view, so the surfaces
sample white (factor 1), never a fresh texture's zero.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";
const { createAmbientOcclusion } = await import(
	"../../src/engine/runtime/renderer/device/ambient-occlusion.ts"
);
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

test("off owns nothing; on totals depth at half size and stales superseded draws", () => {
	const gpu = createStrictGpu(), pipelines = [], passes = [], textures = [];
	const device = {
		...gpu.device,
		createRenderPipeline( descriptor ) {
			pipelines.push( descriptor );
			return gpu.device.createRenderPipeline();
		},
		createTexture( descriptor ) {
			textures.push( descriptor );
			return gpu.device.createTexture( descriptor );
		}
	};
	const occlusion = createAmbientOcclusion( /** @type {any} */ (device) );
	const camera = /** @type {import("../../src/engine/contracts/scene.ts").WorldCamera} */ ({
		eye: [ 0, 0, 0 ],
		target: [ 0, 0, 1 ],
		fov: 1,
		near: 0.5,
		far: 1000,
		lockHorizontalFov: false
	});
	const depth = gpu.device.createTexture( { label: "scene-depth", size: [ 64, 64 ] } );
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
		draw.encode( tracked, depth.createView() );
		gpu.device.queue.submit( [ encoder.finish() ] );
	}
	try {
		assert.equal( occlusion.prepare( 64, 64, false, camera ), undefined );
		assert.equal( pipelines.length, 0, "disabled occlusion compiles neither pass" );
		assert.equal( textures.length, 0, "disabled occlusion owns no targets" );

		const first = occlusion.prepare( 64, 64, true, camera );
		assert.ok( first, "the enabled stage answers a draw" );
		assert.equal(
			first.view,
			null,
			"before the first encode the surfaces sample white, not a fresh target's zero"
		);
		assert.equal( pipelines.length, 2, "the totals and blur pipelines compile once" );
		assert.equal( textures.length, 2 );
		assert.deepEqual( textures[0].size, [ 32, 32 ], "the totals target is half resolution" );
		assert.equal( textures[0].format, "r8unorm" );
		assert.deepEqual( textures[1].size, [ 32, 32 ], "the blurred target is half resolution" );
		encode( first );
		assert.deepEqual(
			passes.map( pass => pass.label ),
			[ "ambient-occlusion", "ambient-occlusion-blur" ],
			"each frame totals the depth, then blurs once"
		);
		assert.equal( passes[0].colorAttachments[0].loadOp, "clear" );
		assert.equal( passes[0].colorAttachments[0].clearValue.r, 1, "unread depth stays unoccluded" );

		const again = occlusion.prepare( 64, 64, true, camera );
		assert.ok( again );
		assert.ok( again.view, "after the first encode the surfaces sample the blurred view" );
		const settled = occlusion.prepare( 64, 64, true, camera );
		assert.equal( settled?.view, again.view, "the sampled view is stable across frames at one size" );
		encode( again );

		const resized = occlusion.prepare( 128, 128, true, camera );
		assert.ok( resized );
		assert.equal( resized.view, null, "a resized target has no measurement yet: white until its first encode" );
		assert.equal( pipelines.length, 2, "a resize rebinds without recompiling" );
		assert.throws( () => encode( first ), /Stale ambient occlusion target/ );
		assert.equal( gpu.live(), 4, "depth, two new targets and the uniforms survive a resize" );
		encode( resized );

		const regrown = occlusion.prepare( 128, 128, true, camera );
		assert.ok( regrown?.view, "after the resized target's first encode the surfaces sample it" );
		assert.notEqual( regrown.view, again.view, "the resized view replaces the old one once it is measured" );

		assert.equal( occlusion.prepare( 128, 128, false, camera ), undefined );
		assert.throws( () => encode( resized ), /Stale ambient occlusion target/ );
		assert.equal( gpu.live(), 2, "disabling releases both targets" );

		const third = occlusion.prepare( 64, 64, true, camera );
		assert.ok( third );
		assert.equal( third.view, null, "re-enabled targets start unmeasured: white again" );
		encode( third );
		const measured = occlusion.prepare( 64, 64, true, camera );
		assert.ok( measured?.view, "the re-enabled target is sampled once it is measured" );
	} finally {
		occlusion.dispose();
		depth.destroy();
	}
	assert.equal( gpu.live(), 0, "dispose releases the uniforms and every target" );
});
