/*
===========================================================================

ui-size-area-sampling.test.mjs - bitmap UI through the UI size slider's scales

Port-only, not native: the Experimental UI size slider draws the retail
interface at fractional scales (75%, 150%). Nearest sampling there makes
some retail texels one output pixel wide and others two, and below 100%
drops whole strokes. The ui-quads shader takes the exact area average of
the texels each pixel covers instead. Read the production GPU output:
every texel keeps the same ink at every phase, no stroke disappears, and
whole-number scales stay byte-identical to nearest sampling.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"fractional UI sizes keep every retail texel's ink and whole sizes stay nearest",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser( { deviceScaleFactor: 1 } );
		try {
			await page.route(
				CLIENT_NEXT_BASE_URL + "/",
				route =>
					route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
			);
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createUiResources } = await import( "/src/engine/runtime/renderer/device/ui.ts" );
				const { decodeUiFont, titleText } = await import( "/src/engine/foundation/rendering/ui-glyphs.ts" );
				const { fitUiGroup } = await import( "/src/engine/foundation/ui/layout.ts" );
				const font = decodeUiFont( await (await fetch( "/assets/fonts/native-ui-font-atlas.json" )).json() );
				const fontBitmap = await createImageBitmap( await (await fetch( font.image )).blob() );
				const adapter = await navigator.gpu.requestAdapter();
				if ( !adapter ) {
					throw Error( "WebGPU adapter unavailable" );
				}
				const device = await adapter.requestDevice(),
					ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm" );
				const errors = [];
				device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
				await ui.ready;
				ui.texture( font.image, fontBitmap );
				fontBitmap.close();
				// Single white texels six apart: each dot's ink can be summed alone.
				const DOT_TEXTURE = "test:dots", DOT_SPACING = 6, DOT_COUNT = 6;
				const dots = new OffscreenCanvas( DOT_SPACING * DOT_COUNT, DOT_SPACING * DOT_COUNT );
				const context = dots.getContext( "2d" );
				if ( !context ) throw Error( "2D canvas unavailable" );
				context.fillStyle = "#fff";
				for ( let y = 0; y < DOT_COUNT; y++ ) {
					for ( let x = 0; x < DOT_COUNT; x++ ) {
						context.fillRect( x * DOT_SPACING + 1, y * DOT_SPACING + 1, 1, 1 );
					}
				}
				ui.texture( DOT_TEXTURE, dots.transferToImageBitmap() );
				const WIDTH = 256, HEIGHT = 128, stride = WIDTH * 4;
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

			One scene at `scale` output pixels per UI unit: the scene's logical
			size is the target's size divided by the scale, as readUiViewport
			makes it. Returns the red channel.
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
					encoder.copyTextureToBuffer( { texture: color }, { buffer, bytesPerRow: stride }, [
						WIDTH,
						HEIGHT
					] );
					device.queue.submit( [ encoder.finish() ] );
					await buffer.mapAsync( GPUMapMode.READ );
					const rgba = new Uint8Array( buffer.getMappedRange() ).slice();
					buffer.unmap();
					const red = new Float64Array( WIDTH * HEIGHT );
					for ( let i = 0; i < red.length; i++ ) {
						// Linear light: the shader lifts coverage so it blends to the right light.
						red[i] = (rgba[i * 4] / 255) ** 2.2;
					}
					return red;
				}
				const screen = [ 0, 0, 4096, 4096 ];
				/*
			================
			dotQuad
			================
			*/
				function dotQuad( x, y, sampling, uvTurn = 0 ) {
					const side = DOT_SPACING * DOT_COUNT;
					return {
						rect: [ x, y, side, side ],
						uv: [ 0, 0, 1, 1 ],
						color: [ 1, 1, 1, 1 ],
						clip: screen,
						texture: DOT_TEXTURE,
						sampling,
						uvTurn
					};
				}
				const out = { errors, dots: {}, glyphs: {}, fitted: {}, whole: {}, turn: 0 };
				// Each dot's ink in output pixels at fractional scales and phases.
				for ( const scale of [ 0.75, 1.25, 1.5, 1.75 ] ) {
					const inks = [];
					for ( const phase of [ 0, 0.3, 0.55 ] ) {
						const pixels = await render( [ dotQuad( 4 + phase, 4 + phase, "nearest" ) ], scale );
						for ( let dy = 0; dy < DOT_COUNT; dy++ ) {
							for ( let dx = 0; dx < DOT_COUNT; dx++ ) {
								// A window around the dot's footprint narrower than the gap
								// to its neighbours, so nothing else lands in it.
								const cx = (4 + phase + dx * DOT_SPACING + 1.5) * scale;
								const cy = (4 + phase + dy * DOT_SPACING + 1.5) * scale;
								const reach = Math.floor( DOT_SPACING * scale / 2 ) - 1;
								let ink = 0;
								for ( let y = Math.floor( cy ) - reach; y <= Math.floor( cy ) + reach; y++ ) {
									for ( let x = Math.floor( cx ) - reach; x <= Math.floor( cx ) + reach; x++ ) {
										ink += pixels[y * WIDTH + x];
									}
								}
								inks.push( ink );
							}
						}
					}
					out.dots[scale] = { min: Math.min( ...inks ), max: Math.max( ...inks ), expected: scale * scale };
				}
				// A retail text run: total ink scales with area and no row of ink is lost.
				const text = titleText( font, "Lacrimosa Jangan 123", [ 4, 4, 300, 20 ], screen, [ 1, 1, 1, 1 ] );
				const native = await render( text, 1 );
				const sum = values => values.reduce( ( a, b ) => a + b, 0 );
				const rows = values => {
					const lit = [];
					for ( let y = 0; y < HEIGHT; y++ ) {
						let row = 0;
						for ( let x = 0; x < WIDTH; x++ ) row += values[y * WIDTH + x];
						if ( row > 0 ) lit.push( y );
					}
					return lit;
				};
				for ( const scale of [ 0.75, 1.5 ] ) {
					const scaled = await render( text, scale );
					out.glyphs[scale] = {
						ratio: sum( scaled ) / (sum( native ) * scale * scale),
						nativeRows: rows( native ).length,
						scaledRows: rows( scaled ).length
					};
				}
				// A fitted window (the world map on a small screen) shrinks its text at
				// a whole output scale: the same area path, not a weak linear smear.
				for ( const fit of [ 0.8, 0.6 ] ) {
					const fitted = text.map( q => ({ ...q }) );
					const source = /** @type {[number, number, number, number]} */ ([ 0, 0, 320, 40 ]);
					fitUiGroup( fitted, [], 0, 0, [ 0, 0, 320 * fit, 40 * fit ], screen, { sourceBounds: source } );
					const shrunk = await render( fitted, 1 );
					out.fitted[fit] = {
						ratio: sum( shrunk ) / (sum( native ) * fit * fit),
						nearest: fitted.every( q => q.sampling === "nearest" )
					};
				}
				// Whole-number scales: 100% equals linear (aligned texel centres), and
				// 2x and 3x replicate each texel into a whole block, as nearest did.
				const plain = await render( text.map( q => ({ ...q, sampling: "linear" }) ), 1 );
				out.whole[1] = native.every( ( v, i ) => v === plain[i] );
				for ( const scale of [ 2, 3 ] ) {
					const scaled = await render( text, scale );
					let same = true;
					for ( let y = 0; y < HEIGHT; y++ ) {
						for ( let x = 0; x < WIDTH; x++ ) {
							const source = Math.floor( y / scale ) * WIDTH + Math.floor( x / scale );
							if ( scaled[y * WIDTH + x] !== native[source] ) same = false;
						}
					}
					out.whole[scale] = same;
				}
				// The turn slot still decodes under the flag: a turned nearest quad
				// equals the same turn sampled linearly at 100%.
				for ( const turn of [ 1, 2, 3 ] ) {
					const a = await render( [ dotQuad( 5, 5, "nearest", turn ) ], 1 );
					const b = await render( [ dotQuad( 5, 5, "linear", turn ) ], 1 );
					if ( a.every( ( v, i ) => v === b[i] ) ) out.turn++;
				}
				// #1973: the tooltip bubble mirrors its right edge and three
				// corners with a negative uv extent (uv 1,1,-1,-1). Stretched, or
				// at a fractional size, those quads take the area path; mirrored,
				// they must draw the same ink as their unmirrored twins.
				// A solid 8 x 4 texture, the shape of com_tooltip_edge.
				const SOLID_TEXTURE = "test:solid", solid = new OffscreenCanvas( 8, 4 );
				const solidContext = solid.getContext( "2d" );
				if ( !solidContext ) throw Error( "2D canvas unavailable" );
				solidContext.fillStyle = "#fff";
				solidContext.fillRect( 0, 0, 8, 4 );
				ui.texture( SOLID_TEXTURE, solid.transferToImageBitmap() );
				out.mirrored = {};
				for ( const scale of [ 1, 1.5 ] ) {
					for ( const [name, rect] of [ [ "edge", [ 4, 4, 8, 60 ] ], [ "corner", [ 4, 4, 8, 4 ] ] ] ) {
						const quad = uv => ({ ...dotQuad( 0, 0, "nearest" ), texture: SOLID_TEXTURE, rect, uv });
						const plain = sum( await render( [ quad( [ 0, 0, 1, 1 ] ) ], scale ) );
						const mirrored = sum( await render( [ quad( [ 1, 1, -1, -1 ] ) ], scale ) );
						out.mirrored[name + "@" + scale] = { plain, mirrored };
					}
				}
				device.destroy();
				return out;
			} );
			assert.deepEqual( result.errors, [] );
			for ( const [scale, dot] of Object.entries( result.dots ) ) {
				// 8-bit output rounds each pixel by up to half a step.
				const tolerance = 0.03 + dot.expected * 0.02;
				assert.ok(
					dot.min >= dot.expected - tolerance && dot.max <= dot.expected + tolerance,
					`scale ${scale}: dot ink ${dot.min.toFixed( 3 )}..${dot.max.toFixed( 3 )}, want ${dot.expected}`
				);
			}
			for ( const [scale, glyphs] of Object.entries( result.glyphs ) ) {
				assert.ok( Math.abs( glyphs.ratio - 1 ) < 0.03, `scale ${scale}: text ink ratio ${glyphs.ratio}` );
				assert.ok(
					glyphs.scaledRows >= Math.floor( glyphs.nativeRows * Number( scale ) ),
					`scale ${scale}: ${glyphs.scaledRows} inked rows from ${glyphs.nativeRows}`
				);
			}
			for ( const [fit, fitted] of Object.entries( result.fitted ) ) {
				assert.ok( fitted.nearest, `fit ${fit}: runs kept nearest` );
				assert.ok( Math.abs( fitted.ratio - 1 ) < 0.03, `fit ${fit}: text ink ratio ${fitted.ratio}` );
			}
			assert.deepEqual( result.whole, { 1: true, 2: true, 3: true } );
			assert.equal( result.turn, 3 );
			for ( const [name, ink] of Object.entries( result.mirrored ) ) {
				assert.ok( ink.plain > 0, `${name}: the unmirrored quad draws` );
				assert.ok(
					Math.abs( ink.mirrored - ink.plain ) <= ink.plain * 0.02,
					`${name}: mirrored ink ${ink.mirrored.toFixed( 3 )}, unmirrored ${ink.plain.toFixed( 3 )}`
				);
			}
		} finally {
			await browser.close();
		}
	}
);
