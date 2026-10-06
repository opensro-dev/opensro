/*
===========================================================================

cloth.test.mjs - dynamic scenery through the production renderer

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

for ( const kind of [ "scenery", "character" ] ) {
	test(
		`Dynamic Animation moves ${kind} cloth and restores ordinary geometry when disabled`,
		{ timeout: 60000 },
		async () => {
			const { browser, page } = await launchProbeBrowser();
			try {
				await holdProbeRuntime( page );
				await page.goto( CLIENT_NEXT_BASE_URL );
				const images = await page.evaluate( async ( kind ) => {
					const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
					const { defaultVideoOptions, changeVideo } = await import(
						"/src/engine/foundation/rendering/video-options.ts"
					);
					const { identity } = await import( "/src/engine/foundation/rendering/world-math.ts" );
					const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
					const canvas = document.createElement( "canvas" ),
						renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
					const copy = document.createElement( "canvas" );
					copy.width = copy.height = 128;
					const context = copy.getContext( "2d", { willReadFrequently: true } );
					if ( !context ) throw Error( "Canvas readback unavailable" );
					const cloth = {
						mobility: [ 1, 1, 0, 0 ],
						pins: [ 0, 0, 1, 1 ],
						constraints: [ [ 2, 1, 2 ], [ 3, 0, 2 ], [ 0, 1, 2 ] ],
						order: [ 0, 1, 2 ],
						force: [ 1, 0, 0 ],
						gravity: 20,
						gravityMobility: 0,
						windMobility: 0,
						damping: .9,
						windPeriod: 1
					};
					const world = {
						id: "cloth-proof",
						originRegion: 257,
						warnings: [],
						groups: [ {
							id: "flag",
							center: [ 0, 1, 3 ],
							radius: 3,
							material: {
								color: [ 1, 0, 0, 1 ],
								alphaCutoff: 0,
								blend: false,
								doubleSided: true,
								unlit: true,
								fogDisabled: true
							},
							geometry: {
								cloth,
								positions: new Float32Array( [ -1, 0, 3, 1, 0, 3, 1, 2, 3, -1, 2, 3 ] ),
								normals: new Float32Array( 12 ).fill( 1 ),
								uvs: new Float32Array( 8 ),
								indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
								transform: identity(),
								instances: identity(),
								world: true
							}
						} ]
					};
					if ( kind === "scenery" ) renderer.setWorld( world );
					else {
						const group = world.groups[0];
						renderer.setWorld( { ...world, groups: [] } );
						renderer.setCharacterModel( "cloth", {
							nodes: [ {
								name: "root",
								parent: -1,
								translation: [ 0, 0, 0 ],
								rotation: [ 0, 0, 0, 1 ],
								scale: [ 1, 1, 1 ]
							} ],
							clips: [],
							images: [],
							primitives: [ {
								name: "cloth",
								node: 0,
								joints: [ 0 ],
								inverseBind: identity(),
								image: -1,
								cloth,
								geometry: {
									...group.geometry,
									material: group.material,
									joints: new Uint32Array( 16 ),
									weights: new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ] )
								}
							} ]
						}, [] );
						renderer.setCharacterActors( [ {
							gid: 1,
							model: "cloth",
							pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
							clip: "",
							time: 0,
							loop: true,
							scale: 1
						} ] );
					}

					renderer.setWorldCamera( {
						eye: [ 0, 1, -5 ],
						target: [ 0, 1, 3 ],
						originRegion: 257,
						near: .1,
						far: 100,
						fov: 1
					} );
					while ( renderer.phase() === "starting" ) await new Promise( requestAnimationFrame );
					if ( renderer.error() ) throw Error( renderer.error() );
					const result = [];
					let frame = 0;
					try {
						for ( const enabled of [ false, true, false, true ] ) {
							renderer.videoOptions( changeVideo( defaultVideoOptions(), 12, Number( enabled ) ) );
							for ( let i = 0; i < 40; i++, frame++ ) {
								renderer.frame( { width: 128, height: 128 }, frame * .05 );
								if ( renderer.error() ) throw Error( renderer.error() );
								await new Promise( requestAnimationFrame );
							}
							const bitmap = await createImageBitmap( canvas );
							context.drawImage( bitmap, 0, 0 );
							bitmap.close();
							const pixels = context.getImageData( 0, 0, 128, 128 ).data;
							let area = 0, x = 0;
							for ( let i = 0; i < pixels.length; i += 4 ) {
								if ( pixels[i] > pixels[i + 1] + 30 && pixels[i] > pixels[i + 2] + 30 ) {
									area++;
									x += (i / 4) % 128;
								}
							}
							result.push( { area, x: x / area } );
						}
						return result;
					} finally {
						renderer.dispose();
					}
				}, kind );
				assert.ok( images[0].area > 100, JSON.stringify( images ) );
				assert.ok( Math.abs( images[1].x - images[0].x ) > 2, JSON.stringify( images ) );
				assert.deepEqual( images[2], images[0] );
				assert.ok( Math.abs( images[3].x - images[2].x ) > 2, JSON.stringify( images ) );
			} finally {
				await browser.close();
			}
		}
	);
}
