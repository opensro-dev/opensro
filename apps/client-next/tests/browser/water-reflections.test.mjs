/*
===========================================================================

water-reflections.test.mjs - the traced water reflections in isolation

Port-only, not native. A red pillar standing in a wide water plane,
through the production renderer. The proof runs mirror-less (the native
video slot off): the retail water then carries no reflection term at all,
so the Water reflections row's traced term is the only thing that can
change the water - the reflected ray marches the copied depth, takes the
copied frame's colour at the pillar and reddens the water under the eye.
Off restores byte-exact native pixels with no stage pass; a second pass
with the native mirror on keeps the same off guarantees.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"the traced water reflections redden the water and off restores native bytes",
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
					waterReflections: false,
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
				const flat = ( id, y, size, material ) => ({
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
				// The pillar: a red wall standing in the water, facing the
				// camera. Its reflection is what the trace must find.
				const H = 14;
				const pillar = {
					id: "pillar",
					center: [ 0, H / 2, 8 ],
					radius: H,
					material: lit( [ .85, .08, .06, 1 ] ),
					geometry: {
						world: true,
						positions: Float32Array.of( -3, 0, 8, 3, 0, 8, 3, H, 8, -3, H, 8 ),
						normals: Float32Array.of( 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0 ),
						uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
						indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
						instances: identity,
						transform: identity
					}
				};
				renderer.setWorld( {
					id: "water-reflections",
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
						flat( "seabed", -5, 90, lit( [ .9, .85, .3, 1 ] ) ),
						{
							...flat( "water", 0, 80, {
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
						pillar
					]
				} );
				/*
				================
				capture

				Mirror-less first (slot 4 off): the retail water then carries no
				reflection term at all, so only the trace can change it. The
				native-mirror round keeps the same off guarantees.
				================
				*/
				const capture = async ( reflections, mirror ) => {
					renderer.videoOptions( changeVideo( defaultVideoOptions(), 4, mirror ? 1 : 0 ) );
					renderer.experimentalVideo( { ...options, waterReflections: reflections } );
					trace.__passes.length = 0;
					trace.__tex.length = 0;
					for ( let frame = 0; frame < 6; frame++ ) await renderer.frame( { width, height }, .16 );
					if ( renderer.error() ) throw Error( renderer.error() );
					await new Promise( requestAnimationFrame );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					return {
						rgba: [ ...context.getImageData( 0, 0, width, height ).data ],
						passes: trace.__passes.slice( -12 ),
						tex: trace.__tex.slice()
					};
				};
				const off = await capture( false, false ),
					on = await capture( true, false ),
					offAgain = await capture( false, false ),
					mirrorOff = await capture( false, true ),
					mirrorOn = await capture( true, true ),
					mirrorOffAgain = await capture( false, true );
				renderer.dispose();
				/*
				================
				redBias

				The water's mean red-minus-blue over the water band left of
				the pillar. The water's own colour leans blue; the traced
				pillar leans red.
				================
				*/
				// The water band left of the pillar's screen column, under the
				// horizon: only water lives there mirror-less. The water's own
				// colour leans blue (no red-dominant texel at all); the traced
				// pillar leans red (hundreds of them) - the trace is the only
				// thing that can redden this band.
				const redCount = rgba => {
					let n = 0;
					for ( let y = 42; y < 66; y++ ) {
						for ( let x = 40; x < 88; x++ ) {
							const i = (y * width + x) * 4;
							if ( rgba[i] - rgba[i + 2] > 20 ) n++;
						}
					}
					return n;
				};
				return {
					off,
					on,
					offAgain,
					mirrorOff,
					mirrorOn,
					mirrorOffAgain,
					redCount: {
						off: redCount( off.rgba ),
						on: redCount( on.rgba ),
						mirrorOff: redCount( mirrorOff.rgba ),
						mirrorOn: redCount( mirrorOn.rgba )
					},
					tex: on.tex,
					gpuErrors: trace.__gpuErrors
				};
			} );
			// A-B-A: the row off restores the native bytes exactly, with and
			// without the native mirror.
			assert.deepEqual( result.offAgain.rgba, result.off.rgba, "The row off restores the exact native frame" );
			assert.deepEqual(
				result.mirrorOffAgain.rgba,
				result.mirrorOff.rgba,
				"The row off restores the exact mirrored frame"
			);
			// The stage's copies exist once, in the frame's format.
			const refractionTex = result.tex.filter( t => t.startsWith( "water-refraction-" ) ).sort();
			assert.deepEqual(
				refractionTex,
				[ "water-refraction-colour:bgra8unorm", "water-refraction-depth:r32float" ],
				"The stage owns one colour copy and one depth copy"
			);
			// With the row on, the stage's three passes run after the main pass;
			// off, on both sides, none runs and the native order stands.
			for ( const pass of [ "water-refraction-copies", "water-refraction-depth", "water-refraction-overlay" ] ) {
				assert.ok( result.on.passes.includes( pass ), "The on frames run " + pass );
			}
			for ( const offShot of [ result.off, result.offAgain, result.mirrorOff, result.mirrorOffAgain ] ) {
				assert.equal( offShot.passes.indexOf( "water-refraction-overlay" ), -1, "No overlay pass off" );
			}
			assert.ok( result.mirrorOff.passes.includes( "water-reflection" ), "The native mirror stays on" );
			// The traced pillar reddens the water: mirror-less the retail water
			// carries no reflection term, so every red-dominant texel in the
			// band is the trace's own take of the pillar's screen pixels.
			assert.equal(
				result.redCount.off,
				0,
				`The off water carries no red (${result.redCount.off} texels)`
			);
			assert.ok(
				result.redCount.on >= 100,
				`The trace reddens the water (${result.redCount.on} texels)`
			);
			// Under the native mirror the row's source takes over too.
			assert.ok(
				result.redCount.mirrorOn >= 100,
				`The trace replaces the mirror's term (${result.redCount.mirrorOn} texels)`
			);
			assert.deepEqual( result.gpuErrors, [], "No uncaptured GPU errors in any mode" );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
