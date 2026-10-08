/*
===========================================================================

particle-query.test.mjs - ordered native visibility queries on the real GPU

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"native particle point queries preserve ordered depth writes, equality and untouched color",
	{ timeout: 45000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
				const device = createDevice(), canvas = document.createElement( "canvas" );
				canvas.width = canvas.height = 16;
				document.body.append( canvas );
				try {
					const deadline = performance.now() + 15000;
					while ( device.phase() === "starting" ) {
						if ( performance.now() > deadline ) throw Error( "Device timeout" );
						await new Promise( requestAnimationFrame );
					}
					if ( device.error() ) throw Error( device.error() );
					const context = canvas.getContext( "webgpu" );
					device.surfaceCommands().configure( context, device.format() );
					const depth = device.surfaceCommands().createDepth( 16, 16 ),
						color = device.surfaceCommands().createColor( 16, 16 ),
						view = color.view;
					const encoder = device.commands().createEncoder(),
						pass = encoder.beginRenderPass( {
							colorAttachments: [ {
								view,
								loadOp: "clear",
								storeOp: "store",
								clearValue: [ 0, 0, 1, 1 ]
							} ],
							depthStencilAttachment: {
								view: depth.view,
								depthLoadOp: "clear",
								depthStoreOp: "store",
								depthClearValue: .5
							}
						} );
					pass.end();
					device.commands().submit( encoder.finish() );
					const I = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ),
						samples = await device.particleQuery(
							Float32Array.of(
								0,
								0,
								.25,
								1,
								0,
								0,
								.4,
								1,
								0,
								0,
								.25,
								1,
								3,
								0,
								.1,
								1,
								0,
								0,
								-.1,
								1,
								-.5,
								0,
								.75,
								1
							),
							I,
							view,
							depth.view
						);
					const next = await device.particleQuery(
						Float32Array.of( 0, 0, .3, 1, -.5, 0, .4, 1 ),
						I,
						view,
						depth.view
					);
					const present = device.commands().createEncoder();
					color.encodePresent( present, context.getCurrentTexture() );
					device.commands().submit( present.finish() );
					const copy = document.createElement( "canvas" );
					copy.width = copy.height = 16;
					const ctx = copy.getContext( "2d" ), image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return { samples, next, pixel: [ ...ctx.getImageData( 8, 8, 1, 1 ).data ], error: device.error() };
				} finally {
					device.dispose();
					canvas.remove();
				}
			} );
			assert.deepEqual( result.samples, [ true, false, true, false, false, false ] );
			assert.deepEqual( result.next, [ false, true ] );
			assert.deepEqual( result.pixel, [ 0, 0, 255, 255 ] );
			assert.equal( result.error, null );
		} finally {
			await browser.close();
		}
	}
);

test( "disposing during a native visibility readback cancels it and rejects reuse", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
			const device = createDevice();
			try {
				const deadline = performance.now() + 15000;
				while ( device.phase() === "starting" ) {
					if ( performance.now() > deadline ) throw Error( "Device timeout" );
					await new Promise( requestAnimationFrame );
				}
				if ( device.error() ) throw Error( device.error() );
				const depth = device.surfaceCommands().createDepth( 16, 16 ),
					color = device.surfaceCommands().createColor( 16, 16 );
				const encoder = device.commands().createEncoder(),
					pass = encoder.beginRenderPass( {
						colorAttachments: [ {
							view: color.view,
							loadOp: "clear",
							storeOp: "store",
							clearValue: [ 0, 0, 0, 1 ]
						} ],
						depthStencilAttachment: {
							view: depth.view,
							depthLoadOp: "clear",
							depthStoreOp: "store",
							depthClearValue: 1
						}
					} );
				pass.end();
				device.commands().submit( encoder.finish() );
				const points = Float32Array.of( 0, 0, .5, 1 ),
					matrix = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
				const pending = device.particleQuery( points, matrix, color.view, depth.view ).then(
					() => false,
					() => true
				);
				// The query's ready continuation runs first and starts mapAsync. Dispose
				// in the following microtask, before a GPU completion task can publish it.
				await Promise.resolve();
				device.dispose();
				let reuseRejected = false;
				try {
					await device.particleQuery( points, matrix, color.view, depth.view );
				} catch {
					reuseRejected = true;
				}
				return { cancelled: await pending, reuseRejected };
			} finally {
				device.dispose();
			}
		} );
		assert.deepEqual( result, { cancelled: true, reuseRejected: true } );
	} finally {
		await browser.close();
	}
} );
