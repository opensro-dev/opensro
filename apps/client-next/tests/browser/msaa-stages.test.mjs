/*
===========================================================================

msaa-stages.test.mjs - the 4x multisampled scene in isolation

Two proofs. The first pins the environment's own capability: a raw
WebGPU diagonal rendered single-sampled holds hard 0/255 edges, and the
same diagonal through a 4x multisampled attachment with a resolveTarget
blends them - the mechanism the row rides on. The second pins the
renderer's wiring through the hdr-stages fixture: with the row on, the
frame's main pass renders into the 4x scene colour and resolves into the
canvas texture (the attachments are traced by name), the 4x depth
resolves into the frame's single-sampled depth, and turning the row off
again restores the byte-exact native pass order with no uncaptured GPU
errors anywhere.

The pixel-softening assertion is deliberately absent from the renderer
proof: a headless canvas capture can composite one presentation behind,
which would flake it; the raw probe above is the deterministic witness
that a 4x resolve softens edges on this very page and device.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

import { captureLightingStages } from "../helpers/hdr-gpu.mjs";

/*
================
diagonalProbe

One hard diagonal triangle, captured twice: single-sampled, then through
a 4x multisampled attachment resolved into the canvas texture. Returns
the intermediate (blended) texel count of each pass.
================
*/
async function diagonalProbe() {
	const adapter = await navigator.gpu.requestAdapter();
	if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
	const device = await adapter.requestDevice();
	const width = 96, height = 96;
	const canvas = document.createElement( "canvas" );
	canvas.width = width;
	canvas.height = height;
	const context = canvas.getContext( "webgpu" );
	if ( !context ) throw Error( "WebGPU canvas unavailable" );
	const format = navigator.gpu.getPreferredCanvasFormat();
	context.configure( { device, format, alphaMode: "opaque" } );
	const module = device.createShaderModule( {
		code: `
		@vertex fn vs(@builtin(vertex_index) i:u32)->@builtin(position) vec4f {
		 let p=array<vec2f,3>(vec2f(-1,1),vec2f(-1,-1),vec2f(1,-1))[i];
		 return vec4f(p,0,1);
		}
		@fragment fn fs()->@location(0) vec4f { return vec4f(1,1,1,1); }
		`
	} );
	const output = document.createElement( "canvas" );
	output.width = width;
	output.height = height;
	const capture = output.getContext( "2d", { willReadFrequently: true } );
	if ( !capture ) throw Error( "No capture context" );
	const blended = {};
	for ( const count of [ 1, 4 ] ) {
		const pipeline = device.createRenderPipeline( {
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "fs", targets: [ { format } ] },
			primitive: { topology: "triangle-list" },
			multisample: { count }
		} );
		const target = context.getCurrentTexture().createView();
		const baseAttachment = {
			view: target,
			clearValue: { r: 0, g: 0, b: 0, a: 1 },
			loadOp: "clear",
			storeOp: "store"
		};
		let attachment = /** @type {GPURenderPassColorAttachment} */ (baseAttachment);
		if ( count === 4 ) {
			const msaa = device.createTexture( {
				size: [ width, height ],
				sampleCount: 4,
				format,
				usage: GPUTextureUsage.RENDER_ATTACHMENT
			} ).createView();
			attachment = {
				view: msaa,
				resolveTarget: target,
				clearValue: { r: 0, g: 0, b: 0, a: 1 },
				loadOp: "clear",
				storeOp: "discard"
			};
		}
		const encoder = device.createCommandEncoder();
		const pass = encoder.beginRenderPass( { colorAttachments: [ attachment ] } );
		pass.setPipeline( pipeline );
		pass.draw( 3 );
		pass.end();
		device.queue.submit( [ encoder.finish() ] );
		await new Promise( requestAnimationFrame );
		const image = await createImageBitmap( canvas );
		capture.drawImage( image, 0, 0 );
		image.close();
		const data = capture.getImageData( 0, 0, width, height ).data;
		let soft = 0;
		for ( let i = 0; i < data.length; i += 4 ) if ( data[i] > 40 && data[i] < 215 ) soft++;
		blended[count] = soft;
	}
	return blended;
}

test(
	"the device resolves a 4x multisampled diagonal into blended edge texels",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.route(
				CLIENT_NEXT_BASE_URL + "/",
				route => route.fulfill( { contentType: "text/html", body: "<!doctype html><body></body>" } )
			);
			await page.goto( CLIENT_NEXT_BASE_URL );
			const blended = await page.evaluate( diagonalProbe );
			assert.equal( blended[1], 0, "A single-sampled diagonal holds hard edges" );
			assert.ok( blended[4] > 16, `A 4x resolve must blend the diagonal's edge texels (${blended[4]})` );
		} finally {
			await browser.close();
		}
	}
);

test(
	"the MSAA row renders the scene through the 4x buffers and off restores the native pass",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		try {
			await page.route(
				CLIENT_NEXT_BASE_URL + "/",
				route => route.fulfill( { contentType: "text/html", body: "<!doctype html><body></body>" } )
			);
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.evaluate( () => {
				const trace = /** @type {any} */ (globalThis);
				trace.__gpuErrors = [];
				const origRequest = GPUAdapter.prototype.requestDevice;
				GPUAdapter.prototype.requestDevice = async function( descriptor ) {
					const device = await origRequest.call( this, descriptor );
					device.addEventListener?.(
						"uncapturederror",
						event => trace.__gpuErrors.push( String( event.error?.message ?? event.error ) )
					);
					return device;
				};
				trace.__viewNames = new Map();
				trace.__passes = [];
				trace.__textures = [];
				const origTexture = GPUDevice.prototype.createTexture;
				GPUDevice.prototype.createTexture = function( descriptor ) {
					const texture = origTexture.call( this, descriptor );
					if ( descriptor.sampleCount === 4 ) trace.__textures.push( descriptor.label ?? "?" );
					const origView = texture.createView.bind( texture );
					texture.createView = viewDescriptor => {
						const view = origView( viewDescriptor ?? {} );
						trace.__viewNames.set( view, descriptor.label ?? "texture" );
						return view;
					};
					return texture;
				};
				const origPass = GPUCommandEncoder.prototype.beginRenderPass;
				GPUCommandEncoder.prototype.beginRenderPass = function( descriptor ) {
					const colour = descriptor.colorAttachments?.[0];
					const name = view => trace.__viewNames.get( view ) ?? "?";
					trace.__passes.push(
						(descriptor.label ?? "?") +
							" c=" + (colour ? name( colour.view ) : "-") +
							(colour?.resolveTarget ? "+R->" + name( colour.resolveTarget ) : "") +
							(descriptor.depthStencilAttachment ?
								" d=" + name( descriptor.depthStencilAttachment.view ) :
								"")
					);
					return origPass.call( this, descriptor );
				};
			} );
			const capture = /** @type {any} */ (captureLightingStages);
			const rows = await page.evaluate( capture, {
				stages: [ "off", "msaa", "off" ],
				boxYaw: Math.PI / 7
			} );
			const textures = await page.evaluate( () => (/** @type {any} */ (globalThis)).__textures );
			const passes = await page.evaluate( () => (/** @type {any} */ (globalThis)).__passes );
			const gpuErrors = await page.evaluate( () => (/** @type {any} */ (globalThis)).__gpuErrors );
			// The 4x scene buffers exist exactly once, in the row's frames.
			assert.deepEqual(
				textures.filter( label => label.startsWith( "msaa-" ) ).sort(),
				[ "msaa-scene-colour", "msaa-scene-depth" ],
				"The row owns one 4x colour and one 4x depth buffer"
			);
			// The main pass rendered into the 4x colour, resolved into an
			// unnamed canvas texture, and the 4x depth resolved into the
			// surface's single-sampled depth - the frame's own attachments.
			const msaaMain = passes.filter( line => line.startsWith( "main-pass c=msaa-scene-colour" ) );
			assert.ok( msaaMain.length >= 5, "Every row-on frame renders through the 4x buffers" );
			// The canvas texture's views come from getCurrentTexture, which no
			// createTexture hook sees: unnamed ("?") is exactly the canvas.
			for ( const line of msaaMain ) {
				assert.ok( line.includes( "+R->?" ), "The main pass resolves into the canvas texture" );
				assert.ok( line.includes( "d=msaa-scene-depth" ), "The main pass tests the 4x depth" );
			}
			assert.ok(
				passes.some( line => line.startsWith( "msaa-depth" ) && line.includes( "d=surface-depth" ) ),
				"The 4x depth resolves into the frame's single-sampled depth"
			);
			assert.ok(
				passes.filter( line => line === "main-pass c=? d=surface-depth" ).length >= 10,
				"The row-off frames before and after render the native single-sampled pass"
			);
			// Off is the native frame, byte for byte, on both sides of the row.
			assert.deepEqual( rows[0].rgba, rows[2].rgba, "The row off must restore exact native pixels" );
			assert.deepEqual( gpuErrors, [], "No uncaptured GPU errors in any mode" );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
