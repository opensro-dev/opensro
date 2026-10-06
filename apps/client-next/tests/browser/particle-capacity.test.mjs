/*
===========================================================================

particle-capacity.test.mjs - real GPU retirement of retained particle rows

An opaque particle must disappear when its actor leaves a padded batch.
Replacement actors must become visible without stale geometry at the origin.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"opaque GPU particle rows disappear and return across retained batch membership",
	{ timeout: 45000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.goto( CLIENT_NEXT_BASE_URL + "/assets/skillfx/manifest.json" );
			const result = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const canvas = document.createElement( "canvas" );
				document.body.append( canvas );
				const renderer = createRenderer( canvas );
				/*
			================
			identity
			================
			*/
				const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
				const model = {
					nodes: [ {
						name: "root",
						parent: -1,
						translation: [ 0, 0, 0 ],
						rotation: [ 0, 0, 0, 1 ],
						scale: [ 1, 1, 1 ]
					} ],
					images: [],
					clips: [ { name: "effect", duration: 10, channels: [] } ],
					primitives: [ {
						name: "particle",
						node: 0,
						joints: [ 0 ],
						inverseBind: identity(),
						image: -1,
						emission: { births: [ 0 ], lifetime: 10 },
						geometry: {
							positions: Float32Array.of( -2, -2, 0, 2, -2, 0, 2, 2, 0, -2, 2, 0 ),
							indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
							transform: identity(),
							joints: new Uint32Array( 16 ),
							weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
							material: {
								color: [ 1, 0, 0, 1 ],
								alphaCutoff: 0,
								blend: false,
								doubleSided: true,
								unlit: true
							}
						}
					} ]
				};
				const copy = document.createElement( "canvas" );
				copy.width = copy.height = 64;
				const context = copy.getContext( "2d" );
				if ( !context ) throw Error( "Canvas readback unavailable" );
				/*
			================
			sample
			================
			*/
				async function sample( ids, time ) {
					if ( !context ) throw Error( "Canvas readback unavailable" );
					renderer.setCharacterActors( ids.map( gid => ({
						gid,
						model: "particle",
						clip: "effect",
						time,
						scale: 1,
						loop: false,
						pose: { regionId: 257, x: gid >= 4 ? 0 : gid === 1 ? -20 : gid * 20, y: 0, z: 0, yaw: 0 }
					}) ) );
					await renderer.frame( { width: 64, height: 64 }, time );
					if ( renderer.error() ) throw Error( renderer.error() );
					const bitmap = await createImageBitmap( canvas );
					context.drawImage( bitmap, 0, 0 );
					bitmap.close();
					return [ ...context.getImageData( 32, 32, 1, 1 ).data ];
				}
				try {
					const deadline = performance.now() + 15000;
					while ( renderer.phase() === "starting" ) {
						if ( performance.now() > deadline ) throw Error( "GPU admission timed out" );
						await new Promise( requestAnimationFrame );
					}
					renderer.setWorld( { id: "particle-capacity", originRegion: 257, groups: [], warnings: [] } );
					renderer.setWorldCamera( {
						originRegion: 257,
						eye: [ 0, 0, 30 ],
						target: [ 0, 0, 0 ],
						fov: 1,
						near: 1,
						far: 100
					} );
					renderer.setCharacterModel( "particle", model, [] );
					return {
						empty: await sample( [ 1, 2, 3 ], .1 ),
						present: await sample( [ 1, 2, 3, 4 ], .2 ),
						removed: await sample( [ 1, 2, 3 ], .3 ),
						replaced: await sample( [ 1, 2, 3, 5 ], .4 )
					};
				} finally {
					renderer.dispose();
					canvas.remove();
				}
			} );
			assert.deepEqual( result.present, [ 255, 0, 0, 255 ] );
			assert.deepEqual( result.removed, result.empty );
			assert.deepEqual( result.replaced, result.present );
		} finally {
			await browser.close();
		}
	}
);
