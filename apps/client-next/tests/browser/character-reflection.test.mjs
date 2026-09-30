/*
===========================================================================

character-reflection.test.mjs - native metal reflection on characters

Renders a metal stage with and without native lighting in a real browser
and checks texture alpha, environment tint and the fade retirement.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime, enableNativeCharacterLighting } from "./helpers/hold-runtime.mjs";

for ( const enabled of [ false, true ] ) {
	test(
		`lighting ${
			enabled ? "enabled" : "disabled"
		}: native metal stages combine texture alpha, environment tint and vertex lighting; fades retire reflection`,
		{ timeout: 60000 },
		async () => {
			const { browser, page } = await launchProbeBrowser();
			try {
				page.on( "console", message => {
					if ( [ "error", "warning" ].includes( message.type() ) ) console.error( message.text() );
				} );
				await holdProbeRuntime( page );
				if ( enabled ) await enableNativeCharacterLighting( page );
				await page.goto( CLIENT_NEXT_BASE_URL );
				const result = await page.evaluate( async () => {
					const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
					const { createHitLights } = await import( "/src/engine/foundation/animation/hit-light.ts" );
					const { defaultVideoOptions } = await import( "/src/engine/foundation/rendering/video-options.ts" );
					const canvas = document.createElement( "canvas" ),
						renderer = createRenderer( canvas ),
						copy = document.createElement( "canvas" );
					copy.width = copy.height = 128;
					const ctx = copy.getContext( "2d" );
					const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
					const bitmap = async rgba =>
						createImageBitmap( new ImageData( Uint8ClampedArray.from( rgba ), 1, 1 ) );
					const material = {
						color: [ 1, 1, 1, 1 ],
						ambient: [ 1, 1, 1 ],
						objectLight: 1,
						stageFactor: 2,
						alphaCutoff: 1 / 255,
						blend: false,
						doubleSided: true,
						unlit: false,
						textureAlpha: true,
						fadeAlphaOnly: true,
						environmentReflection: true,
						fogDisabled: true
					};
					const model = {
						nodes: [ {
							name: "root",
							parent: -1,
							translation: [ 0, 0, 0 ],
							rotation: [ 0, 0, 0, 1 ],
							scale: [ 1, 1, 1 ]
						} ],
						primitives: [ {
							name: "metal",
							node: 0,
							joints: [ 0 ],
							inverseBind: identity(),
							image: 0,
							environmentImage: 1,
							geometry: {
								positions: Float32Array.of( -20, -20, 0, 20, -20, 0, 0, 20, 0 ),
								normals: Float32Array.of( 0, 0, -1, 0, 0, -1, 0, 0, -1 ),
								uvs: new Float32Array( 6 ),
								indices: Uint32Array.of( 0, 1, 2 ),
								joints: new Uint32Array( 12 ),
								weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
								transform: identity(),
								material
							}
						} ],
						clips: [],
						images: [ { width: 1, height: 1 }, { width: 1, height: 1 } ]
					};
					const deadline = performance.now() + 20000;
					while ( renderer.phase() === "starting" ) {
						if ( performance.now() > deadline ) throw Error( "GPU startup timeout" );
						await new Promise( requestAnimationFrame );
					}
					renderer.setCharacterModel( "metal", model, [
						await bitmap( [ 20, 30, 40, 128 ] ),
						await bitmap( [ 100, 80, 60, 255 ] )
					] );
					renderer.setWorldCamera( {
						eye: [ 0, 0, -80 ],
						target: [ 0, 0, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 500
					} );
					let serial = 0;
					async function sample(
						on,
						opacity = 1,
						tint,
						modelId = "metal",
						cameraZ = -80,
						pointLight,
						globalReflection = 1
					) {
						const options = defaultVideoOptions();
						renderer.videoOptions( {
							...options,
							records: options.records.map( row => row.map( ( v, i ) => i === 6 ? Number( on ) : v ) )
						} );
						renderer.setWorld( null );
						const rgb = n => [ { t: 0, r: n, g: n, b: n } ];
						renderer.setWorld( {
							id: "reflection:" + serial++,
							originRegion: 257,
							groups: [],
							warnings: [],
							environment: {
								startTimeOfDay: .5,
								ratePerSecond: 0,
								tracks: {
									zenith: rgb( 0 ),
									horizon: rgb( 0 ),
									color0xf0: rgb( globalReflection ),
									color0x124: rgb( .25 )
								}
							}
						} );
						renderer.setWorldCamera( {
							eye: [ 0, 0, cameraZ ],
							target: [ 0, 0, 0 ],
							originRegion: 257,
							fov: 1,
							near: 1,
							far: 500
						} );
						renderer.setCharacterActors( [ {
							gid: 1,
							model: modelId,
							pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
							clip: "",
							time: 0,
							loop: true,
							scale: 1,
							opacity,
							materialTint: tint,
							pointLight
						} ] );
						for ( let i = 0; i < 12; i++ ) {
							renderer.frame( { width: 128, height: 128 }, 0 );
							if ( renderer.error() ) throw Error( renderer.error() );
							await new Promise( requestAnimationFrame );
						}
						const image = await createImageBitmap( canvas );
						ctx.drawImage( image, 0, 0 );
						image.close();
						return [ ...ctx.getImageData( 64, 64, 1, 1 ).data ];
					}
					try {
						const result = {
							on: await sample( true ),
							off: await sample( false ),
							fadingOn: await sample( true, .5 ),
							fadingOff: await sample( false, .5 ),
							tint: await sample( true, 1, [ 1, .5, .25 ] )
						};
						const pose = { regionId: 257, x: 0, y: 0, z: -80 },
							zeroLight = {
								pose,
								ambient: [ 0, 0, 0 ],
								diffuse: [ 0, 0, 0 ],
								range: 1000,
								attenuation: .2
							};
						result.zeroPoint = await sample( true, 1, undefined, "metal", -80, zeroLight, .1 );
						result.dimIdle = await sample( true, 1, undefined, "metal", -80, undefined, .1 );
						const hit = createHitLights();
						hit.start( 1, { color: [ .6, .2, .1 ], duration: .3, range: 1000, attenuation: .2 }, pose, 0 );
						result.hitStart = await sample( true, 1, undefined, "metal", -80, hit.get( 1 ), .1 );
						hit.step( .15, new Set( [ 1 ] ) );
						result.hitTick = await sample( true, 1, undefined, "metal", -80, hit.get( 1 ), .1 );
						hit.step( .31, new Set( [ 1 ] ) );
						result.hitExpired = await sample( true, 1, undefined, "metal", -80, hit.get( 1 ), .1 );
						const gradient = Uint8ClampedArray.from( [
							0,
							0,
							0,
							255,
							64,
							64,
							64,
							255,
							128,
							128,
							128,
							255,
							192,
							192,
							192,
							255
						] );
						const mapped = structuredClone( model );
						mapped.images[1] = { width: 4, height: 1 };
						mapped.primitives[0].geometry.normals = Float32Array.from(
							Array.from( { length: 3 }, () => [ -.3125, -.3125, -Math.sqrt( 1 - 2 * .3125 ** 2 ) ] )
								.flat()
						);
						renderer.setCharacterModel( "mapped", mapped, [
							await bitmap( [ 0, 0, 0, 255 ] ),
							await createImageBitmap( new ImageData( gradient, 4, 1 ) )
						] );
						result.front = await sample( true, 1, undefined, "mapped" );
						result.back = await sample( true, 1, undefined, "mapped", 80 );
						return result;
					} finally {
						renderer.dispose();
					}
				} );
				await mkdir( "temp/artifacts/character-reflection", { recursive: true } );
				await writeFile(
					`temp/artifacts/character-reflection/raster-${enabled}.json`,
					JSON.stringify( result, null, 2 )
				);
				for ( let c = 0; c < 3; c++ ) {
					assert.ok(
						Math.abs( result.on[c] - (enabled ? [ 35, 35, 35 ] : [ 10, 15, 20 ])[c] ) <= 1,
						JSON.stringify( result )
					);
					assert.ok( Math.abs( result.off[c] - [ 10, 15, 20 ][c] ) <= 1, JSON.stringify( result ) );
				}
				assert.deepEqual(
					result.fadingOn,
					result.fadingOff,
					"retail opacity gate must disable RGB reflection independently of coverage"
				);
				for ( let c = 0; c < 3; c++ ) {
					assert.ok(
						Math.abs( result.tint[c] - result.on[c] * [ 1, .5, .25 ][c] ) <= 1,
						"SCT_MAT changes lighting, not sphere factor: " + JSON.stringify( result )
					);
				}
				if ( !enabled ) {
					assert.deepEqual( result.on, result.off, "saved Metal Detail cannot override the build switch" );
				}
				assert.deepEqual(
					result.zeroPoint,
					result.dimIdle,
					"temporary hit light does not replace the dim global reflection factor"
				);
				for ( let c = 0; c < 3; c++ ) {
					assert.ok(
						Math.abs( result.hitStart[c] - (enabled ? [ 14, 18, 22 ] : result.dimIdle)[c] ) <= 1,
						"hit-start reflection uses global factor: " + JSON.stringify( result )
					);
					assert.ok(
						Math.abs( result.hitTick[c] - (enabled ? [ 23, 22, 25 ] : result.dimIdle)[c] ) <= 1,
						"hit-tick reflection uses global factor: " + JSON.stringify( result )
					);
				}
				assert.deepEqual(
					result.hitExpired,
					result.dimIdle,
					"hit-light expiry restores the original lighting"
				);
				for ( let c = 0; c < 3; c++ ) {
					assert.ok( Math.abs( result.front[c] - (enabled ? 16 : 0) ) <= 1, JSON.stringify( result ) );
					assert.ok( Math.abs( result.back[c] - (enabled ? 80 : 0) ) <= 1, JSON.stringify( result ) );
				}
			} finally {
				await browser.close();
			}
		}
	);
}
