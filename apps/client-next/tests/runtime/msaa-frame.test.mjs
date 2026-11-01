/*
===========================================================================

msaa-frame.test.mjs - the frame owner's pass order under the MSAA stage

A recording encoder stands in for the GPU, so these tests read the passes
the frame asks for. The 4x depth resolves into the scene's single-sampled
depth even when the render-scale stage owns a separate frame depth, and the
deferred tail leaves the world labels to the one pass that draws them after
the depth resolve.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createFrame } = await import( "../../src/engine/runtime/renderer/frame/frame.ts" );

const WORLD_LABELS = { layer: "world", count: 1, first: 0, pipeline: {}, binding: {} };

/*
================
recordingCommands

Frame commands over a recording encoder: one trace row per render pass, and
the bundles each pass executed.
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
			const row = { label: descriptor.label, depth: descriptor.depthStencilAttachment?.view, bundles: 0 };
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
		finish: () => ({})
	};
	return { passes, commands: { createBundleEncoder: bundleEncoder, createEncoder: () => encoder, submit() {} } };
}

/*
================
msaaExperimental

The MSAA stage's frame input; its depth resolve lands in the trace.
================
*/
function msaaExperimental( passes, extra = {} ) {
	return {
		...extra,
		msaa: {
			view: "msaa-colour",
			depth: "msaa-depth",
			resolveDepth( _encoder, target ) {
				passes.push( { label: "msaa-resolve", depth: target, bundles: 0 } );
			}
		}
	};
}

/*
================
drawFrame

One frame with the world labels, no scene content and the given inputs.
================
*/
function drawFrame( commands, { deferred, experimental } ) {
	return createFrame( commands ).draw(
		"view",
		undefined,
		undefined,
		"scene-depth",
		[],
		[ WORLD_LABELS ],
		[],
		undefined,
		undefined,
		undefined,
		undefined,
		[],
		1,
		deferred,
		undefined,
		undefined,
		experimental
	);
}

test("the 4x depth resolves into the scene depth under the render-scale stage", () => {
	const { passes, commands } = recordingCommands();
	const upscales = [];
	drawFrame( commands, {
		experimental: msaaExperimental( passes, {
			sceneScale: {
				view: "scene-colour",
				frameDepth: "frame-depth",
				resolve() {},
				resolveDepth() {
					upscales.push( passes.at( -1 ).label );
				}
			}
		} )
	} );
	const resolve = passes.find( pass => pass.label === "msaa-resolve" ) ?? assert.fail( "no depth resolve" );
	assert.equal( resolve.depth, "scene-depth", "the upscale reads the scene depth, so that is what must hold it" );
	assert.deepEqual( upscales, [ "msaa-resolve" ], "the upscale runs after the resolve" );
	assert.equal( passes.find( pass => pass.label === "hdr-world-ui" ).depth, "frame-depth" );
});

test("the deferred tail leaves the labels to the pass after it", () => {
	const { passes, commands } = recordingCommands();
	drawFrame( commands, {
		deferred: { asynchronous: false, prepare: () => [] },
		experimental: msaaExperimental( passes )
	} );
	const labels = passes.filter( pass => pass.bundles > 0 ).map( pass => pass.label );
	assert.deepEqual( labels, [ "hdr-world-ui" ], "the world labels draw exactly once" );
	assert.equal( passes.find( pass => pass.label === "deferred-particles" ).depth, "scene-depth" );
});
