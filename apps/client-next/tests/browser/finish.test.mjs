/*
===========================================================================

finish.test.mjs - real GPU presentation shader and native copy behavior

The public experimental setting selects the pass. Both paths encode after
scene drawing in one submission; source pixels and edge checks stay fixed.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

// The presentation pass replaces ColorTarget.encodePresent's byte-exact copy with
// FXAA, sharpening and a grade. Black, mid gray and white are fixed points
// and sit below FXAA's local-contrast floor, so a three-band pattern pins
// the pass: flat interiors survive byte-for-byte, the soft band boundary
// (gray/white) anti-aliases toward the neighbouring band, the near-binary
// boundary (black/gray, the typography stand-in whose luma jump exceeds
// FXAA's hard-edge floor) passes through FXAA untouched, and a
// finish-disabled device still takes the plain copy path.
test( "presentation finish preserves flat colours and gates FXAA off hard edges", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	page.on( "console", m => {
		if ( m.type() === "error" ) console.log( m.text() );
	} );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
			const pattern = document.createElement( "canvas" );
			pattern.width = 128;
			pattern.height = 16;
			const paint = pattern.getContext( "2d" );
			if ( !paint ) throw Error( "2d canvas unavailable" );
			paint.fillStyle = "#000";
			paint.fillRect( 0, 0, 128, 16 );
			paint.fillStyle = "#808080";
			paint.fillRect( 40, 0, 49, 16 );
			paint.fillStyle = "#fff";
			paint.fillRect( 89, 0, 39, 16 );
			const bitmap = await createImageBitmap( pattern );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			/*
			================
			run
			================
			*/
			const run = async ( finishEnabled, width, height ) => {
				const device = createDevice();
				device.experimentalVideo( {
					postProcessing: finishEnabled,
					anisotropicFiltering: false,
					heightFog: false
				} );
				try {
					const deadline = performance.now() + 15000;
					while ( device.phase() === "starting" ) {
						if ( performance.now() > deadline ) throw Error( "Device timeout" );
						await new Promise( requestAnimationFrame );
					}
					if ( device.error() ) throw Error( device.error() );
					const draw = device.images().upload( bitmap );
					const out = device.surfaceCommands().createColor( width, height );
					const depth = device.surfaceCommands().createDepth( width, height );
					const encoder = device.commands().createEncoder();
					const pass = encoder.beginRenderPass( {
						colorAttachments: [ {
							view: out.view,
							clearValue: [ 0, 0, 0, 1 ],
							loadOp: "clear",
							storeOp: "store"
						} ],
						depthStencilAttachment: {
							view: depth.view,
							depthClearValue: 1,
							depthLoadOp: "clear",
							depthStoreOp: "discard"
						}
					} );
					pass.setPipeline( draw.pipeline );
					pass.setBindGroup( 0, draw.binding );
					pass.setViewport( 0, 0, width, height, 0, 1 );
					pass.draw( 6 );
					pass.end();
					canvas.width = width;
					canvas.height = height;
					const context = canvas.getContext( "webgpu" );
					if ( !context ) throw Error( "WebGPU canvas unavailable" );
					device.surfaceCommands().configure( context, device.format() );
					out.encodePresent( encoder, context.getCurrentTexture() );
					device.commands().submit( encoder.finish() );
					const copy = document.createElement( "canvas" );
					copy.width = width;
					copy.height = height;
					const reader = copy.getContext( "2d" );
					if ( !reader ) throw Error( "2d canvas unavailable" );
					const image = await createImageBitmap( canvas );
					reader.drawImage( image, 0, 0 );
					image.close();
					/*
					================
					pixel
					================
					*/
					const pixel = x => [ ...reader.getImageData( x, height >> 1, 1, 1 ).data ];
					depth.dispose();
					out.dispose();
					if ( device.error() ) throw Error( device.error() );
					return pixel;
				} finally {
					device.dispose();
				}
			};
			const finish = await run( true, 128, 16 );
			const plain = await run( false, 128, 16 );
			// A second surface size rebuilds the pass binding for a new source.
			const resized = await run( true, 64, 16 );
			bitmap.close();
			canvas.remove();
			return {
				finish: [ 10, 40, 64, 88, 120 ].map( finish ),
				plain: [ 40, 64 ].map( plain ),
				resized: resized( 32 ),
				error: null
			};
		} );
		assert.equal( result.error, null );
		// Flat interiors are fixed points of the grade.
		assert.deepEqual( result.finish[0], [ 0, 0, 0, 255 ] );
		assert.deepEqual( result.finish[2], [ 128, 128, 128, 255 ] );
		assert.deepEqual( result.finish[4], [ 255, 255, 255, 255 ] );
		// The near-binary boundary (typography stand-in): FXAA must not pull it
		// toward black. The sharpen still adds its halo-guarded contrast term,
		// so it lands a few counts above the untouched 128 - if it ever reads
		// at or below 127, the hard-edge gate has failed and text is smearing.
		assert.ok( result.finish[1][0] >= 129, JSON.stringify( result ) );
		assert.ok( result.finish[1][0] <= 160, JSON.stringify( result ) );
		// The soft boundary (gray/white, under the hard-edge floor): a straight
		// synthetic band gives the FXAA walk nothing to resolve, so only the
		// sub-texel term acts - observed +2 counts over the sharpen-only
		// prediction of 124. The band proves the sub-texel term is alive
		// without pinning its tuning; a fully-guarded or dead FXAA reads 124.
		assert.ok( result.finish[3][0] >= 125, JSON.stringify( result ) );
		assert.ok( result.finish[3][0] <= 200, JSON.stringify( result ) );
		// The disabled device keeps the byte-exact copy path.
		assert.deepEqual( result.plain[0], [ 128, 128, 128, 255 ] );
		assert.deepEqual( result.plain[1], [ 128, 128, 128, 255 ] );
		assert.deepEqual( result.resized, [ 128, 128, 128, 255 ] );
	} finally {
		await browser.close();
	}
} );
