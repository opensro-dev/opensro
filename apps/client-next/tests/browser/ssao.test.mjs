/*
===========================================================================

ssao.test.mjs - the ambient-occlusion stage in isolation

Port-only, not native. A ground plane with a box standing on it (the
contact shadow case): the Ambient occlusion row cycles off-on-off
through the production renderer. Off must restore the native bytes
exactly and run no stage pass; on must run the half-res convert,
trace and blur passes after the main pass, and the frame must darken
where the box meets the ground - the ambient term the row shades. The
capture waits one compositor frame after the submits (the capture
rule); the occlusion lags one frame by design, so the on mode renders
extra frames before its capture.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"the ambient occlusion row shades the contact and restores native bytes",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		try {
			await page.route(
				CLIENT_NEXT_BASE_URL + "/",
				route =>
					route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
			);
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.evaluate( () => {
				const trace = /** @type {any} */ (globalThis);
				trace.__gpuErrors = [];
				trace.__passes = [];
				const origRequest = GPUAdapter.prototype.requestDevice;
				GPUAdapter.prototype.requestDevice = async function( descriptor ) {
					const device = await origRequest.call( this, descriptor );
					device.addEventListener?.(
						"uncapturederror",
						event => trace.__gpuErrors.push( String( event.error?.message ?? event.error ).slice( 0, 200 ) )
					);
					return device;
				};
				const origPass = GPUCommandEncoder.prototype.beginRenderPass;
				GPUCommandEncoder.prototype.beginRenderPass = function( d ) {
					trace.__passes.push( d.label ?? "?" );
					return origPass.call( this, d );
				};
			} );
			const result = await page.evaluate( async () => {
				const trace = /** @type {any} */ (globalThis);
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
				const width = 192, height = 120;
				const canvas = document.createElement( "canvas" );
				const output = document.createElement( "canvas" );
				output.width = width;
				output.height = height;
				const context = output.getContext( "2d", { willReadFrequently: true } );
				if ( !context ) throw Error( "No capture context" );
				const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
				const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
				const options = {
					renderScale: 100,
					phoneRenderCap: false,
					postProcessing: false,
					anisotropicFiltering: false,
					heightFog: false,
					dynamicSun: false,
					floatBloom: false,
					hdrToneMap: false,
					sunShadow: false,
					perPixelLighting: false,
					ambientOcclusion: false,
					smoothCamera: false,
					enchantSparks: false,
					sharedActorRoom: false,
					vHidesCompanions: false,
					dungeonCharacterPvs: false
				};
				let deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
				// A high camera looking down at a box standing on the
				// ground: the box's foot is the contact the row shades.
				renderer.setWorldCamera( { eye: [ 6, 26, -52 ], target: [ 4, 0, 4 ], fov: 1.1, near: 1, far: 500 } );
				const quad = ( id, material, y, size ) => ({
					id,
					center: [ 0, y, 0 ],
					radius: size * 2,
					material,
					geometry: {
						world: true,
						positions: Float32Array.of(
							-size,
							y,
							-size,
							size,
							y,
							-size,
							size,
							y,
							size,
							-size,
							y,
							size
						),
						normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
						uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
						indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
						instances: identity,
						transform: identity
					}
				});
				const lit = colour => ({
					color: colour,
					unlit: false,
					objectLight: 1,
					alphaCutoff: 0,
					blend: false,
					doubleSided: true
				});
				const box = [], normals = [], indices = [];
				const corners = [ [ 0, 0, 0 ], [ 8, 0, 0 ], [ 8, 0, 8 ], [ 0, 0, 8 ], [ 0, 8, 0 ], [ 8, 8, 0 ], [
					8,
					8,
					8
				], [ 0, 8, 8 ] ];
				const faces = [
					[ [ 0, 3, 2, 1 ], [ 0, -1, 0 ] ],
					[ [ [ 4 ], [ 5 ], [ 6 ], [ 7 ] ].flat(), [ 0, 1, 0 ] ],
					[ [ 0, 1, 5, 4 ], [ 0, 0, -1 ] ],
					[ [ [ 2 ], [ 3 ], [ 7 ], [ 6 ] ].flat(), [ 0, 0, 1 ] ],
					[ [ 1, 2, 6, 5 ], [ 1, 0, 0 ] ],
					[ [ [ 0 ], [ 4 ], [ 7 ], [ 3 ] ].flat(), [ -1, 0, 0 ] ]
				];
				for ( const [quadIndices, normal] of faces ) {
					const base = box.length / 3;
					for ( const corner of quadIndices ) {
						box.push( corners[corner][0] + 10, corners[corner][1], corners[corner][2] - 2 );
						normals.push( normal[0], normal[1], normal[2] );
					}
					indices.push( base, base + 1, base + 2, base, base + 2, base + 3 );
				}
				renderer.setWorld( {
					id: "ssao",
					originRegion: 257,
					warnings: [],
					environment: {
						startTimeOfDay: .5,
						ratePerSecond: 0,
						tracks: {
							color0xf0: [ { t: 0, r: 1, g: 1, b: 1 } ],
							color0x124: [ { t: 0, r: .55, g: .55, b: .55 } ]
						}
					},
					groups: [
						quad( "ground", lit( [ .72, .62, .5, 1 ] ), 0, 120 ),
						{
							id: "box",
							center: [ 14, 4, 2 ],
							radius: 12,
							material: lit( [ .5, .55, .65, 1 ] ),
							geometry: {
								world: true,
								positions: new Float32Array( box ),
								normals: new Float32Array( normals ),
								uvs: new Float32Array( box.length / 3 * 2 ),
								indices: new Uint32Array( indices ),
								instances: identity,
								transform: identity
							}
						}
					]
				} );
				const shots = [];
				for ( const mode of [ "off", "ambientOcclusion", "off" ] ) {
					renderer.experimentalVideo( {
						...options,
						...(mode === "ambientOcclusion" ? { ambientOcclusion: true } : {})
					} );
					trace.__passes.length = 0;
					// The occlusion lags a frame: render enough frames that
					// the on capture samples a completed occlusion.
					for ( let frame = 0; frame < 10; frame++ ) await renderer.frame( { width, height }, .16 );
					if ( renderer.error() ) throw Error( `${mode}: ${renderer.error()}` );
					await new Promise( requestAnimationFrame );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					shots.push( {
						mode,
						rgba: [ ...context.getImageData( 0, 0, width, height ).data ],
						passes: trace.__passes.slice( -12 )
					} );
				}
				renderer.dispose();
				return { shots, gpuErrors: trace.__gpuErrors };
			} );
			const [before, during, after] = result.shots;
			// A-B-A: the row off restores the native bytes exactly.
			assert.deepEqual( after.rgba, before.rgba, "The row off restores the exact native frame" );
			// The on frames run the stage's three passes.
			for ( const pass of [ "ssao-convert", "ssao-trace", "ssao-blur" ] ) {
				assert.ok( during.passes.includes( pass ), "The on frames run " + pass );
			}
			// The off frames run no stage pass.
			for ( const off of [ before, after ] ) {
				assert.equal( off.passes.indexOf( "ssao-trace" ), -1, "No SSAO pass off" );
			}
			// The contact darkens: pixels changed, and the change is a
			// darkening (the ambient term only shades down).
			let darkened = 0, brightened = 0;
			for ( let i = 0; i < before.rgba.length; i += 4 ) {
				const was = before.rgba[i] + before.rgba[i + 1] + before.rgba[i + 2];
				const now = during.rgba[i] + during.rgba[i + 1] + during.rgba[i + 2];
				if ( now < was - 12 ) darkened++;
				if ( now > was + 12 ) brightened++;
			}
			assert.ok( darkened > 30, `The contact shadows darken (${darkened} pixels)` );
			assert.ok( brightened < darkened / 4, `The row only shades down (${brightened} brightened)` );
			assert.deepEqual( result.gpuErrors, [], "No uncaptured GPU errors in any mode" );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
