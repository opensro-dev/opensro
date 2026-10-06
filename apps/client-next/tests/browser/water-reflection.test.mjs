/*
===========================================================================

water-reflection.test.mjs - planar water capture through the production renderer

===========================================================================
*/
import { test } from "node:test";
import { writeFile, mkdir } from "node:fs/promises";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

for ( const above of [ true, false ] ) {
	for ( const bump of [ false, true ] ) {
		test(
			`native water projection ${
				above ? "above" : "below"
			} the surface, bump=${bump}, survives off/on transitions`,
			{
				timeout: 60000
			},
			async () => {
				const { browser, page } = await launchProbeBrowser();
				try {
					await holdProbeRuntime( page );
					await page.goto( CLIENT_NEXT_BASE_URL );
					const result = await page.evaluate( async ( { above, bump } ) => {
						const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
						const { defaultVideoOptions } = await import(
							"/src/engine/foundation/rendering/video-options.ts"
						);
						const canvas = document.createElement( "canvas" ), renderer = createRenderer( canvas );
						const copy = document.createElement( "canvas" );
						copy.width = copy.height = 128;
						const context = copy.getContext( "2d", { willReadFrequently: true } );
						if ( !context ) throw Error( "Canvas readback unavailable" );
						const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
						const group = ( id, positions, color, water = false ) => ({
							id,
							center: [ 0, water ? 0 : 1, 2 ],
							radius: 20,
							material: {
								color,
								water,
								alphaCutoff: 0,
								blend: water,
								doubleSided: water,
								unlit: true,
								fogDisabled: true
							},
							geometry: {
								positions: new Float32Array( positions ),
								indices: new Uint32Array( water ? [ 0, 1, 2, 0, 2, 3 ] : [ 0, 2, 1, 0, 3, 2 ] ),
								normals: new Float32Array( 12 ).fill( 1 ),
								uvs: new Float32Array( 8 ),
								colors: new Float32Array( 16 ).fill( 1 ),
								transform: identity(),
								instances: identity(),
								world: true
							}
						});
						renderer.setWorld( {
							id: "water-proof",
							waterBump: bump ? "native-bump" : undefined,
							originRegion: 257,
							warnings: [],
							groups: [
								group( "above", [ -1, .5, 3, 1, .5, 3, 1, 2.5, 3, -1, 2.5, 3 ], [ 1, 0, 0, 1 ] ),
								group( "below", [
									-1,
									-2.5,
									above ? 3 : -2,
									1,
									-2.5,
									above ? 3 : -2,
									1,
									-.5,
									above ? 3 : -2,
									-1,
									-.5,
									above ? 3 : -2
								], [ 0, 0, 1, 1 ] ),
								group(
									"water",
									[ -10, 0, -3, 10, 0, -3, 10, 0, 12, -10, 0, 12 ],
									[ .1, .1, .1, 1 ],
									true
								)
							]
						} );
						if ( bump ) {
							renderer.setWorldTexture(
								"native-bump",
								await createImageBitmap(
									new ImageData( Uint8ClampedArray.of( 128, 136, 0, 255 ), 1, 1 )
								)
							);
						}
						renderer.setWorldCamera( {
							eye: [ 0, above ? 3 : -3, -5 ],
							target: [ 0, 0, 3 ],
							originRegion: 257,
							near: .1,
							far: 100,
							fov: 1
						} );
						while ( renderer.phase() === "starting" ) await new Promise( requestAnimationFrame );
						if ( renderer.error() ) throw Error( renderer.error() );
						const counts = [];
						try {
							for ( const enabled of [ false, true, false, true ] ) {
								const options = defaultVideoOptions();
								renderer.videoOptions( {
									...options,
									records: options.records.map( row =>
										row.map( ( v, i ) => i === 4 ? Number( enabled ) : v )
									)
								} );
								for ( let frame = 0; frame < 12; frame++ ) {
									renderer.frame( { width: 128, height: 128 }, frame / 60 );
									if ( renderer.error() ) throw Error( renderer.error() );
									await new Promise( requestAnimationFrame );
								}
								const bitmap = await createImageBitmap( canvas );
								context.drawImage( bitmap, 0, 0 );
								bitmap.close();
								const pixels = context.getImageData( 0, above ? 64 : 0, 128, 64 ).data;
								let red = 0, blue = 0;
								for ( let i = 0; i < pixels.length; i += 4 ) {
									if ( pixels[i] > pixels[i + 1] + 30 && pixels[i] > pixels[i + 2] + 30 ) red++;
								}
								for ( let i = 0; i < pixels.length; i += 4 ) {
									if ( pixels[i + 2] > pixels[i] + 30 && pixels[i + 2] > pixels[i + 1] + 30 ) blue++;
								}
								if ( above ? blue : red ) {
									throw Error( "Opposite side leaked through the capture clip plane" );
								}
								counts.push( above ? red : blue );
								if ( enabled ) {
									// Independent ray/quad oracle for the native camera-space texture matrix.
									// Neither renderer matrix helper nor shader output supplies these coordinates.
									const eyeY = above ? 3 : -3, length = Math.hypot( 3, 8 ), tangent = Math.tan( .5 );
									const fy = -eyeY / length, fz = 8 / length, uy = fz, uz = -fy;
									let checked = 0, expectedColored = 0, wrong = 0;
									for ( let py = 2; py < 64; py += 4 ) {
										for ( let px = 2; px < 128; px += 4 ) {
											const sy = py + (above ? 64 : 0),
												nx = (px + .5) / 64 - 1,
												ny = 1 - (sy + .5) / 64;
											const dy = fy + ny * tangent * uy,
												dz = fz + ny * tangent * uz,
												distance = -eyeY / dy;
											const x = distance * nx * tangent, z = -5 + distance * dz;
											if (
												distance <= 0 || Math.abs( x ) > 9.5 || z < -2.5 || z > 11.5
											) continue;
											// Exclude direct, foreground visibility of the submerged test panel.
											if ( !above ) {
												const front = 3 / dz,
													frontX = front * nx * tangent,
													frontY = eyeY + front * dy;
												if (
													front < distance && Math.abs( frontX ) < 1.08 && frontY > -2.58 &&
													frontY < -.42
												) continue;
											}
											const cameraY = -eyeY * uy + (z + 5) * uz,
												cameraZ = -eyeY * fy + (z + 5) * fz;
											const angle = Math.fround( Math.fround( 11 / 60 * .5 ) * 9 );
											const bumpU = bump ? Math.fround( Math.sin( angle ) ) * -.04 * 8 / 127 : 0;
											const bumpV = bump ? Math.fround( Math.cos( angle ) ) * 8 / 127 : 0;
											const u = .6499999761581421 * x / cameraZ + .5 + bumpU;
											const v = (above ? 1 : -1) *
													(.800000011920929 * cameraY / cameraZ - .5199999809265137) + bumpV;
											const tx = (u - Math.floor( u )) * 2 - 1,
												ty = 1 - (v - Math.floor( v )) * 2;
											// Above water, native negates orbit pitch; underwater it leaves the camera alone.
											const captureFy = 3 / length, captureUz = -3 / length;
											const t = (above ? 8 : 3) / (fz + ty * tangent * captureUz);
											const hitX = t * tx * tangent,
												hitY = -3 + t * (captureFy + ty * tangent * uy);
											const low = above ? .5 : -2.5, high = above ? 2.5 : -.5;
											if (
												Math.abs( Math.abs( hitX ) - 1 ) < .08 ||
												Math.abs( hitY - low ) < .08 ||
												Math.abs( hitY - high ) < .08
											) continue;
											const expected = Math.abs( hitX ) < 1 && hitY > low && hitY < high;
											const at = (py * 128 + px) * 4, channel = above ? 0 : 2;
											const actual = pixels[at + channel] > pixels[at + 1] + 30;
											checked++;
											expectedColored += Number( expected );
											wrong += Number( actual !== expected );
										}
									}
									if ( checked < 100 || expectedColored < 3 || wrong > 2 ) {
										throw Error( JSON.stringify( { above, checked, expectedColored, wrong } ) );
									}
								}
							}
							return { counts, image: copy.toDataURL(), error: renderer.error() };
						} finally {
							renderer.dispose();
						}
					}, { above, bump } );
					await mkdir( "../../.state/water", { recursive: true } );
					await writeFile(
						`../../.state/water/reflection-${above ? "above" : "below"}-${bump ? "bump" : "flat"}.png`,
						Buffer.from( result.image.split( "," )[1], "base64" )
					);
					const counts = result.counts;
					assert.ok( counts[1] > counts[0] + 100, JSON.stringify( counts ) );
					assert.equal( counts[2], counts[0] );
					assert.equal( counts[3], counts[1] );
				} finally {
					await browser.close();
				}
			}
		);
	}
}
