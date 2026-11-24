/*
===========================================================================

brightness-lifecycle.test.mjs - brightness scene target lifetime (#3908)

Exercises the production owner against retained GPU resources. The
Normal step owns nothing; any other step owns one scene intermediate
and one gamma pass, and a resized, switched-off or disposed owner
retires what it no longer presents. A recording encoder then pins the
frame's composition: the ramp is a display ramp, so with a step chosen
the labels and the UI compose into the intermediate and the gamma pass
is the frame's final colour pass.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";
const { createBrightness } = await import( "../../src/engine/runtime/renderer/device/brightness.ts" );
const { createFrame } = await import( "../../src/engine/runtime/renderer/frame/frame.ts" );
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

/*
================
recordingCommands

Frame commands over a recording encoder: one trace row per render pass,
with the colour view it wrote.
================
*/
function recordingCommands() {
	const passes = [];
	const bundleEncoder = () => ({
		setPipeline() {},
		setBindGroup() {},
		setVertexBuffer() {},
		setIndexBuffer() {},
		draw() {},
		drawIndexed() {},
		finish: () => ({ bundle: true })
	});
	const encoder = {
		beginRenderPass( descriptor ) {
			const row = { label: descriptor.label, color: descriptor.colorAttachments?.[0]?.view, bundles: 0 };
			passes.push( row );
			return {
				setBlendConstant() {},
				setPipeline() {},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				draw() {},
				drawIndexed() {},
				executeBundles( bundles ) {
					row.bundles += bundles.length;
				},
				end() {}
			};
		},
		beginComputePass: () => ({
			setPipeline() {},
			setBindGroup() {},
			dispatchWorkgroups() {},
			end() {}
		}),
		finish: () => ({})
	};
	return { passes, commands: { createBundleEncoder: bundleEncoder, createEncoder: () => encoder, submit() {} } };
}

/** @type {any} */
const WORLD_LABELS = { layer: "world", count: 1, first: 0, pipeline: {}, binding: {} };
/** @type {any} */
const UI_DRAW = { count: 1, first: 0, pipeline: {}, binding: {} };

/*
================
drawComposedFrame

One frame with world labels and UI, no scene content, under the given
experimental stages.
================
*/
function drawComposedFrame( commands, experimental ) {
	return createFrame( /** @type {any} */ (commands) ).draw(
		/** @type {any} */ ("view"),
		undefined,
		undefined,
		/** @type {any} */ ("scene-depth"),
		[],
		[ WORLD_LABELS, UI_DRAW ],
		[],
		undefined,
		undefined,
		undefined,
		undefined,
		[],
		1,
		undefined,
		undefined,
		undefined,
		experimental
	);
}

test("the gamma pass is the frame's final colour pass, after the labels and the UI", () => {
	const { passes, commands } = recordingCommands();
	drawComposedFrame( commands, {
		brightness: {
			view: "brightness-view",
			encode( _encoder, output ) {
				passes.push( { label: "brightness-gamma", color: output, bundles: 0 } );
			}
		}
	} );
	assert.deepEqual(
		passes.map( pass => pass.label ),
		[ "main-pass", "hud-after-flares", "brightness-gamma" ],
		"the scene resolve, then the labels and the UI, then the gamma pass"
	);
	assert.equal( passes[0].color, "brightness-view", "the scene renders into the intermediate" );
	assert.equal( passes[1].color, "brightness-view", "the labels and the UI compose into the intermediate" );
	assert.equal( passes[1].bundles, 2, "the world labels and the UI both draw in the HUD pass" );
	assert.equal( passes[2].color, "view", "the gamma pass writes the presented frame" );
});

test("at Normal the frame composes straight into the presented view, unchanged", () => {
	const { passes, commands } = recordingCommands();
	drawComposedFrame( commands, undefined );
	assert.deepEqual( passes.map( pass => pass.label ), [ "main-pass" ], "no intermediate, no gamma pass" );
	assert.equal( passes[0].color, "view" );
	assert.equal( passes[0].bundles, 2, "the labels and the UI draw in the main pass" );
});
