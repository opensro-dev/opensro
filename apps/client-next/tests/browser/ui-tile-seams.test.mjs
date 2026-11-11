/*
===========================================================================

ui-tile-seams.test.mjs - a tiled UI background leaves no seam

CIFNormalTile (normal-tile.ts) repeats its texture across a rect. Drawn as
texture-sized quads edge to edge, every shared edge that fell inside an
output pixel (a fractional UI scale, the job alias window's 1.01) was
covered partly by each quad, and the two partial coverages let up to a
quarter of the world through as a thin vertical line. Read the production
GPU output of normalTile's quad, with a separate quad against its left
edge as a window frame's border, at whole and fractional scales and
origins: every pixel between them is opaque, and at 100% the texels match
the separate tiles exactly.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "a tiled background covers every pixel at whole and fractional scales", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { deviceScaleFactor: 1 } );
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createUiResources } = await import( "/src/engine/runtime/renderer/device/ui.ts" );
			const { normalTile } = await import( "/src/engine/foundation/ui/normal-tile.ts" );
			const adapter = await navigator.gpu.requestAdapter();
			if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
			const device = await adapter.requestDevice(),
				ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm" );
			const errors = [];
			device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
			await ui.ready;
			// An opaque tile, as com_bg_tile_b: alpha 255 everywhere. Its columns
			// ramp in red so a wrong texel shows; green stays full for the seam test.
			const TILE = "test:tile", TILE_SIZE = 16;
			const tile = new OffscreenCanvas( TILE_SIZE, TILE_SIZE ), context = tile.getContext( "2d" );
			if ( !context ) throw Error( "2D canvas unavailable" );
			for ( let x = 0; x < TILE_SIZE; x++ ) {
				context.fillStyle = `rgb(${x * 16},255,0)`;
				context.fillRect( x, 0, 1, TILE_SIZE );
			}
			ui.texture( TILE, tile.transferToImageBitmap() );
			const WIDTH = 192, HEIGHT = 80, stride = WIDTH * 4;
			const color = device.createTexture( {
				size: [ WIDTH, HEIGHT ],
				format: "rgba8unorm",
				usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC
			} );
			const depth = device.createTexture( {
				size: [ WIDTH, HEIGHT ],
				format: "depth24plus",
				usage: GPUTextureUsage.RENDER_ATTACHMENT
			} );
			const buffer = device.createBuffer( {
				size: stride * HEIGHT,
				usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
			} );
			let revision = 0;
			/*
			================
			render

			One scene at `scale` output pixels per UI unit over a black clear;
			returns the red and green channels, 0..255.
			================
			*/
			async function render( quads, scale ) {
				const draws = ui.prepare( {
					revision: ++revision,
					width: WIDTH / scale,
					height: HEIGHT / scale,
					quads
				}, WIDTH );
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
				encoder.copyTextureToBuffer( { texture: color }, { buffer, bytesPerRow: stride }, [ WIDTH, HEIGHT ] );
				device.queue.submit( [ encoder.finish() ] );
				await buffer.mapAsync( GPUMapMode.READ );
				const rgba = new Uint8Array( buffer.getMappedRange() ).slice();
				buffer.unmap();
				const red = new Uint8Array( WIDTH * HEIGHT ), green = new Uint8Array( WIDTH * HEIGHT );
				for ( let i = 0; i < red.length; i++ ) {
					red[i] = rgba[i * 4];
					green[i] = rgba[i * 4 + 1];
				}
				return { red, green };
			}
			const screen = [ 0, 0, 4096, 4096 ];
			/*
			================
			tileQuad

			One texture-sized tile drawn on its own, as the tiles were before.
			================
			*/
			function tileQuad( x, y ) {
				return {
					rect: [ x, y, TILE_SIZE, TILE_SIZE ],
					uv: [ 0, 0, 1, 1 ],
					color: [ 1, 1, 1, 1 ],
					clip: screen,
					texture: TILE
				};
			}
			const span = origin =>
				normalTile( [ origin, origin, 2 * TILE_SIZE + 5, TILE_SIZE ], TILE, [ TILE_SIZE, TILE_SIZE ], screen );
			const out = { errors, darkest: {}, sameAtWhole: false, leftEdge: 0, topEdge: 0 };
			// At 100% on whole pixels a tiled background draws texel for texel
			// what its separate tiles drew: the same red ramp in every column.
			const whole = await render( span( 8 ), 1 );
			const separate = await render( [ tileQuad( 8, 8 ), tileQuad( 8 + TILE_SIZE, 8 ), {
				...tileQuad( 8 + 2 * TILE_SIZE, 8 ),
				rect: [ 8 + 2 * TILE_SIZE, 8, 5, TILE_SIZE ],
				uv: [ 0, 0, 5 / TILE_SIZE, 1 ]
			} ], 1 );
			out.sameAtWhole = whole.red.every( ( v, i ) => v === separate.red[i] );
			// A background on a half pixel at 100% (a window centred on an odd
			// extent): the pixel whose centre lies on its left and top edges is
			// the background's, by the rasterizer's top-left rule.
			const half = (await render( span( 8.5 ), 1 )).green;
			out.leftEdge = half[20 * WIDTH + 8];
			out.topEdge = half[8 * WIDTH + 20];
			for ( const scale of [ 1, 1.1, 1.5 ] ) {
				for ( const origin of [ 24, 24.5, 24.25 ] ) {
					// The background and, against its left edge, a separate quad as the
					// window frame's border: their shared edge must not let the clear through.
					const pixels =
						(await render( [ tileQuad( origin - TILE_SIZE, origin ), ...span( origin ) ], scale ))
							.green;
					// Rows and columns wholly inside the pair, one pixel in from its outer edges.
					const x0 = Math.ceil( (origin - TILE_SIZE) * scale ) + 1,
						x1 = Math.floor( (origin + 2 * TILE_SIZE) * scale ) - 2;
					const y0 = Math.ceil( origin * scale ) + 1, y1 = Math.floor( (origin + TILE_SIZE) * scale ) - 2;
					let darkest = 255;
					for ( let y = y0; y <= y1; y++ ) {
						for ( let x = x0; x <= x1; x++ ) darkest = Math.min( darkest, pixels[y * WIDTH + x] );
					}
					out.darkest[scale + "@" + origin] = darkest;
				}
			}
			device.destroy();
			return out;
		} );
		assert.deepEqual( result.errors, [] );
		assert.ok( result.sameAtWhole, "a tiled background at 100% draws the texels its separate tiles drew" );
		assert.deepEqual(
			[ result.leftEdge, result.topEdge ],
			[ 255, 255 ],
			"a half-pixel background keeps its left and top edge pixels"
		);
		// Opaque full green over black: any pixel short of full green lets the clear through.
		const seams = Object.entries( result.darkest ).filter( ( [, darkest] ) => darkest < 254 );
		assert.deepEqual( seams, [], "scale@origin -> darkest pixel between the quads, want 255" );
	} finally {
		await browser.close();
	}
} );

test( "the job alias window's frame and background leave no gap between them", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { deviceScaleFactor: 1 } );
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createUiResources } = await import( "/src/engine/runtime/renderer/device/ui.ts" );
			const { normalTile } = await import( "/src/engine/foundation/ui/normal-tile.ts" );
			const { frameRing, frameParts } = await import( "/src/engine/foundation/ui/frame-ring.ts" );
			const adapter = await navigator.gpu.requestAdapter();
			if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
			const device = await adapter.requestDevice(),
				ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm" );
			const errors = [];
			device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
			await ui.ready;
			const sizes = {};
			/*
			================
			load
			================
			*/
			async function load( path ) {
				const bitmap = await createImageBitmap( await (await fetch( path )).blob() );
				sizes[path] = [ bitmap.width, bitmap.height ];
				ui.texture( path, bitmap );
			}
			// ginterface GDR_JOB_ALIAS: the msgbox2 frame, 297 wide, and the
			// ifjobalias background tile at 16,40 (124 tall to modify an alias).
			const FRAME = "/assets/images/Media_extracted/interface/messagebox/msgbox2_window_";
			const TILE = "/assets/images/Media_extracted/interface/ifcommon/bg_tile/com_bg_tile_b.png";
			for ( const part of frameParts() ) await load( FRAME + part + ".png" );
			await load( TILE );
			const WIDTH = 384, HEIGHT = 256, stride = WIDTH * 4;
			const color = device.createTexture( {
				size: [ WIDTH, HEIGHT ],
				format: "rgba8unorm",
				usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC
			} );
			const depth = device.createTexture( {
				size: [ WIDTH, HEIGHT ],
				format: "depth24plus",
				usage: GPUTextureUsage.RENDER_ATTACHMENT
			} );
			const buffer = device.createBuffer( {
				size: stride * HEIGHT,
				usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
			} );
			let revision = 0;
			/*
			================
			render

			Over a magenta clear, the world any gap would show.
			================
			*/
			async function render( quads, scale ) {
				const draws = ui.prepare( {
					revision: ++revision,
					width: WIDTH / scale,
					height: HEIGHT / scale,
					quads
				}, WIDTH );
				const encoder = device.createCommandEncoder();
				const pass = encoder.beginRenderPass( {
					colorAttachments: [ {
						view: color.createView(),
						clearValue: [ 1, 0, 1, 1 ],
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
				encoder.copyTextureToBuffer( { texture: color }, { buffer, bytesPerRow: stride }, [ WIDTH, HEIGHT ] );
				device.queue.submit( [ encoder.finish() ] );
				await buffer.mapAsync( GPUMapMode.READ );
				const rgba = new Uint8Array( buffer.getMappedRange() ).slice();
				buffer.unmap();
				return rgba;
			}
			const screen = [ 0, 0, 4096, 4096 ], gaps = [];
			for ( const scale of [ 1, 1.01, 1.1, 1.25 ] ) {
				// A window centred on an odd extent sits on a half pixel.
				for ( const ox of [ 10, 10.5, 10.25 ] ) {
					const oy = 10;
					const rgba = await render( [
						...frameRing(
							[ ox, oy, 297, 180 ],
							FRAME,
							frameParts().map( part => sizes[FRAME + part + ".png"] ),
							screen
						),
						...normalTile( [ ox + 16, oy + 40, 265, 124 ], TILE, sizes[TILE], screen )
					], scale );
					// The window's left side, frame border to well inside the background.
					for ( let y = Math.ceil( (oy + 50) * scale ); y < (oy + 150) * scale; y++ ) {
						for ( let x = Math.ceil( ox * scale ); x < (ox + 40) * scale; x++ ) {
							const i = (y * WIDTH + x) * 4;
							if ( rgba[i] > 90 && rgba[i + 2] > 90 && rgba[i + 1] < 60 ) {
								gaps.push( scale + "@" + ox + " x=" + x + " y=" + y );
							}
						}
					}
				}
			}
			device.destroy();
			return { errors, gaps: gaps.slice( 0, 8 ) };
		} );
		assert.deepEqual( result.errors, [] );
		assert.deepEqual( result.gaps, [], "world pixels showing between the alias window's frame and background" );
	} finally {
		await browser.close();
	}
} );
