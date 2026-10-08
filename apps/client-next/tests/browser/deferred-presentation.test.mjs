/*
===========================================================================

deferred-presentation.test.mjs - retained frames across real browser turns

Exercise production surface acquisition and a depth-compatible HUD bundle
after an asynchronous query boundary has expired earlier canvas textures.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "deferred presentation survives browser turns and submits the visible HUD once", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.route( CLIENT_NEXT_BASE_URL, route =>
			route.fulfill( {
				contentType: "text/html",
				body: "<!doctype html><html><body></body></html>"
			} ) );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
			const { createFrame } = await import( "/src/engine/runtime/renderer/frame/frame.ts" );
			const { createSurface } = await import( "/src/engine/runtime/renderer/surface/surface.ts" );
			const SIZE = 64, READY_TIMEOUT_MS = 15000;
			const device = createDevice();
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			let surface;
			try {
				const deadline = performance.now() + READY_TIMEOUT_MS;
				while ( device.phase() === "starting" ) {
					if ( performance.now() > deadline ) throw Error( "Device timeout" );
					await new Promise( requestAnimationFrame );
				}
				if ( device.error() ) throw Error( device.error() );
				const commands = device.commands(), surfaceCommands = device.surfaceCommands();
				if ( !commands || !surfaceCommands ) throw Error( "Device commands unavailable" );
				surface = createSurface( canvas, surfaceCommands, device.format() );
				let submissions = 0, browserTurns = 0;
				const frame = createFrame( {
					...commands,
					/*
					================
					submit
					================
					*/
					submit( buffer ) {
						submissions++;
						commands.submit( buffer );
					}
				} );
				device.beginFrame();
				try {
					const color = surface.acquire( { width: SIZE, height: SIZE }, true );
					const ui = device.ui( {
						revision: 1,
						width: SIZE,
						height: SIZE,
						quads: [ {
							rect: [ 8, 8, 48, 48 ],
							clip: [ 0, 0, SIZE, SIZE ],
							uv: [ 0, 0, 1, 1 ],
							texture: "",
							color: [ 1, 0, 0, 1 ]
						} ]
					} );
					const pending = frame.draw(
						color,
						undefined,
						undefined,
						surface.depth(),
						[],
						ui,
						[],
						undefined,
						undefined,
						undefined,
						undefined,
						[],
						undefined,
						{
							asynchronous: true,
							/*
							================
							prepare

							Two animation frames cross the swapchain expiry boundary.
							================
							*/
							async prepare() {
								await new Promise( requestAnimationFrame );
								browserTurns++;
								await new Promise( requestAnimationFrame );
								browserTurns++;
								return [];
							}
						},
						undefined,
						undefined,
						surface
					);
					const before = { submissions, browserTurns };
					await pending;
					const copy = document.createElement( "canvas" );
					copy.width = copy.height = SIZE;
					const context = copy.getContext( "2d" );
					if ( !context ) throw Error( "No capture context" );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					return {
						before,
						submissions,
						browserTurns,
						center: [ ...context.getImageData( SIZE / 2, SIZE / 2, 1, 1 ).data ],
						corner: [ ...context.getImageData( 0, 0, 1, 1 ).data ],
						error: device.error()
					};
				} finally {
					device.endFrame();
				}
			} finally {
				surface?.dispose();
				device.dispose();
				canvas.remove();
			}
		} );
		assert.deepEqual( result.before, { submissions: 1, browserTurns: 0 } );
		assert.equal( result.browserTurns, 2 );
		assert.equal( result.submissions, 2, "Presentation shares the deferred tail submission" );
		assert.deepEqual( result.center, [ 255, 0, 0, 255 ] );
		assert.notDeepEqual( result.corner, result.center );
		assert.equal( result.corner[3], 255 );
		assert.equal( result.error, null );
	} finally {
		await browser.close();
	}
} );
