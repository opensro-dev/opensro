/*
===========================================================================

soft-particles.test.mjs - the split sprite fade in isolation

Port-only, not native. A lit ground with a wide additive-style sprite
quad over it carrying the softDepth material (the weather owner's
sprites carry the same flag): the Soft particles row cycles off-on-off
through the production renderer. Off must restore the native bytes
exactly and run no stage pass; on must take the flagged sprites out of
the main pass, run the split stage's depth copy and overlay, and the
presented frame must differ - the sprite's hard edge at the ground
softens. The capture waits one compositor frame after the submits (the
msaa and water lanes' rule: a bitmap taken in the same task reads the
prior present).
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"the soft particles row splits the sprites, runs the stage and restores native bytes",
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
				/** The page's instrumentation slots (typed for the test checker). */
				const trace = /** @type {any} */ (globalThis);
				trace.__passes = [];
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
					waterRefraction: false,
					softParticles: false,
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
				renderer.setWorldCamera( { eye: [ 6, 22, -52 ], target: [ 4, 2, 0 ], fov: 1.1, near: 1, far: 500 } );
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
				renderer.setWorld( {
					id: "soft-particles",
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
						quad( "ground", 0, 120, {
							color: [ .72, .62, .5, 1 ],
							unlit: false,
							objectLight: 1,
							alphaCutoff: 0,
							blend: false,
							doubleSided: true
						} ),
						// The sprite: the softDepth material the weather
						// sprites carry, blended over the ground's far side.
						quad( "sprite", 0.5, 40, {
							color: [ 1, 1, 1, 0.9 ],
							unlit: true,
							alphaCutoff: 0,
							blend: true,
							doubleSided: true,
							fogDisabled: true,
							softDepth: true
						} )
					]
				} );
				const shots = [];
				for ( const mode of [ "off", "softParticles", "off" ] ) {
					renderer.experimentalVideo( {
						...options,
						...(mode === "softParticles" ? { softParticles: true } : {})
					} );
					trace.__passes.length = 0;
					for ( let frame = 0; frame < 10; frame++ ) await renderer.frame( { width, height }, .16 );
					if ( renderer.error() ) throw Error( `${mode}: ${renderer.error()}` );
					// One compositor frame after the submits (the capture rule).
					await new Promise( requestAnimationFrame );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					shots.push( {
						mode,
						rgba: [ ...context.getImageData( 0, 0, width, height ).data ],
						passes: trace.__passes.slice( -14 )
					} );
				}
				renderer.dispose();
				return shots;
			} );
			const [before, during, after] = result;
			// A-B-A: the row off restores the native bytes exactly.
			assert.deepEqual( after.rgba, before.rgba, "The row off restores the exact native frame" );
			// The on frames run the stage's copies and the overlay.
			assert.ok( during.passes.includes( "water-refraction-copies" ), "The on frames copy the frame" );
			assert.ok( during.passes.includes( "water-refraction-overlay" ), "The on frames redraw the sprites" );
			// The off frames run no stage pass.
			for ( const off of [ before, after ] ) {
				assert.equal( off.passes.indexOf( "water-refraction-overlay" ), -1, "No overlay pass off" );
			}
			// The presented on frame differs: the sprite's edge softened.
			let changed = 0;
			for ( let i = 0; i < before.rgba.length; i += 4 ) {
				if ( before.rgba[i] !== during.rgba[i] || before.rgba[i + 1] !== during.rgba[i + 1] ) changed++;
			}
			assert.ok( changed > 20, `The on frame differs (${changed} pixels)` );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
