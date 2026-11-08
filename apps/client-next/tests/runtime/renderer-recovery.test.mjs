/*
===========================================================================

renderer-recovery.test.mjs - a device refused for memory is replaced

The renderer side of device recovery (#2866, split from
lifecycle.test.mjs for the source-size gate): the first device is lost,
and while its replacement builds, Chrome refuses a pipeline allocation
with an OperationError DOMException ("Not enough memory left."). The
renderer must stay in its paced recovery and accept the next device
instead of reporting the session failed. Device-level shapes of the
same refusal live in device-loss.test.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

/*
================
mockGlobal
================
*/
function mockGlobal( t, name, value ) {
	const original = Object.getOwnPropertyDescriptor( globalThis, name );
	Object.defineProperty( globalThis, name, { configurable: true, value } );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, name, original );
		else delete globalThis[name];
	} );
}

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}

const GPU_LIMITS = {
	maxStorageBufferBindingSize: 128 * 1024 * 1024,
	maxBufferSize: 256 * 1024 * 1024,
	maxComputeWorkgroupsPerDimension: 65535
};
const { createRenderer } = await load( "src/engine/runtime/renderer/renderer.ts" );

test("a replacement device refused for memory is retried, not the end of the session", async t => {
	// #2866: the first device is lost, and while its replacement builds,
	// Chrome refuses a pipeline allocation with this DOMException. The
	// renderer must stay in its paced recovery and accept the next
	// device instead of reporting the session failed.
	mockGlobal( t, "GPUShaderStage", { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } );
	mockGlobal( t, "GPUBufferUsage", { UNIFORM: 64, COPY_DST: 8, STORAGE: 128 } );
	mockGlobal( t, "GPUTextureUsage", { TEXTURE_BINDING: 4, COPY_DST: 2, RENDER_ATTACHMENT: 16 } );
	const devices = [];
	const context = {
		configure() {},
		unconfigure() {},
		getCurrentTexture() {
			return {
				createView() {
					return {};
				}
			};
		}
	};
	mockGlobal( t, "navigator", {
		gpu: {
			requestAdapter: async () => ({
				features: new Set(),
				requestDevice: async () => {
					let lose;
					const index = devices.length;
					const device = {
						limits: GPU_LIMITS,
						lost: new Promise( resolve => lose = resolve ),
						addEventListener() {},
						destroyed: false,
						destroy() {
							this.destroyed = true;
						},
						pushErrorScope() {},
						popErrorScope: async () => null,
						createShaderModule() {
							return { getCompilationInfo: async () => ({ messages: [] }) };
						},
						createBindGroupLayout() {
							return {};
						},
						createPipelineLayout() {
							return {};
						},
						createComputePipelineAsync: async () => ({}),
						createRenderPipeline() {
							return {
								getBindGroupLayout() {
									return {};
								}
							};
						},
						async createRenderPipelineAsync() {
							if ( index === 1 ) {
								throw new DOMException( "Not enough memory left.", "OperationError" );
							}
							return {
								getBindGroupLayout() {
									return {};
								}
							};
						},
						createSampler() {
							return {};
						},
						createBuffer() {
							return { destroy() {} };
						},
						createTexture() {
							return {
								createView() {
									return {};
								},
								destroy() {}
							};
						},
						createBindGroup() {
							return {};
						},
						createRenderBundleEncoder() {
							return {
								setPipeline() {},
								setBindGroup() {},
								draw() {},
								finish() {
									return {};
								}
							};
						},
						createCommandEncoder() {
							return {
								beginRenderPass() {
									return {
										end() {},
										setBlendConstant() {},
										executeBundles() {},
										setPipeline() {},
										setBindGroup() {},
										draw() {}
									};
								},
								finish() {
									return {};
								}
							};
						},
						queue: { writeBuffer() {}, writeTexture() {}, submit() {}, copyExternalImageToTexture() {} }
					};
					devices.push( { device, lose } );
					return device;
				}
			}),
			getPreferredCanvasFormat: () => "bgra8unorm"
		}
	} );
	let nowMs = 0;
	mockGlobal( t, "performance", { now: () => nowMs } );
	const settle = () => new Promise( resolve => setImmediate( resolve ) );
	const renderer = createRenderer( { getContext: () => context } );
	await settle();
	renderer.frame( { width: 100, height: 100 } );
	assert.equal( renderer.phase(), "running", renderer.error() ?? undefined );
	// Lose the device; the replacement's build is refused for memory.
	devices[0].lose( { reason: "unknown", message: "fixture loss" } );
	await settle();
	renderer.frame( { width: 100, height: 100 } );
	nowMs += 30000;
	renderer.frame( { width: 100, height: 100 } );
	await settle();
	await settle();
	assert.equal( devices.length, 2, "the replacement device was attempted" );
	assert.equal( renderer.phase(), "starting", "the refusal is retried, not terminal" );
	// The next device builds clean and the session runs on: one frame
	// stamps the refusal, the clock crosses the wait, the next replaces.
	renderer.frame( { width: 100, height: 100 } );
	nowMs += 30000;
	renderer.frame( { width: 100, height: 100 } );
	await settle();
	renderer.frame( { width: 100, height: 100 } );
	assert.equal( devices.length, 3 );
	assert.equal( renderer.phase(), "running", renderer.error() ?? undefined );
	renderer.dispose();
});
