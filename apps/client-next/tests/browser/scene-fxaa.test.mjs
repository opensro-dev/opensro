/*
===========================================================================

scene-fxaa.test.mjs - the scene FXAA resolve on a real device

The Experimental FXAA row resolves the scene into the frame through the
presentation owner's encodeUpscale with antialias on (device/finish.ts).
This drives that owner alone under a validation scope: a hard diagonal
rendered into a scene texture resolves into a frame texture of the same
size. The plain resolve samples texel centres, so it keeps every edge
texel hard; the FXAA resolve blends them. Both read back exactly.

The second proof drives the renderer through the hdr-stages fixture: with
the row on, every frame resolves its scene through the scene-fxaa pass,
turning it off restores the native pixels exactly, and no uncaptured GPU
error occurs in either mode.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

import { captureLightingStages } from "../helpers/hdr-gpu.mjs";

/*
================
fxaaProbe

Resolve one hard diagonal twice, plain then FXAA. Returns the validation
error, if any, and the intermediate (blended) texel count of each.
================
*/
async function fxaaProbe() {
	const SIZE = 64, FORMAT = "rgba8unorm";
	const { createFinish } = await import( "/src/engine/runtime/renderer/device/finish.ts" );
	const adapter = await navigator.gpu.requestAdapter();
	if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
	const device = await adapter.requestDevice();
	device.pushErrorScope( "validation" );
	const finish = createFinish( device, FORMAT );
	await finish.ready;
	const texture = () =>
		device.createTexture( {
			size: [ SIZE, SIZE ],
			format: FORMAT,
			usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_SRC
		} );
	const scene = texture(), frame = texture();
	const module = device.createShaderModule( {
		code: `
		@vertex fn vs(@builtin(vertex_index) i:u32)->@builtin(position) vec4f {
		 let p=array<vec2f,3>(vec2f(-1,1),vec2f(-1,-1),vec2f(1,-0.4))[i];
		 return vec4f(p,0,1);
		}
		@fragment fn fs()->@location(0) vec4f { return vec4f(0.8,0.8,0.8,1); }
		`
	} );
	const pipeline = device.createRenderPipeline( {
		layout: "auto",
		vertex: { module, entryPoint: "vs" },
		fragment: { module, entryPoint: "fs", targets: [ { format: FORMAT } ] },
		primitive: { topology: "triangle-list" }
	} );
	const blended = {};
	for ( const antialias of [ false, true ] ) {
		const encoder = device.createCommandEncoder();
		const pass = encoder.beginRenderPass( {
			colorAttachments: [ {
				view: scene.createView(),
				clearValue: { r: 0.1, g: 0.1, b: 0.1, a: 1 },
				loadOp: "clear",
				storeOp: "store"
			} ]
		} );
		pass.setPipeline( pipeline );
		pass.draw( 3 );
		pass.end();
		finish.encodeUpscale( encoder, scene.createView(), frame.createView(), antialias );
		const read = device.createBuffer( {
			size: SIZE * SIZE * 4,
			usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
		} );
		encoder.copyTextureToBuffer( { texture: frame }, { buffer: read, bytesPerRow: SIZE * 4 }, [ SIZE, SIZE ] );
		device.queue.submit( [ encoder.finish() ] );
		await read.mapAsync( GPUMapMode.READ );
		const data = new Uint8Array( read.getMappedRange() );
		let soft = 0;
		// The two tones sit at 26 and 204: anything well between is a blend.
		for ( let i = 0; i < data.length; i += 4 ) if ( data[i] > 40 && data[i] < 190 ) soft++;
		blended[String( antialias )] = soft;
		read.unmap();
	}
	const error = await device.popErrorScope();
	finish.dispose();
	return { error: error ? error.message : null, blended };
}

test( "the scene FXAA resolve blends a hard edge the plain resolve keeps", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><body></body>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		const { error, blended } = await page.evaluate( fxaaProbe );
		assert.equal( error, null, "No validation error" );
		assert.equal( blended.false, 0, "The plain same-size resolve keeps every edge texel hard" );
		assert.ok( blended.true > 16, `FXAA must blend the diagonal's edge texels (${blended.true})` );
	} finally {
		await browser.close();
	}
} );

/*
================
tracePasses

Record every render pass label and every uncaptured GPU error on the page.
================
*/
function tracePasses() {
	const trace = /** @type {any} */ (globalThis);
	trace.__passes = [];
	trace.__gpuErrors = [];
	const request = GPUAdapter.prototype.requestDevice;
	GPUAdapter.prototype.requestDevice = async function( descriptor ) {
		const device = await request.call( this, descriptor );
		device.addEventListener?.(
			"uncapturederror",
			event => trace.__gpuErrors.push( String( event.error?.message ?? event.error ) )
		);
		return device;
	};
	const begin = GPUCommandEncoder.prototype.beginRenderPass;
	GPUCommandEncoder.prototype.beginRenderPass = function( descriptor ) {
		trace.__passes.push( descriptor.label ?? "?" );
		return begin.call( this, descriptor );
	};
}

test( "the FXAA row resolves the scene through scene-fxaa and off restores native pixels", {
	timeout: 120000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", error => errors.push( error.message ) );
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><body></body>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		await page.evaluate( tracePasses );
		const capture = /** @type {any} */ (captureLightingStages);
		const rows = await page.evaluate( capture, { stages: [ "off", "sceneFxaa", "off" ], boxYaw: Math.PI / 7 } );
		const passes = await page.evaluate( () => (/** @type {any} */ (globalThis)).__passes );
		const gpuErrors = await page.evaluate( () => (/** @type {any} */ (globalThis)).__gpuErrors );
		const resolves = passes.filter( label => label === "scene-fxaa" ).length;
		assert.ok( resolves >= 5, `Every row-on frame resolves through scene-fxaa (${resolves})` );
		assert.deepEqual( rows[0].rgba, rows[2].rgba, "The row off must restore exact native pixels" );
		assert.deepEqual( gpuErrors, [], "No uncaptured GPU errors in any mode" );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
