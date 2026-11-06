/*
===========================================================================

dungeon-fog.test.mjs - a dungeon block's fog follows the retail shader curve

Draws one white quad 2000 units from the camera through the real shader. The
tomb's density leaves it almost clear, where the linear ramp over the block's
near and far planes turned it solid fog colour.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";

test( "block fog is exp2 by density, height-limited, and water stays linear", {
	timeout: 45000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { identity } = await import( "/src/engine/foundation/rendering/world-math.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const path = "/assets/dungeon-fog-fixture";
			const distance = 2000, half = 4000;
			const group = ( fog, z ) => ({
				id: "dungeon-fog",
				dungeonBlock: 0,
				center: [ 0, 0, z ],
				radius: half * 2,
				material: {
					color: [ 1, 1, 1, 1 ],
					texture: path,
					fog,
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
						z,
						half,
						-half,
						z,
						half,
						half,
						z,
						-half,
						half,
						z
					] ),
					normals: new Float32Array( [ 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1 ] ),
					uvs: new Float32Array( 8 ),
					indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
					instances: identity(),
					transform: identity()
				}
			});
			const camera = {
				eye: [ 0, 0, 0 ],
				target: [ 0, 0, distance ],
				up: [ 0, 1, 0 ],
				fov: Math.PI / 2,
				near: 1,
				far: 10000,
				dungeonBlock: 0
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
			const scene = ( id, fog, z = distance ) => ({
				id,
				originRegion: 0x8007,
				dungeonVisibility: [ [ 0 ] ],
				warnings: [],
				groups: [ group( fog, z ) ]
			});
			const show = async fog => {
				renderer.setWorld( scene( "fog-" + JSON.stringify( fog ), fog ) );
				return center();
			};
			try {
				renderer.setWorldCamera( camera );
				renderer.setWorld( scene( "fog-load", undefined ) );
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
				const fog = { color: 0xff000000, nearPlane: -200, farPlane: 1000, intensity: .00014 };
				return {
					clear: await show( undefined ),
					thin: await show( fog ),
					dense: await show( { ...fog, intensity: .001 } ),
					height: await show( { ...fog, heightFog: { unk3: 1000, unk4: 0, unk5: 1, unk6: .0005 } } ),
					water: await show( { ...fog, linear: true } ),
					error: renderer.error()
				};
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		// Black fog leaves the surface scaled by oFog, measured against no fog at all.
		const kept = pixel => pixel[0] / result.clear[0];
		assert.ok( result.clear[0] > 64, String( result.clear ) );
		// 2 ^ -( 2000 * .00014 ) ^ 2 keeps .947 of the surface; .001 keeps 2 ^ -4.
		assert.ok( Math.abs( kept( result.thin ) - .947 ) < .03, String( result.thin ) );
		assert.ok( Math.abs( kept( result.dense ) - .0625 ) < .03, String( result.dense ) );
		// y 0 lies 1000 from the band's centre: 1 - 1000 * .0005 keeps half.
		assert.ok( Math.abs( kept( result.height ) - .5 ) < .03, String( result.height ) );
		// Past the far plane the linear ramp is solid fog.
		assert.ok( kept( result.water ) < .03, String( result.water ) );
	} finally {
		await browser.close();
	}
} );
