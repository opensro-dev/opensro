/*
===========================================================================

staff-nameplate-fx.test.mjs - the staff nameplate layers on the GPU

Port-only, not native (owner 2026-11-28). The production ui-quads shader
animates a staff board's layers from the device clock: the halo is a
soft round glow that keeps its gold at any time (only its strength
breathes), and a spark rises. A quad without a layer, the name among
them, draws exactly as before at any time, so the native board is
untouched.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "the halo keeps its gold, a spark rises and a plain quad stays still", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { deviceScaleFactor: 1 } );
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createUiResources } = await import( "/src/engine/runtime/renderer/device/ui.ts" );
			const { staffNameplateAura, staffNameplateSparks } = await import(
				"/src/engine/foundation/ui/staff-nameplate.ts"
			);
			const adapter = await navigator.gpu.requestAdapter();
			if ( !adapter ) throw Error( "WebGPU adapter unavailable" );
			const device = await adapter.requestDevice(),
				ui = createUiResources( device, "rgba8unorm", () => "rgba8unorm" );
			const errors = [];
			device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
			await ui.ready;
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

			One scene at clock seconds; the device reads performance.now.
			================
			*/
			async function render( quads, seconds ) {
				performance.now = () => seconds * 1000;
				const draws = ui.prepare( { revision: ++revision, width: WIDTH, height: HEIGHT, quads }, WIDTH );
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
				return rgba;
			}
			/*
			================
			average

			The mean RGB and the summed brightness inside a rect.
			================
			*/
			function average( rgba, [x, y, w, h] ) {
				const sum = [ 0, 0, 0 ];
				let n = 0;
				for ( let j = y; j < y + h; j++ ) {
					for ( let i = x; i < x + w; i++ ) {
						const at = (j * WIDTH + i) * 4;
						for ( let c = 0; c < 3; c++ ) sum[c] += rgba[at + c];
						n++;
					}
				}
				return sum.map( v => v / n );
			}
			/*
			================
			centroidY

			The brightness-weighted row of everything drawn.
			================
			*/
			function centroidY( rgba ) {
				let weight = 0, rows = 0;
				for ( let j = 0; j < HEIGHT; j++ ) {
					for ( let i = 0; i < WIDTH; i++ ) {
						const at = (j * WIDTH + i) * 4, v = rgba[at] + rgba[at + 1] + rgba[at + 2];
						weight += v;
						rows += v * j;
					}
				}
				return weight ? rows / weight : -1;
			}
			const screen = [ 0, 0, WIDTH, HEIGHT ], white = [ 1, 1, 1, 1 ];
			const glyph = x => ({
				rect: [ x, 56, 8, 12 ],
				uv: [ 0, 0, 1, 1 ],
				color: white,
				clip: screen,
				texture: ""
			});
			const plain = [ 0, 1, 2, 3, 4, 5, 6, 7 ].map( i => glyph( 72 + i * 14 ) );
			const first = [ 72, 60, 8, 4 ];
			const plain1 = await render( plain, 1 ), plain2 = await render( plain, 1.7 );
			const board = [ 68, 54, 120, 16 ];
			const auraQuads = [ staffNameplateAura( board, screen, 1 ) ].map( ( { characterAnchor, ...q } ) => q );
			const aura = await render( auraQuads, 0.5 ), auraLater = await render( auraQuads, 3.2 );
			const auraCentre = [ 120, 58, 16, 8 ], auraCorner = [ 54, 46, 4, 3 ];
			const sparks = staffNameplateSparks( board, screen, 1 ).map( ( { characterAnchor, ...q } ) => q ).slice(
				0,
				1
			);
			const rise = [];
			for ( const seconds of [ 0, 0.3, 0.6, 0.9, 1.2 ] ) {
				rise.push( centroidY( await render( sparks, seconds ) ) );
			}
			return {
				errors,
				plain1: [ ...plain1 ].join( "," ) === [ ...plain2 ].join( "," ),
				plainFirst: average( plain1, first ),
				auraCentre: average( aura, auraCentre ),
				auraLater: average( auraLater, auraCentre ),
				auraCorner: average( aura, auraCorner ),
				rise
			};
		} );
		assert.deepEqual( result.errors, [] );
		// A plain quad is white and identical at any time.
		assert.equal( result.plain1, true );
		assert.deepEqual( result.plainFirst.map( Math.round ), [ 255, 255, 255 ] );
		// The halo is a soft round glow: bright in the middle, nothing in its corner.
		const brightness = c => c[0] + c[1] + c[2];
		assert.ok( brightness( result.auraCentre ) > 60, JSON.stringify( result ) );
		assert.ok( brightness( result.auraCorner ) < 5, JSON.stringify( result ) );
		// Its colour holds at any time: gold (red over green over blue) and
		// the same hue later; only its strength breathes.
		const hue = c => c.map( v => v / brightness( c ) );
		for ( const c of [ result.auraCentre, result.auraLater ] ) {
			assert.ok( c[0] > c[1] && c[1] > c[2], JSON.stringify( result ) );
		}
		const [h1, h2] = [ hue( result.auraCentre ), hue( result.auraLater ) ];
		assert.ok( Math.hypot( h1[0] - h2[0], h1[1] - h2[1], h1[2] - h2[2] ) < 0.03, JSON.stringify( result ) );
		// A spark rises: some later frame stands above the first.
		const shown = result.rise.filter( y => y >= 0 );
		assert.ok( shown.length >= 2 && Math.min( ...shown ) < shown[0] - 3, JSON.stringify( result.rise ) );
	} finally {
		await browser.close();
	}
} );
