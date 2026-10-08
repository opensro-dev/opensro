/*
===========================================================================

bloom.test.mjs - native bloom composition followed by same-submit presentation

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "native bloom compiles, filters, resizes, disables and rejects stale targets", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	page.on( "console", m => {
		if ( m.type() === "error" ) console.log( m.text() );
	} );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
			const device = createDevice(), canvas = document.createElement( "canvas" );
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
				const pixels = [];
				let stale;
				for ( const [size, value] of [ [ 64, 0 ], [ 128, 128 ], [ 64, 255 ] ] ) {
					canvas.width = canvas.height = size;
					const bloom = device.bloom( size, size, true ),
						out = device.surfaceCommands().createColor( size, size ),
						encoder = device.commands().createEncoder();
					const pass = encoder.beginRenderPass( {
						colorAttachments: [ {
							view: bloom.view,
							clearValue: [ value / 255, value / 255, value / 255, 1 ],
							loadOp: "clear",
							storeOp: "store"
						} ]
					} );
					pass.end();
					bloom.encode( encoder, out.view );
					out.encodePresent( encoder, context.getCurrentTexture() );
					device.commands().submit( encoder.finish() );
					const copy = document.createElement( "canvas" );
					copy.width = copy.height = size;
					const ctx = copy.getContext( "2d" ), image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					pixels.push( [ ...ctx.getImageData( size / 2, size / 2, 1, 1 ).data ] );
					out.dispose();
					stale = bloom;
				}
				const disabled = device.bloom( 64, 64, false );
				let rejected = false;
				try {
					stale.encode( device.commands().createEncoder(), stale.view );
				} catch {
					rejected = true;
				}
				return { pixels, disabled: disabled === undefined, rejected, error: device.error() };
			} finally {
				device.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		assert.deepEqual( result.pixels[0], [ 0, 0, 0, 255 ] );
		assert.deepEqual( result.pixels[2], [ 255, 255, 255, 255 ] );
		for ( const c of result.pixels[1].slice( 0, 3 ) ) {
			assert.ok( Math.abs( c - 130 ) <= 1, JSON.stringify( result ) );
		}
		assert.ok( result.disabled );
		assert.ok( result.rejected );
	} finally {
		await browser.close();
	}
} );
