/*
===========================================================================
environment-stages.test.mjs - isolated opt-in effects and exact native restoration
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { prepareEnvironmentFixture, captureEnvironment, captureFlatRelief } from "../helpers/environment-gpu.mjs";

test(
	"each environment option changes the retail frame and disabling it restores every channel",
	{ timeout: 180000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		try {
			const fixture = await prepareEnvironmentFixture( page, CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( captureEnvironment, { ...fixture, features: true } );
			const native = result.rows[0].rgba;
			for ( const row of result.rows ) {
				if ( row.mode === "off" ) {
					assert.deepEqual(
						row.rgba,
						native,
						"Disabled stages must restore exact native pixels"
					);
				} else assert.notDeepEqual( row.rgba, native, `${row.mode} must independently affect the frame` );
			}
			const flat = await page.evaluate( captureFlatRelief );
			assert.deepEqual( flat[1], flat[0], "Flat terrain with zero ambient and diffuse must retain albedo" );
			assert.ok( flat[0].some( ( value, i ) => i % 4 !== 3 && value > 32 ), "Witness must draw visible terrain" );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);

test( "the float bloom chain composites, resizes and hands quality back", { timeout: 60000 }, async () => {
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
				if ( !context ) throw Error( "webgpu context unavailable" );
				device.surfaceCommands().configure( context, device.format() );
				const pixels = [];
				for ( const floatBloom of [ false, true, false ] ) {
					for ( const [size, value] of [ [ 64, 0 ], [ 128, 128 ], [ 64, 255 ], [ 64, 128 ] ] ) {
						device.experimentalVideo( {
							postProcessing: false,
							anisotropicFiltering: false,
							heightFog: false,
							dynamicSun: false,
							terrainRelief: false,
							texturedHorizon: false,
							floatBloom
						} );
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
						const ctx = copy.getContext( "2d" );
						if ( !ctx ) throw Error( "2d context unavailable" );
						const image = await createImageBitmap( canvas );
						ctx.drawImage( image, 0, 0 );
						image.close();
						pixels.push( [ ...ctx.getImageData( size / 2, size / 2, 1, 1 ).data ] );
						out.dispose();
					}
				}
				return { pixels, error: device.error() };
			} finally {
				device.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		assert.deepEqual(
			result.pixels.slice( 0, 4 ),
			result.pixels.slice( 8, 12 ),
			"Switching float bloom off restores native composite pixels after resizing"
		);
		assert.deepEqual( result.pixels[4], [ 0, 0, 0, 255 ] );
		assert.deepEqual( result.pixels[6], [ 255, 255, 255, 255 ] );
		assert.notDeepEqual(
			result.pixels[1],
			result.pixels[5],
			"Float bloom must differ from native quantized bloom"
		);
		assert.deepEqual( result.pixels[5], result.pixels[7], "Float glow center survives resize" );
	} finally {
		await browser.close();
	}
} );
