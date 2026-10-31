/*
===========================================================================

ui-icon-atlas.test.mjs - the icon atlas through the production UI owner

Port-only, not native. The asset build packs the interface's icons into
one edge-padded sheet (scripts/build/ui_icon_atlas.py); this proves the
client side end to end through createUiResources: the same quads drawn
from their own textures and from the sheet - with the packer's uv remap,
linearly and mirrored and turned, at whole and fractional UI sizes -
must read back byte-identical frames, and the sheet's family batches
into fewer draw commands than the per-icon textures.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"the atlas sheet draws the icon quads byte-identically and batches the family",
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
				const { iconAtlas } = await import( "/src/engine/foundation/ui/icon-atlas.ts" );
				const adapter = await navigator.gpu.requestAdapter();
				if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
				const device = await adapter.requestDevice(),
					ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm" );
				const errors = [];
				device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
				await ui.ready;
				// Two synthetic icons with smooth colour and alpha, as the retail
				// art: every texel differs, so a wrong uv or a bleeding neighbour
				// shows in the readback.
				const ICON = 32, GUTTER = 2;
				function icon( seed ) {
					const canvas = new OffscreenCanvas( ICON, ICON );
					const context = canvas.getContext( "2d" );
					if ( !context ) throw Error( "No icon context" );
					const image = context.createImageData( ICON, ICON );
					for ( let y = 0; y < ICON; y++ ) {
						for ( let x = 0; x < ICON; x++ ) {
							const at = (y * ICON + x) * 4;
							image.data[at] = (x * 8 + seed * 37) % 256;
							image.data[at + 1] = (y * 5 + seed * 11) % 256;
							image.data[at + 2] = (x * y + seed) % 256;
							image.data[at + 3] = 64 + (x * 3 + y * 2) % 128;
						}
					}
					context.putImageData( image, 0, 0 );
					return canvas.transferToImageBitmap();
				}
				const a = icon( 3 ), b = icon( 7 );
				// A high-frequency icon: hard alpha edges and a checker, the
				// case where a gutter bleed would show first.
				const checker = new OffscreenCanvas( ICON, ICON );
				const cc = checker.getContext( "2d" );
				if ( !cc ) throw Error( "No checker context" );
				for ( let y = 0; y < ICON; y++ ) {
					for ( let x = 0; x < ICON; x++ ) {
						cc.fillStyle = (x >> 2) % 2 === (y >> 2) % 2 ? "#fff" : "#000";
						cc.fillRect( x, y, 1, 1 );
					}
				}
				cc.fillStyle = "#fff";
				cc.fillRect( 0, 0, 1, ICON );
				cc.fillRect( ICON - 1, 0, 1, ICON );
				const A = "/assets/images/Media_extracted/icon/atlas-a.png",
					B = "/assets/images/Media_extracted/icon/atlas-b.png",
					SHEET = "/assets/ui-atlas/sheet-0.png";
				// The sheet, composed exactly as the build does: each icon in a
				// cell of its own edge texels repeated around it.
				const C = "/assets/images/Media_extracted/icon/atlas-c.png";
				/** One sheet cell: its icon path, its bitmap, its cell origin. */
				const cells = /** @type {[string, ImageBitmap, number, number][]} */ ([
					[ A, a, GUTTER, GUTTER ],
					[ B, b, GUTTER + ICON + GUTTER * 2, GUTTER ],
					[ C, checker, GUTTER * 2 + (ICON + GUTTER * 2) * 2, GUTTER ]
				]);
				const sheet = new OffscreenCanvas( GUTTER * 6 + ICON * 3, GUTTER * 2 + ICON );
				const sc = sheet.getContext( "2d" );
				if ( !sc ) throw Error( "No sheet context" );
				sc.imageSmoothingEnabled = false;
				for ( const [, bitmap, x, y] of cells ) {
					sc.drawImage( bitmap, x, y );
					sc.drawImage( bitmap, 0, 0, 1, ICON, x - GUTTER, y, GUTTER, ICON );
					sc.drawImage( bitmap, ICON - 1, 0, 1, ICON, x + ICON, y, GUTTER, ICON );
					sc.drawImage( bitmap, 0, 0, ICON, 1, x, y - GUTTER, ICON, GUTTER );
					sc.drawImage( bitmap, 0, ICON - 1, ICON, 1, x, y + ICON, ICON, GUTTER );
					for (
						const [cx, cy] of [
							[ x - GUTTER, y - GUTTER ],
							[ x + ICON, y - GUTTER ],
							[ x - GUTTER, y + ICON ],
							[ x + ICON, y + ICON ]
						]
					) {
						sc.drawImage(
							bitmap,
							cx < x ? 0 : ICON - 1,
							cy < y ? 0 : ICON - 1,
							1,
							1,
							cx,
							cy,
							GUTTER,
							GUTTER
						);
					}
				}
				const manifest = {
					version: 1,
					sheets: [ SHEET ],
					sheetSize: [ [ sheet.width, sheet.height ] ],
					icons: {
						[A]: [ 0, GUTTER, GUTTER, ICON, ICON ],
						[B]: [ 0, GUTTER + ICON + GUTTER * 2, GUTTER, ICON, ICON ],
						[C]: [ 0, GUTTER * 2 + (ICON + GUTTER * 2) * 2, GUTTER, ICON, ICON ]
					}
				};
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
				const screen = [ 0, 0, 4096, 4096 ];
				let revision = 0;
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
					const bytes = new Uint8Array( buffer.getMappedRange() ).slice();
					buffer.unmap();
					return { bytes, draws: draws.length };
				}
				function quads( texture ) {
					// A plain quad, a mirrored quad (the tooltip's right edge,
					// uv 1,1,-1,-1 as #1973) and a turned one, side by side.
					const plain = {
						rect: [ 4, 4, ICON, ICON ],
						uv: [ 0, 0, 1, 1 ],
						color: [ 1, 1, 1, 1 ],
						clip: screen,
						texture
					};
					const mirrored = { ...plain, rect: [ 44, 4, ICON, ICON ], uv: [ 1, 1, -1, -1 ] };
					const turned = { ...plain, rect: [ 84, 4, ICON, ICON ], uvTurn: 1 };
					const second = { ...plain, texture: B, rect: [ 4, 44, ICON, ICON ] };
					const third = { ...plain, texture: C, rect: [ 4, 84, ICON, ICON ] };
					return [ plain, mirrored, turned, second, third ];
				}
				const out = { errors, frames: {}, draws: {} };
				ui.texture( A, a );
				ui.texture( B, b );
				ui.texture( C, checker );
				for ( const scale of [ 1, 1.5, 0.75 ] ) {
					const perIcon = await render( quads( A ), scale );
					out.frames["per" + scale] = [ ...perIcon.bytes ];
					out.draws["per" + scale] = perIcon.draws;
				}
				// The atlas: the sheet replaces the per-icon textures, the
				// mapping directs the same quads into it.
				ui.texture( A, null );
				ui.texture( B, null );
				ui.texture( C, null );
				ui.texture( SHEET, sheet.transferToImageBitmap() );
				ui.atlas( iconAtlas( manifest ) );
				for ( const scale of [ 1, 1.5, 0.75 ] ) {
					const fromSheet = await render( quads( A ), scale );
					out.frames["sheet" + scale] = [ ...fromSheet.bytes ];
					out.draws["sheet" + scale] = fromSheet.draws;
				}
				// Clearing the mapping restores the per-icon reads (the icon
				// textures come back as the publisher would re-send them).
				ui.atlas( null );
				ui.texture( A, a );
				ui.texture( B, b );
				ui.texture( C, checker );
				const cleared = await render( quads( A ), 1 );
				out.frames.cleared = [ ...cleared.bytes ];
				ui.dispose();
				return out;
			} );
			assert.deepEqual( result.errors, [], "No uncaptured GPU errors" );
			// A whole scale keeps every sample on the texel grid: byte-identical.
			assert.deepEqual(
				result.frames.sheet1,
				result.frames.per1,
				"The sheet's frame is byte-identical at UI scale 1"
			);
			// A fractional scale interpolates the remapped corner uvs in
			// float32: a pixel centre can round to the neighbouring tap cell
			// and shift one texel's weight by a step of 1/255. Pin the parity
			// to that: no pixel moves further, and almost none move at all.
			for ( const scale of [ 1.5, 0.75 ] ) {
				const a2 = result.frames["per" + scale], b2 = result.frames["sheet" + scale];
				let diff = 0, maxDelta = 0;
				for ( let i = 0; i < a2.length; i += 4 ) {
					let delta = 0;
					for ( let c = 0; c < 4; c++ ) delta = Math.max( delta, Math.abs( a2[i + c] - b2[i + c] ) );
					if ( delta > 0 ) {
						diff++;
						maxDelta = Math.max( maxDelta, delta );
					}
				}
				assert.ok(
					diff <= 60 && maxDelta <= 1,
					`UI scale ${scale}: ${diff} pixels differ by at most 1/255 (float32 uv rounding)`
				);
			}
			assert.deepEqual(
				result.frames.cleared,
				result.frames.per1,
				"Clearing the mapping restores the per-icon frame"
			);
			assert.ok(
				result.draws.sheet1 < result.draws.per1,
				`The family batches into fewer draws (${result.draws.sheet1} vs ${result.draws.per1})`
			);
		} finally {
			await browser.close();
		}
	}
);
