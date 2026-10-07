/*
===========================================================================

bitmap-text-scaling.test.mjs - retail glyph ink through fractional output scale

BR-261005-1421-A2CF: browser zoom and Windows scaling must not blur bitmap
text or change the shape of a moving name. Read the production GPU output,
including retained scenes resized without a new UI publication.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "bitmap text stays sharp and motion-stable through browser scaling", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { deviceScaleFactor: 1.25 } );
	try {
		// This fixture renders through the production glyph and GPU owners; it
		// needs no player login, game state, or application entrypoint.
		await page.route( CLIENT_NEXT_BASE_URL + "/", route =>
			route.fulfill( {
				contentType: "text/html",
				body: "<!doctype html><html><body></body></html>"
			} ) );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const session = await page.context().newCDPSession( page );
		const captured = [];
		for ( const ratio of [ .8, .9, 1, 1.1, 1.25, 1.5, 1.75, 2, .9, 1 ] ) {
			await session.send( "Emulation.setDeviceMetricsOverride", {
				width: 800,
				height: 600,
				deviceScaleFactor: ratio,
				mobile: false
			} );
			const result = await page.evaluate( async () => {
				const uiPath = "/src/engine/runtime/renderer/device/ui.ts";
				const fontPath = "/src/engine/foundation/rendering/ui-glyphs.ts";
				const { createUiResources } = await import( uiPath );
				const { decodeUiFont, titleText } = await import( fontPath );
				const platformPath = "/src/engine/runtime/platform/platform.ts";
				const { createPlatform } = await import( platformPath );
				const font = decodeUiFont( await (await fetch( "/assets/fonts/native-ui-font-atlas.json" )).json() );
				const bitmap = await createImageBitmap( await (await fetch( font.image )).blob() );
				const adapter = await navigator.gpu.requestAdapter();
				if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
				const device = await adapter.requestDevice(), ui = createUiResources( device, "rgba8unorm" );
				/** @type {string[]} */
				const errors = [];
				device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
				await ui.ready;
				ui.texture( font.image, bitmap );
				bitmap.close();
				const results = [];
				try {
					for ( const sceneWidth of [ 640, 641 ] ) {
						const sceneHeight = 80;
						const canvas = document.createElement( "canvas" ), status = document.createElement( "output" );
						canvas.style.width = sceneWidth + "px";
						canvas.style.height = sceneHeight + "px";
						document.body.append( canvas, status );
						const platform = createPlatform( canvas, status, () => {}, () => {} );
						// The default mode removes inline sizing; set the fixture's CSS box
						// through a stylesheet just as the production page sizes its canvas.
						canvas.style.width = sceneWidth + "px";
						canvas.style.height = sceneHeight + "px";
						await new Promise( requestAnimationFrame );
						await new Promise( requestAnimationFrame );
						const physical = { ...platform.readViewport() };
						const scene = {
							revision: 1,
							width: Math.round( platform.canvasSize().width / platform.displayScale() ),
							height: Math.round( platform.canvasSize().height / platform.displayScale() ),
							quads: titleText( font, "Lacrimosa  Jangan  123456", [ 20, 20, 500, 25 ], [
								0,
								0,
								sceneWidth,
								sceneHeight
							], [ 1, 1, 1, 1 ] )
						};
						for ( const ratio of [ devicePixelRatio ] ) {
							const { width, height } = physical;
							const stride = Math.ceil( width * 4 / 256 ) * 256;
							const color = device.createTexture( {
								size: [ width, height ],
								format: "rgba8unorm",
								usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC
							} );
							const depth = device.createTexture( {
								size: [ width, height ],
								format: "depth24plus",
								usage: GPUTextureUsage.RENDER_ATTACHMENT
							} );
							const buffer = device.createBuffer( {
								size: stride * height,
								usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
							} );
							/*
						================
						render
						================
						*/
							async function render( publication ) {
								const draws = ui.prepare( publication, { width, height } );
								const encoder = device.createCommandEncoder();
								const pass = encoder.beginRenderPass( {
									colorAttachments: [ {
										view: color.createView(),
										clearValue: [ 0, 0, 0, 1 ],
										loadOp: "clear",
										storeOp: "store"
									} ],
									depthStencilAttachment: {
										view: depth.createView(),
										depthClearValue: 1,
										depthLoadOp: "clear",
										depthStoreOp: "store"
									}
								} );
								for ( const draw of draws ) {
									pass.setPipeline( draw.pipeline );
									pass.setBindGroup( 0, draw.binding );
									pass.draw( 6, draw.count, 0, draw.first );
								}
								pass.end();
								encoder.copyTextureToBuffer( { texture: color }, { buffer, bytesPerRow: stride }, [
									width,
									height
								] );
								device.queue.submit( [ encoder.finish() ] );
								await buffer.mapAsync( GPUMapMode.READ );
								const pixels = new Uint8Array( buffer.getMappedRange() ).slice();
								buffer.unmap();
								return pixels;
							}
							try {
								// Resize with identical scene identity, then force a fresh publication:
								// both paths must paint the same physical positions.
								const retained = await render( scene ), refreshed = await render( { ...scene } );
								const retainedEqual = retained.every( ( value, i ) => value === refreshed[i] );
								const old = ratio === 1 ?
									await render( {
										...scene,
										quads: scene.quads.map( q => ({ ...q, sampling: "linear" }) )
									} ) :
									retained;
								const nativeEqual = retained.every( ( value, i ) => value === old[i] );
								let reference = "", changes = 0, blurred = 0, lit = 0;
								for ( let offset = 0; offset < 8; offset++ ) {
									const quads = scene.quads.map( q => ({
										...q,
										rect: [ q.rect[0] + offset, q.rect[1] + offset, q.rect[2], q.rect[3] ]
									}) );
									const pixels = await render( { ...scene, quads } );
									let left = width, top = height, right = 0, bottom = 0;
									for ( let y = 0; y < height; y++ ) {
										for ( let x = 0; x < width; x++ ) {
											const value = pixels[y * stride + x * 4];
											if ( !value ) continue;
											lit++;
											if ( value !== 255 ) blurred++;
											left = Math.min( left, x );
											right = Math.max( right, x );
											top = Math.min( top, y );
											bottom = Math.max( bottom, y );
										}
									}
									const crop = [];
									for ( let y = top; y <= bottom; y++ ) {
										for ( let x = left; x <= right; x++ ) {
											crop.push( pixels[y * stride + x * 4] );
										}
									}
									const signature = JSON.stringify( [ right - left, bottom - top, crop ] );
									if ( offset === 0 ) reference = signature;
									else if ( reference !== signature ) changes++;
								}
								// Leave the same original scene cached across the next zoom change.
								await render( scene );
								results.push( {
									sceneWidth,
									ratio,
									retainedEqual,
									nativeEqual,
									changes,
									blurred,
									lit,
									physicalMatch: scene.width === width && scene.height === height,
									ink: reference
								} );
							} finally {
								color.destroy();
								depth.destroy();
								buffer.destroy();
							}
						}
						platform.dispose();
						canvas.remove();
						status.remove();
					}
					return { errors, results };
				} finally {
					ui.dispose();
					device.destroy();
				}
			} );
			captured.push( result );
		}
		const result = { errors: captured.flatMap( r => r.errors ), results: captured.flatMap( r => r.results ) };
		await mkdir( "temp/artifacts/bitmap-text-scaling", { recursive: true } );
		await writeFile( "temp/artifacts/bitmap-text-scaling/gpu.json", JSON.stringify( result, null, 2 ) );
		assert.deepEqual( result.errors, [] );
		for ( const row of result.results ) {
			const context = JSON.stringify( row );
			assert.ok( row.lit > 0, context );
			assert.equal( row.blurred, 0, context );
			assert.equal( row.changes, 0, context );
			assert.ok( row.retainedEqual, context );
			assert.ok( row.nativeEqual, context );
			assert.ok( row.physicalMatch, context );
			assert.equal( row.ink, result.results[0].ink, "Every scale preserves exactly the same glyph pixels" );
		}
	} finally {
		await browser.close();
	}
} );

test(
	"zoom keeps retained controls, pointer coordinates and selected screen sizes aligned",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser( {
			viewport: { width: 1600, height: 900 },
			deviceScaleFactor: 1
		} );
		try {
			await page.route( CLIENT_NEXT_BASE_URL + "/", route =>
				route.fulfill( {
					contentType: "text/html",
					body:
						"<!doctype html><style>body{margin:0;overflow:hidden}canvas{display:block;width:100vw;height:100vh}</style><canvas></canvas><output></output>"
				} ) );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.evaluate( async () => {
				const path = "/src/engine/runtime/platform/platform.ts";
				const { createPlatform } = await import( path );
				/** @type {any[]} */
				const events = [], hits = [];
				const canvas = document.querySelector( "canvas" );
				const platform = createPlatform(
					canvas,
					document.querySelector( "output" ),
					() => {},
					() => {},
					() => {},
					event => events.push( event ),
					( x, y ) => {
						hits.push( [ x, y ] );
						return false;
					}
				);
				const semantics = {
					title: "Scaling fixture",
					message: "",
					controls: [ {
						id: "scaling-button",
						label: "Native button",
						kind: "button",
						rect: [ 100, 80, 120, 30 ]
					}, {
						id: "scaling-editor",
						label: "Native editor",
						kind: "text",
						textInsets: [ 3, 1, 5, 2 ],
						rect: [ 100, 130, 160, 20 ]
					} ]
				};
				platform.presentUi( semantics );
				/** @type {any} */ (globalThis).scalingFixture = { platform, canvas, semantics, events, hits };
			} );
			const session = await page.context().newCDPSession( page );
			for ( const ratio of [ 1, .9, 1.25, .8, 1.5, 2, 1 ] ) {
				await session.send( "Emulation.setDeviceMetricsOverride", {
					width: Math.round( 1600 / ratio ),
					height: Math.round( 900 / ratio ),
					deviceScaleFactor: ratio,
					mobile: false
				} );
				await page.waitForFunction( ratio => {
					const button = document.querySelector( '[data-ui-id="scaling-button"]' );
					return button && Math.abs( button.getBoundingClientRect().width * ratio - 120 ) < .1;
				}, ratio );
				await page.evaluate( () => {
					const f = /** @type {any} */ (globalThis).scalingFixture;
					f.events.length = 0;
					f.hits.length = 0;
				} );
				await page.mouse.click( 160 / ratio, 95 / ratio );
				await page.mouse.click( 500 / ratio, 300 / ratio );
				const result = await page.evaluate( () => {
					const f = /** @type {any} */ (globalThis).scalingFixture;
					const editor = document.querySelector( '[data-ui-id="scaling-editor"]' );
					if ( !editor ) {
						throw new Error( "Native text editor is missing" );
					}
					return {
						activated: f.events.some( e => e.kind === "activate" && e.id === "scaling-button" ),
						hit: f.hits.at( -1 ),
						viewport: f.platform.readViewport(),
						fontSize: parseFloat( getComputedStyle( editor ).fontSize ),
						padding: [ "paddingLeft", "paddingTop", "paddingRight", "paddingBottom" ].map(
							key => parseFloat( getComputedStyle( editor )[key] )
						)
					};
				} );
				assert.ok( result.activated );
				assert.ok( Math.abs( result.fontSize * ratio - 12 ) < .001 );
				for ( const [index, inset] of [ 3, 1, 5, 2 ].entries() ) {
					assert.ok( Math.abs( result.padding[index] * ratio - inset ) < .001 );
				}
				assert.ok( Math.abs( result.hit[0] - 500 ) <= 1, JSON.stringify( result ) );
				assert.ok( Math.abs( result.hit[1] - 300 ) <= 1, JSON.stringify( result ) );
			}
			const fixed = await page.evaluate( async () => {
				const f = /** @type {any} */ (globalThis).scalingFixture;
				const path = "/src/engine/foundation/rendering/video-options.ts";
				const { defaultVideoOptions } = await import( path );
				f.platform.saveVideoOptions( { ...defaultVideoOptions(), displaySize: [ 800, 600 ] } );
				return { ...f.platform.readViewport() };
			} );
			assert.deepEqual( fixed, { width: 800, height: 600 } );
			await session.send( "Emulation.setDeviceMetricsOverride", {
				width: 1280,
				height: 720,
				deviceScaleFactor: 1.25,
				mobile: false
			} );
			await page.waitForFunction( () => {
				const f = /** @type {any} */ (globalThis).scalingFixture;
				return Math.abs( f.canvas.getBoundingClientRect().width - 640 ) < .1;
			} );
			const scaled = await page.evaluate( () => {
				const f = /** @type {any} */ (globalThis).scalingFixture;
				const result = { ...f.platform.readViewport() };
				f.platform.dispose();
				return result;
			} );
			assert.deepEqual( scaled, fixed );
		} finally {
			await browser.close();
		}
	}
);
