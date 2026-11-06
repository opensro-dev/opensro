/*
===========================================================================

dungeon-camera.test.mjs - no far plane slices a long dungeon room

Draws one white quad 6000 units from a camera whose far plane is 3500, the
length of hall a tomb floor shows from one end. A dungeon scene draws it;
clipped, the pixel would be the empty clear.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";

test( "a dungeon draws past the background distance", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { identity } = await import( "/src/engine/foundation/rendering/world-math.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const path = "/assets/dungeon-camera-fixture";
			const distance = 6000, half = 8000;
			const group = {
				id: "dungeon-camera",
				dungeonBlock: 0,
				center: [ 0, 0, distance ],
				radius: half * 2,
				material: {
					color: [ 1, 1, 1, 1 ],
					texture: path,
					alphaCutoff: 0,
					blend: false,
					doubleSided: true,
					unlit: true
				},
				geometry: {
					world: true,
					positions: new Float32Array( [
						-half,
						-half,
						distance,
						half,
						-half,
						distance,
						half,
						half,
						distance,
						-half,
						half,
						distance
					] ),
					normals: new Float32Array( [ 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1 ] ),
					uvs: new Float32Array( 8 ),
					indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
					instances: identity(),
					transform: identity()
				}
			};
			let seconds = 0;
			const center = async () => {
				for ( let i = 0; i < 3; i++ ) {
					renderer.frame( { width: 64, height: 64 }, seconds += .1 );
					await new Promise( requestAnimationFrame );
				}
				renderer.frame( { width: 64, height: 64 }, seconds += .1 );
				const copy = document.createElement( "canvas" );
				copy.width = copy.height = 64;
				const context = copy.getContext( "2d", { willReadFrequently: true } );
				if ( !context ) throw Error( "No 2D context" );
				context.drawImage( canvas, 0, 0, 64, 64 );
				return [ ...context.getImageData( 32, 32, 1, 1 ).data ];
			};
			try {
				renderer.setWorldCamera( {
					eye: [ 0, 0, 0 ],
					target: [ 0, 0, distance ],
					up: [ 0, 1, 0 ],
					fov: Math.PI / 2,
					near: 1,
					far: 3500,
					dungeonBlock: 0
				} );
				renderer.setWorld( {
					id: "dungeon-camera",
					originRegion: 0x8007,
					dungeonVisibility: [ [ 0 ] ],
					warnings: [],
					groups: [ group ]
				} );
				renderer.setWorldTexture(
					path,
					await createImageBitmap(
						new ImageData( Uint8ClampedArray.of( 255, 255, 255, 255 ), 1, 1 ),
						{ premultiplyAlpha: "none" }
					)
				);
				for ( let i = 0; i < 300 && renderer.phase() === "starting"; i++ ) {
					await new Promise( requestAnimationFrame );
				}
				return { pixel: await center(), error: renderer.error() };
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		assert.ok( result.pixel[0] > 64, String( result.pixel ) );
	} finally {
		await browser.close();
	}
} );
