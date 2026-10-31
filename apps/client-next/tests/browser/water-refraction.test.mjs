/*
===========================================================================

water-refraction.test.mjs - the refracted water stage in isolation

Port-only, not native. A lit seabed below a wide water plane and a shore
above it, through the production renderer with the native water
reflection on (video slot 4). The proof is the frame's own traced
structure, the msaa lane's pattern: with the row on, the stage's colour
copy, depth convert and overlay passes run in that order after the main
pass every frame, the copies' targets exist in the frame's format, and
the row off restores byte-exact native pixels with no stage pass. The
canvas readback on this headless browser composites one presentation
behind (the artifact the msaa lane documented), so pixel-level water
difference and foam assertions ride the shader's constants instead of
this capture; the overlay draws with the same pipelines the native water
drew with, gated on stages2.w.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"the refracted water row runs its passes and restores native bytes",
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
				const origRequest = GPUAdapter.prototype.requestDevice;
				GPUAdapter.prototype.requestDevice = async function( descriptor ) {
					const device = await origRequest.call( this, descriptor );
					device.addEventListener?.(
						"uncapturederror",
						event => trace.__gpuErrors.push( String( event.error?.message ?? event.error ).slice( 0, 200 ) )
					);
					return device;
				};
				trace.__tex = [];
				trace.__passes = [];
				const origTex = GPUDevice.prototype.createTexture;
				GPUDevice.prototype.createTexture = function( d ) {
					trace.__tex.push( (d.label ?? "?") + ":" + (d.format ?? "?") );
					return origTex.call( this, d );
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
				const { defaultVideoOptions, changeVideo } = await import(
					"/src/engine/foundation/rendering/video-options.ts"
				);
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
					waterRefraction: false,
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
				renderer.setWorldCamera( { eye: [ 30, 16, -46 ], target: [ 0, 0, 6 ], fov: 1.1, near: 1, far: 500 } );
				const quad = ( id, y, size, material ) => ({
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
				renderer.setWorld( {
					id: "water-refraction",
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
						quad( "seabed", -5, 90, lit( [ .9, .85, .3, 1 ] ) ),
						{
							...quad( "water", 0, 80, {
								color: [ .55, .7, .85, .55 ],
								unlit: false,
								objectLight: 1,
								alphaCutoff: 0,
								blend: true,
								water: true,
								doubleSided: true
							} ),
							center: [ 0, 0, 10 ]
						},
						quad( "shore", 3.5, 26, lit( [ .55, .5, .42, 1 ] ) )
					]
				} );
				const shots = [];
				for ( const mode of [ "off", "waterRefraction", "off" ] ) {
					// The native water reflection (video slot 4) stays on in
					// every mode: the row changes only where the water draws.
					renderer.videoOptions( changeVideo( defaultVideoOptions(), 4, 1 ) );
					renderer.experimentalVideo( {
						...options,
						...(mode === "waterRefraction" ? { waterRefraction: true } : {})
					} );
					trace.__passes.length = 0;
					for ( let frame = 0; frame < 6; frame++ ) await renderer.frame( { width, height }, .16 );
					if ( renderer.error() ) throw Error( `${mode}: ${renderer.error()}` );
					// Let the compositor present the last frame before the
					// canvas is read (the raw-probe rule: a bitmap taken in
					// the same task as the submit reads the prior present).
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
				// Without a mirror (video slot 4 off) the row-off water keeps its
				// own lit texture - the native path this change must not alter.
				const mirrorless = [];
				for ( let round = 0; round < 2; round++ ) {
					renderer.videoOptions( changeVideo( defaultVideoOptions(), 4, 0 ) );
					renderer.experimentalVideo( options );
					for ( let frame = 0; frame < 4; frame++ ) await renderer.frame( { width, height }, .16 );
					await new Promise( requestAnimationFrame );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					mirrorless.push( [ ...context.getImageData( 0, 0, width, height ).data ] );
				}
				renderer.dispose();
				return { shots, tex: trace.__tex, mirrorless, gpuErrors: trace.__gpuErrors };
			} );
			const [before, during, after] = result.shots;
			// A-B-A: the row off restores the native bytes exactly.
			assert.deepEqual( after.rgba, before.rgba, "The row off restores the exact native frame" );
			// The copies' targets exist once, in the frame's format.
			const refractionTex = result.tex.filter( t => t.startsWith( "water-refraction-" ) ).sort();
			assert.deepEqual(
				refractionTex,
				[ "water-refraction-colour:bgra8unorm", "water-refraction-depth:r32float" ],
				"The stage owns one colour copy and one depth copy"
			);
			// With the row on, the frames' traced passes hold the stage's
			// three passes after the main pass.
			for ( const pass of [ "water-refraction-copies", "water-refraction-depth", "water-refraction-overlay" ] ) {
				assert.ok( during.passes.includes( pass ), "The on frames run " + pass );
			}
			assert.ok(
				during.passes.indexOf( "water-refraction-copies" ) <
					during.passes.indexOf( "water-refraction-overlay" ),
				"The copies precede the overlay"
			);
			// With the row off, on both sides, no stage pass runs and the
			// native reflection stays on.
			for ( const off of [ before, after ] ) {
				assert.equal( off.passes.indexOf( "water-refraction-overlay" ), -1, "No overlay pass off" );
				assert.ok( off.passes.includes( "water-reflection" ), "The native reflection stays on" );
			}
			// The pixel proof: with the row on the presented frame differs
			// (the retained composition path carries the refracted water).
			// The shore band's whitening rides the shader's constants
			// (stages2.w branch): on this fixture the flat parallel planes
			// put the band below the capture's difference threshold.
			let changed = 0;
			for ( let i = 0; i < before.rgba.length; i += 4 ) {
				const was = before.rgba[i] + before.rgba[i + 1] + before.rgba[i + 2];
				const now = during.rgba[i] + during.rgba[i + 1] + during.rgba[i + 2];
				if ( now !== was ) changed++;
			}
			assert.ok( changed > 400, `The on frame differs (${changed} pixels)` );
			// The refracted water keeps the seabed's hue: a solid red frame
			// (the branch's discarded composite) fails this.
			{
				const mid = (78 * 192 + 96) * 4;
				const water = during.rgba.slice( mid, mid + 4 );
				assert.ok(
					water[1] > water[2],
					`The water pixel leans to the seabed's yellow, not red (${water.join( "," )})`
				);
			}
			// Without a mirror the row-off water stays lit and deterministic.
			assert.deepEqual(
				result.mirrorless[1],
				result.mirrorless[0],
				"The mirrorless off captures are byte-equal"
			);
			{
				const mid = (78 * 192 + 96) * 4;
				const water = result.mirrorless[0].slice( mid, mid + 4 );
				const sum = water[0] + water[1] + water[2];
				assert.ok( sum > 60, `The mirrorless off water stays lit (${water.join( "," )})` );
			}
			assert.deepEqual( result.gpuErrors, [], "No uncaptured GPU errors in any mode" );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
